package store

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"sync"
	"time"
)

// Stream op names.
const (
	OpAppend = "APPEND"
)

// Stream tuning constants. Conservative numbers — never need to be
// configurable in Phase 1; revisit if a single project routinely
// exceeds these.
const (
	streamRotateBytes  = 100 * 1024 * 1024 // rotate when active segment >= 100 MB
	streamSegmentLimit = 5                 // keep .1.ndjson .. .5.ndjson
	streamTailCapacity = 1000              // in-memory tail per stream
	streamMaxLineBytes = 4096              // PIPE_BUF on Linux; below this, O_APPEND is atomic
)

// StreamLine is the wire shape returned by ReadStream. Each line is its
// own envelope-lite: a server-set timestamp plus the user value. We do
// not version individual stream lines (streams have no CAS surface).
type StreamLine struct {
	TS    string          `json:"ts"`
	Value json.RawMessage `json:"value"`
}

// streamState tracks the in-memory tail buffer for one stream. Held
// inside the Store under a single mutex (s.streamMu). The on-disk file
// is the source of truth; this is a read-side cache only.
type streamState struct {
	mu        sync.Mutex
	tail      []StreamLine // ring buffer; len <= streamTailCapacity
	approxLen int64        // approximate bytes in active segment, for rotation
}

func (s *Store) streamStateFor(key string) *streamState {
	s.streamsMu.Lock()
	defer s.streamsMu.Unlock()
	if st, ok := s.streams[key]; ok {
		return st
	}
	if s.streams == nil {
		s.streams = map[string]*streamState{}
	}
	// Lazy init: scan disk once to populate tail + approxLen.
	st := &streamState{tail: nil, approxLen: 0}
	if fi, err := os.Stat(streamPath(s.dataDir, key)); err == nil {
		st.approxLen = fi.Size()
		st.tail = readStreamTail(streamPath(s.dataDir, key), streamTailCapacity)
	}
	s.streams[key] = st
	return st
}

// Append writes one line to a stream. Creates the stream on first call.
// Returns the timestamp the server assigned.
//
// Concurrency: O_APPEND on a single open file is atomic for writes
// shorter than PIPE_BUF (Linux: 4096 bytes; macOS: 512 bytes). We
// enforce streamMaxLineBytes = 4096 on the line bytes (envelope JSON +
// trailing newline) and reject anything larger as ErrLineTooLong; the
// caller can split into multiple lines or store as a singleton blob.
func (s *Store) Append(key string, value json.RawMessage, actor string) (StreamLine, error) {
	if err := ValidateKey(key); err != nil {
		return StreamLine{}, err
	}
	if !json.Valid(value) {
		return StreamLine{}, fmt.Errorf("%w: stream value is not valid JSON", ErrInvalidValue)
	}
	shape, err := detectShape(s.dataDir, key)
	if err != nil {
		return StreamLine{}, err
	}
	if shape != "" && shape != ShapeStream {
		return StreamLine{}, &WrongShapeError{Key: key, Actual: shape, Attempt: ShapeStream}
	}

	line := StreamLine{
		TS:    time.Now().UTC().Format(time.RFC3339Nano),
		Value: value,
	}
	bytes, err := json.Marshal(line)
	if err != nil {
		return StreamLine{}, err
	}
	bytes = append(bytes, '\n')
	if len(bytes) > streamMaxLineBytes {
		return StreamLine{}, fmt.Errorf("%w: line is %d bytes (max %d)", ErrLineTooLong, len(bytes), streamMaxLineBytes)
	}

	st := s.streamStateFor(key)
	st.mu.Lock()
	defer st.mu.Unlock()

	// Rotate before write if appending would push us past the limit.
	if st.approxLen+int64(len(bytes)) > streamRotateBytes {
		if err := rotateStream(s.dataDir, key); err != nil {
			return StreamLine{}, err
		}
		st.approxLen = 0
		// Tail buffer survives rotation: it represents recent lines
		// regardless of which segment they live in.
	}

	f, err := os.OpenFile(streamPath(s.dataDir, key), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return StreamLine{}, err
	}
	if _, err := f.Write(bytes); err != nil {
		f.Close()
		return StreamLine{}, err
	}
	if err := f.Close(); err != nil {
		return StreamLine{}, err
	}

	st.approxLen += int64(len(bytes))
	if len(st.tail) >= streamTailCapacity {
		copy(st.tail, st.tail[1:])
		st.tail = st.tail[:streamTailCapacity-1]
	}
	st.tail = append(st.tail, line)

	s.touchCatalog(key, ShapeStream)
	s.recordActivity(ActivityEntry{
		Actor: actor, Op: OpAppend, Path: key, Shape: ShapeStream,
	})
	s.notify(Event{Key: key, Op: OpAppend, Shape: ShapeStream})
	return line, nil
}

