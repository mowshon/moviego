package ffmpeg

import "sync"

// ringWriter is an io.Writer that retains only the last cap bytes written and
// never blocks. FFmpeg logs to stderr continuously; draining into a ring keeps
// a full pipe from stalling process exit while preserving the tail for error
// reporting. Write and Bytes are safe for concurrent use.
type ringWriter struct {
	mu   sync.Mutex
	buf  []byte
	pos  int  // next write index
	full bool // buf has wrapped at least once
}

func newRingWriter(capacity int) *ringWriter {
	return &ringWriter{buf: make([]byte, capacity)}
}

func (r *ringWriter) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	total := len(p)
	c := len(r.buf)
	if total >= c {
		copy(r.buf, p[total-c:])
		r.pos = 0
		r.full = true
		return total, nil
	}
	n := copy(r.buf[r.pos:], p)
	if n < total {
		copy(r.buf, p[n:])
		r.full = true
	}
	r.pos += total
	if r.pos >= c {
		r.pos -= c
		r.full = true
	}
	return total, nil
}

// Bytes returns a copy of the retained tail in write order.
func (r *ringWriter) Bytes() []byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.full {
		out := make([]byte, r.pos)
		copy(out, r.buf[:r.pos])
		return out
	}
	out := make([]byte, len(r.buf))
	n := copy(out, r.buf[r.pos:])
	copy(out[n:], r.buf[:r.pos])
	return out
}
