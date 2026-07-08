package manager

import (
	"testing"
)

func TestRingBuffer_pushPop(t *testing.T) {
	rb := newRingBuffer(3)
	rb.push(sinkRecord{line: "a"})
	rb.push(sinkRecord{line: "b"})

	r, ok := rb.pop()
	if !ok || r.line != "a" {
		t.Errorf("expected first record 'a', got %q ok=%v", r.line, ok)
	}
	r, ok = rb.pop()
	if !ok || r.line != "b" {
		t.Errorf("expected second record 'b', got %q ok=%v", r.line, ok)
	}
	_, ok = rb.pop()
	if ok {
		t.Error("expected empty buffer after two pops")
	}
}

func TestRingBuffer_overflow_dropsOldest(t *testing.T) {
	rb := newRingBuffer(2)
	rb.push(sinkRecord{line: "a"})
	rb.push(sinkRecord{line: "b"})
	rb.push(sinkRecord{line: "c"}) // should evict "a"

	if rb.Dropped() != 1 {
		t.Errorf("expected 1 dropped, got %d", rb.Dropped())
	}

	r, ok := rb.pop()
	if !ok || r.line != "b" {
		t.Errorf("expected oldest surviving 'b', got %q ok=%v", r.line, ok)
	}
	r, ok = rb.pop()
	if !ok || r.line != "c" {
		t.Errorf("expected 'c', got %q ok=%v", r.line, ok)
	}
}

func TestRingBuffer_panicOnZeroCapacity(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for capacity=0")
		}
	}()
	newRingBuffer(0)
}

func TestRingBuffer_len(t *testing.T) {
	rb := newRingBuffer(5)
	if rb.Len() != 0 {
		t.Errorf("expected len 0, got %d", rb.Len())
	}
	rb.push(sinkRecord{line: "x"})
	rb.push(sinkRecord{line: "y"})
	if rb.Len() != 2 {
		t.Errorf("expected len 2, got %d", rb.Len())
	}
	rb.pop()
	if rb.Len() != 1 {
		t.Errorf("expected len 1 after pop, got %d", rb.Len())
	}
}

func TestRingBuffer_wrapsAround(t *testing.T) {
	rb := newRingBuffer(3)
	rb.push(sinkRecord{line: "1"})
	rb.push(sinkRecord{line: "2"})
	rb.push(sinkRecord{line: "3"})
	rb.pop()
	rb.push(sinkRecord{line: "4"}) // wraps tail around

	r, _ := rb.pop()
	if r.line != "2" {
		t.Errorf("expected '2', got %q", r.line)
	}
	r, _ = rb.pop()
	if r.line != "3" {
		t.Errorf("expected '3', got %q", r.line)
	}
	r, _ = rb.pop()
	if r.line != "4" {
		t.Errorf("expected '4', got %q", r.line)
	}
}
