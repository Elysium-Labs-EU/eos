package manager

import "sync"

// ringBuffer is a fixed-capacity circular buffer of sinkRecords.
// On overflow it drops the oldest record and increments a counter.
// All methods are safe for concurrent use.
type ringBuffer struct {
	buf     []sinkRecord
	head    int
	tail    int
	size    int
	cap     int
	dropped uint64
	mu      sync.Mutex
}

func newRingBuffer(capacity int) *ringBuffer {
	return &ringBuffer{
		buf: make([]sinkRecord, capacity),
		cap: capacity,
	}
}

// push adds a record. If full, the oldest record is overwritten.
func (r *ringBuffer) push(rec sinkRecord) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.size == r.cap {
		// Overwrite oldest: advance head past it.
		r.head = (r.head + 1) % r.cap
		r.dropped++
	} else {
		r.size++
	}
	r.buf[r.tail] = rec
	r.tail = (r.tail + 1) % r.cap
}

// pop removes and returns the oldest record.
// Returns false if the buffer is empty.
func (r *ringBuffer) pop() (sinkRecord, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.size == 0 {
		return sinkRecord{}, false
	}
	rec := r.buf[r.head]
	r.buf[r.head] = sinkRecord{} // clear for GC
	r.head = (r.head + 1) % r.cap
	r.size--
	return rec, true
}

// dropped returns the number of records dropped due to overflow since creation.
func (r *ringBuffer) Dropped() uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.dropped
}

// Len returns the current number of records in the buffer.
func (r *ringBuffer) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.size
}
