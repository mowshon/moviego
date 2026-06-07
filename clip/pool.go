package clip

import "sync"

// FramePool recycles frame buffers keyed by byte length, so variable-size
// graphs bucket naturally and a render never pays for repeated allocation.
// Pooling is a fast path, never a correctness requirement.
type FramePool struct {
	mu   sync.Mutex
	free map[int][]*Frame
}

// NewFramePool returns an empty pool.
func NewFramePool() *FramePool {
	return &FramePool{free: make(map[int][]*Frame)}
}

// Get returns a frame of the requested size/format, reusing a released buffer
// of the same byte length when one is available.
func (p *FramePool) Get(w, h int, f PixelFormat) *Frame {
	stride := w * f.BytesPerPixel()
	n := stride * h

	p.mu.Lock()
	bucket := p.free[n]
	if len(bucket) > 0 {
		fr := bucket[len(bucket)-1]
		p.free[n] = bucket[:len(bucket)-1]
		p.mu.Unlock()
		fr.W, fr.H, fr.Stride, fr.Format, fr.pool = w, h, stride, f, p
		return fr
	}
	p.mu.Unlock()

	fr := NewFrame(w, h, f)
	fr.pool = p
	return fr
}

// Put returns a frame's buffer to the pool. Prefer Frame.Release. It accepts
// only a frame currently checked out from this pool and clears that link, so a
// double Put (or a Put followed by Release) is a safe no-op and a buffer is
// never handed to two owners. Concurrent Put of the same frame violates the
// single-owner contract and is not defended against here.
func (p *FramePool) Put(fr *Frame) {
	if fr == nil || fr.pool != p {
		return
	}
	fr.pool = nil
	n := len(fr.Pix)
	p.mu.Lock()
	p.free[n] = append(p.free[n], fr)
	p.mu.Unlock()
}