// AppendBatch appends multiple lines in one server call. Each line is
// timestamped at the moment it's written; bytes go to disk in order.
// On error mid-batch the lines that already landed stay; the caller
// gets back the count and a slice of successful lines so it can retry
// from the failure point.
func (s *Store) AppendBatch(key string, values []json.RawMessage, actor string) ([]StreamLine, error) {
	out := make([]StreamLine, 0, len(values))
	for _, v := range values {
		line, err := s.Append(key, v, actor)
		if err != nil {
			return out, err
		}
		out = append(out, line)
	}
	return out, nil
}

// ReadStreamOpts narrows a stream read.
type ReadStreamOpts struct {
	Limit int    // last N lines (default 100, max streamTailCapacity for cheap path)
	Since string // RFC3339Nano; only return lines with ts > Since
	Until string // RFC3339Nano; only return lines with ts <= Until
}

// ReadStream returns up to Limit recent lines, newest last. For Limit
// <= streamTailCapacity the cheap path uses the in-memory tail buffer;
// larger queries fall through to walking files.
func (s *Store) ReadStream(key string, opts ReadStreamOpts) ([]StreamLine, error) {
	if err := ValidateKey(key); err != nil {
		return nil, err
	}
	shape, err := detectShape(s.dataDir, key)
	if err != nil {
		return nil, err
	}
	switch shape {
	case "":
		return nil, ErrNotFound
	case ShapeStream:
		// fall through
	default:
		return nil, &WrongShapeError{Key: key, Actual: shape, Attempt: ShapeStream}
	}

	if opts.Limit <= 0 {
		opts.Limit = 100
	}

	st := s.streamStateFor(key)
	st.mu.Lock()
	tailCopy := append([]StreamLine{}, st.tail...)
	st.mu.Unlock()

	if opts.Limit <= len(tailCopy) && opts.Since == "" && opts.Until == "" {
		// Cheap path: tail buffer is sufficient.
		return tailCopy[len(tailCopy)-opts.Limit:], nil
	}

	// Slow path: walk segments oldest-first and filter.
	all, err := readAllSegments(s.dataDir, key)
	if err != nil {
		return nil, err
	}
	filtered := all[:0]
	for _, l := range all {
		if opts.Since != "" && l.TS <= opts.Since {
			continue
		}
		if opts.Until != "" && l.TS > opts.Until {
			continue
		}
		filtered = append(filtered, l)
	}
	if len(filtered) > opts.Limit {
		filtered = filtered[len(filtered)-opts.Limit:]
	}
	return filtered, nil
}

