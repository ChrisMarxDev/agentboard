package files

import (
	"bytes"

	"gopkg.in/yaml.v3"
)

// frontmatterScanBytes caps how much of a file we read when hunting for YAML
// frontmatter. Frontmatter lives at the top of the file, so a small cap keeps
// the cost of parsing trivial even when a folder has hundreds of markdown files.
const frontmatterScanBytes = 4096

// parseFrontmatter extracts a YAML frontmatter block from the head of a file's
// bytes. Returns nil on any failure — no frontmatter marker, malformed YAML,
// non-object root. Silent by design: callers treat "no frontmatter" as normal.
func parseFrontmatter(data []byte) map[string]any {
	if len(data) == 0 {
		return nil
	}
	head := data
	if len(head) > frontmatterScanBytes {
		head = head[:frontmatterScanBytes]
	}

	// Frontmatter must start with "---" followed by a newline.
	if !bytes.HasPrefix(head, []byte("---\n")) && !bytes.HasPrefix(head, []byte("---\r\n")) {
		return nil
	}
	// Skip the opening marker.
	var body []byte
	if bytes.HasPrefix(head, []byte("---\r\n")) {
		body = head[5:]
	} else {
		body = head[4:]
	}

	// Find the closing "---" on its own line.
	end := findClosingMarker(body)
	if end < 0 {
		return nil
	}
	raw := body[:end]

	var parsed map[string]any
	if err := yaml.Unmarshal(raw, &parsed); err != nil {
		return nil
	}
	if len(parsed) == 0 {
		return nil
	}
	return parsed
}

// findClosingMarker returns the byte offset of "---" at line start, or -1.
func findClosingMarker(body []byte) int {
	// Scan for \n---\n or \n---\r\n. Also accept a leading "---\n" (empty body).
	if bytes.HasPrefix(body, []byte("---\n")) || bytes.HasPrefix(body, []byte("---\r\n")) {
		return 0
	}
	for i := 0; i < len(body)-3; i++ {
		if body[i] != '\n' {
			continue
		}
		if bytes.HasPrefix(body[i+1:], []byte("---\n")) || bytes.HasPrefix(body[i+1:], []byte("---\r\n")) || bytes.Equal(body[i+1:], []byte("---")) {
			return i
		}
	}
	return -1
}
