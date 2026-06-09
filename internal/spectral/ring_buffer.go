// Package spectral provides frequency-domain signal processing for vehicle
// trajectory analysis. It has no dependencies on other internal packages,
// which allows memory, traffic, and attention to import it freely.
package spectral

// RingBuffer is a fixed-size circular buffer of float64 samples.
// It is not safe for concurrent use; callers rely on MemoryStore's
// existing RWMutex for synchronisation.
type RingBuffer struct {
	data []float64
	head int // next write position; also the oldest entry when the buffer is full
	count int
	size  int
}

// NewRingBuffer allocates a RingBuffer with the given capacity.
func NewRingBuffer(size int) *RingBuffer {
	return &RingBuffer{
		data: make([]float64, size),
		size: size,
	}
}

// Push appends v, overwriting the oldest sample when the buffer is full.
func (r *RingBuffer) Push(v float64) {
	r.data[r.head] = v
	r.head = (r.head + 1) % r.size
	if r.count < r.size {
		r.count++
	}
}

// Slice returns a chronologically ordered copy of all valid samples
// (oldest first). Returns nil when the buffer is empty.
func (r *RingBuffer) Slice() []float64 {
	if r.count == 0 {
		return nil
	}
	out := make([]float64, r.count)
	if r.count < r.size {
		// Buffer not yet full: valid data is data[0..count-1].
		copy(out, r.data[:r.count])
	} else {
		// Buffer full: head points to the oldest entry.
		n := copy(out, r.data[r.head:])
		copy(out[n:], r.data[:r.head])
	}
	return out
}

// Len returns the number of valid samples currently held.
func (r *RingBuffer) Len() int { return r.count }

// Full reports whether the buffer has reached capacity.
func (r *RingBuffer) Full() bool { return r.count == r.size }

// Reset clears all samples without reallocating.
func (r *RingBuffer) Reset() {
	r.head = 0
	r.count = 0
}