// DeleteStream removes the active segment + every rotated segment.
// Idempotent.
func (s *Store) DeleteStream(key, actor string) error {
	if err := ValidateKey(key); err != nil {
		return err
	}
	shape, err := detectShape(s.dataDir, key)
	if err != nil {
		return err
	}
	switch shape {
	case "":
		return nil
	case ShapeStream:
		// fall through
	default:
		return &WrongShapeError{Key: key, Actual: shape, Attempt: ShapeStream}
	}

	st := s.streamStateFor(key)
	st.mu.Lock()
	defer st.mu.Unlock()

	if err := removePath(streamPath(s.dataDir, key)); err != nil {
		return err
	}
	for n := 1; n <= streamSegmentLimit; n++ {
		_ = removePath(streamRotatedPath(s.dataDir, key, n))
	}
	st.tail = nil
	st.approxLen = 0

	s.dropFromCatalog(key)
	s.recordActivity(ActivityEntry{
		Actor: actor, Op: OpDelete, Path: key, Shape: ShapeStream,
	})
	s.notify(Event{Key: key, Op: OpDelete, Shape: ShapeStream})
	return nil
}

// rotateStream renames the active segment to .1.ndjson, bumps the chain
// (5 -> delete, 4 -> 5, ..., 1 -> 2), and lets the next Append create a
// fresh active file. All renames are atomic; the brief window between
// renames is tolerable because the tail buffer covers any reads.
func rotateStream(dataDir, key string) error {
	// Drop the oldest segment if at the cap.
	if err := removePath(streamRotatedPath(dataDir, key, streamSegmentLimit)); err != nil {
		return err
	}
	// Slide existing segments down: ..., 2->3, 1->2.
	for n := streamSegmentLimit - 1; n >= 1; n-- {
		from := streamRotatedPath(dataDir, key, n)
		if _, err := os.Stat(from); errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err := os.Rename(from, streamRotatedPath(dataDir, key, n+1)); err != nil {
			return err
		}
	}
	// Active -> .1.ndjson.
	if _, err := os.Stat(streamPath(dataDir, key)); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return os.Rename(streamPath(dataDir, key), streamRotatedPath(dataDir, key, 1))
}

// readStreamTail walks one file from the start collecting lines, then
// returns the last N. Inefficient for huge files; only called on lazy
// init for the streamState. For active streams this is one-time-per-
// process; for replay the slow path in ReadStream walks all segments.
func readStreamTail(path string, n int) []StreamLine {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, streamMaxLineBytes), streamMaxLineBytes)

	out := make([]StreamLine, 0, n)
	for scanner.Scan() {
		var l StreamLine
		if err := json.Unmarshal(scanner.Bytes(), &l); err != nil {
			continue
		}
		if len(out) >= n {
			copy(out, out[1:])
			out = out[:n-1]
		}
		out = append(out, l)
	}
	return out
}

// readAllSegments returns every line across all segments for a stream,
// oldest first. Used for time-range queries and history reads. Walks
// files in oldest -> newest order: .5.ndjson, .4.ndjson, ..., .ndjson.
func readAllSegments(dataDir, key string) ([]StreamLine, error) {
	// Collect segment paths that exist, sorted by segment number desc
	// (older segments have higher numbers, e.g. .5 is the oldest).
	type seg struct {
		n    int
		path string
	}
	var segs []seg
	for n := streamSegmentLimit; n >= 1; n-- {
		p := streamRotatedPath(dataDir, key, n)
		if _, err := os.Stat(p); err == nil {
			segs = append(segs, seg{n, p})
		}
	}
	// Active segment is newest; appended last.
	if _, err := os.Stat(streamPath(dataDir, key)); err == nil {
		segs = append(segs, seg{0, streamPath(dataDir, key)})
	}
	sort.Slice(segs, func(i, j int) bool { return segs[i].n > segs[j].n })

	var out []StreamLine
	for _, s := range segs {
		f, err := os.Open(s.path)
		if err != nil {
			return nil, err
		}
		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, streamMaxLineBytes), streamMaxLineBytes)
		for scanner.Scan() {
			var l StreamLine
			if err := json.Unmarshal(scanner.Bytes(), &l); err != nil {
				continue
			}
			out = append(out, l)
		}
		f.Close()
	}
	return out, nil
}
