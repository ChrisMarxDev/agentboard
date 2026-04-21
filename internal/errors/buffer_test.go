package errors

import (
	"strings"
	"testing"
	"time"
)

func fixed(t time.Time) func() time.Time { return func() time.Time { return t } }

func TestRecordNewAndDedupe(t *testing.T) {
	b := NewBuffer()
	now := time.Unix(1_700_000_000, 0)
	b.now = fixed(now)

	in := Input{Component: "Mermaid", Source: "arch.flow", Page: "/features/files", Error: "Parse error on line 2"}

	e1 := b.Record(in)
	if e1 == nil || e1.Count != 1 {
		t.Fatalf("first record: got %+v", e1)
	}

	// Same input within dup window — should NOT bump count.
	b.now = fixed(now.Add(100 * time.Millisecond))
	e2 := b.Record(in)
	if e2.Count != 1 {
		t.Fatalf("dup within window: expected count=1, got %d", e2.Count)
	}

	// Past the dup window — count bumps to 2.
	b.now = fixed(now.Add(2 * time.Second))
	e3 := b.Record(in)
	if e3.Count != 2 {
		t.Fatalf("past window: expected count=2, got %d", e3.Count)
	}
}

func TestDifferentComponentsAreDistinct(t *testing.T) {
	b := NewBuffer()
	b.Record(Input{Component: "Mermaid", Error: "boom"})
	b.Record(Input{Component: "Markdown", Error: "boom"})
	if b.Len() != 2 {
		t.Fatalf("expected 2 distinct entries, got %d", b.Len())
	}
}

func TestClear(t *testing.T) {
	b := NewBuffer()
	b.Record(Input{Component: "X", Error: "a"})
	b.Record(Input{Component: "Y", Error: "b"})
	if got := b.Clear(); got != 2 {
		t.Fatalf("Clear returned %d, want 2", got)
	}
	if b.Len() != 0 {
		t.Fatalf("post-clear len=%d", b.Len())
	}
}

func TestEmptyErrorIsNoOp(t *testing.T) {
	b := NewBuffer()
	if b.Record(Input{Component: "X", Error: ""}) != nil {
		t.Fatal("empty Error should return nil")
	}
	if b.Record(Input{Component: "X", Error: "   "}) != nil {
		t.Fatal("whitespace Error should return nil")
	}
	if b.Len() != 0 {
		t.Fatal("buffer should be empty")
	}
}

func TestFingerprintIgnoresTrailingLines(t *testing.T) {
	b := NewBuffer()
	now := time.Unix(1_700_000_000, 0)
	b.now = fixed(now)

	b.Record(Input{Component: "Mermaid", Source: "x", Page: "/p", Error: "Parse error\n  at line 3\n  at line 4"})
	b.now = fixed(now.Add(2 * time.Second))
	b.Record(Input{Component: "Mermaid", Source: "x", Page: "/p", Error: "Parse error\n  DIFFERENT trailing lines"})

	if b.Len() != 1 {
		t.Fatalf("expected dedupe on first line only, got %d distinct", b.Len())
	}
	list := b.List()
	if list[0].Count != 2 {
		t.Fatalf("expected Count=2, got %d", list[0].Count)
	}
}

func TestListSortedNewestFirst(t *testing.T) {
	b := NewBuffer()
	now := time.Unix(1_700_000_000, 0)

	b.now = fixed(now)
	b.Record(Input{Component: "A", Error: "a"})
	b.now = fixed(now.Add(5 * time.Second))
	b.Record(Input{Component: "B", Error: "b"})
	b.now = fixed(now.Add(10 * time.Second))
	b.Record(Input{Component: "C", Error: "c"})

	list := b.List()
	if list[0].Component != "C" || list[1].Component != "B" || list[2].Component != "A" {
		t.Fatalf("not sorted newest-first: %v",
			[]string{list[0].Component, list[1].Component, list[2].Component})
	}
}

func TestEviction(t *testing.T) {
	b := NewBuffer()
	now := time.Unix(1_700_000_000, 0)
	// Fill over cap. Each entry has a distinct timestamp so eviction has a stable order.
	for i := 0; i < MaxEntries+5; i++ {
		b.now = fixed(now.Add(time.Duration(i) * time.Second))
		b.Record(Input{Component: "C", Error: strings.Repeat("x", 1) + " " + string(rune('a'+i%26)) + " " + itoa(i)})
	}
	if b.Len() > MaxEntries {
		t.Fatalf("buffer exceeded cap: %d > %d", b.Len(), MaxEntries)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
