package render

import (
	"context"
	"errors"
	"sync"

	"github.com/mowshon/moviego/v2/clip"
	"github.com/mowshon/moviego/v2/video"
	videoio "github.com/mowshon/moviego/v2/video/io"
)

// reverseWindowBudget caps the per-source backward cache at ~256 MiB of decoded
// frames. The window length is derived from it and the frame size, so a clip
// short enough to fit is buffered whole (one forward decode pass) and a longer
// one keeps a sliding tail.
const reverseWindowBudget int64 = 256 << 20

// reverseProvider serves a backward (or otherwise non-forward) walk over file
// sources for the sequential engine. The plain VideoFileNode seeks its own
// decoder, and a backward step there triggers a full FFmpeg restart-and-skip, so
// reversing an N-frame clip is O(N^2) decodes. This provider keeps a sliding
// window of recently decoded frames per source: a step inside the window is a
// copy, and a step past its start refills the window with a single forward pass,
// turning the reverse walk into O(N) decodes with O(N/window) restarts.
//
// Short clips (frame count ≤ window) are fully buffered on first access with one
// forward pass and zero subsequent restarts.
//
// It implements video.FrameProvider, so the file node delegates to it through
// the same frameProviderFrom seam the pipeline uses — no node or engine change.
type reverseProvider struct {
	sources map[any]*reverseReader
}

// newReverseProvider opens one decoder per source, bound to ctx so a canceled
// render kills every decoder, and sizes each window from the byte budget.
func newReverseProvider(ctx context.Context, sources []video.FileSource) (*reverseProvider, error) {
	return newReverseProviderWindow(ctx, sources, 0)
}

// newReverseProviderWindow is newReverseProvider with an explicit window length
// (in frames); maxFrames <= 0 derives it from reverseWindowBudget. It exists so
// tests can force a small window and exercise the refill path.
func newReverseProviderWindow(ctx context.Context, sources []video.FileSource, maxFrames int) (*reverseProvider, error) {
	p := &reverseProvider{sources: make(map[any]*reverseReader, len(sources))}
	for _, s := range sources {
		size := s.SourceSize()
		dec, err := videoio.OpenDecoder(ctx, s.SourcePath(), videoio.DecoderOptions{
			Size: size,
			Rate: s.SourceRate(),
		})
		if err != nil {
			p.close()
			return nil, err
		}
		window := maxFrames
		if window <= 0 {
			window = windowFrames(size)
		}

		// Short-clip optimisation: if the whole source fits in the window, record
		// the exact frame count so prefillFull can load it all in one pass.
		var total int
		dur := s.Duration()
		if clip.Finite(dur) {
			n := video.FrameCount(s.SourceRate(), dur)
			if n <= window {
				total = n
			}
		}

		p.sources[s.SourceKey()] = &reverseReader{
			dec:    dec,
			size:   size,
			pool:   clip.NewFramePool(),
			max:    window,
			window: make([]*clip.Frame, 0, window),
			start:  -1,
			total:  total,
		}
	}
	return p, nil
}

// windowFrames returns how many frames of size fit in the byte budget, at least
// one.
func windowFrames(size clip.Size) int {
	frameBytes := int64(size.W) * int64(size.H) * int64(clip.RGB24.BytesPerPixel())
	if frameBytes <= 0 {
		return 1
	}
	n := reverseWindowBudget / frameBytes
	if n < 1 {
		n = 1
	}
	return int(n)
}

// SourceFrameInto serves the frame at source index idx for key.
func (p *reverseProvider) SourceFrameInto(ctx context.Context, key any, idx int, dst *clip.Frame) error {
	r, ok := p.sources[key]
	if !ok {
		return clip.Wrap("reverse source", errUnknownSource)
	}
	return r.frameInto(ctx, idx, dst)
}

// close stops every decoder and releases cached frames. It is safe to call once.
func (p *reverseProvider) close() {
	for _, r := range p.sources {
		if r.dec != nil {
			_ = r.dec.Close()
		}
		r.releaseWindow()
	}
}

// reverseReader is one source's decoder plus a sliding cache of decoded frames
// for indices [start, start+len(window)). The sequential engine drives it from a
// single goroutine; the mutex keeps it safe regardless, matching the other
// provider.
type reverseReader struct {
	dec  *videoio.Decoder
	size clip.Size
	pool *clip.FramePool
	max  int // window length cap, in frames

	mu        sync.Mutex
	window    []*clip.Frame
	start     int  // source index of window[0]; -1 when the window is empty
	total     int  // 0 = long clip; >0 = short clip whose full frame count fits in max
	prefilled bool // true once prefillFull has loaded the entire clip
	reloads   int  // sliding-window refills (backward/jump seeks); read by tests
	// eof/eofIdx record a clean end of stream discovered while decoding: the
	// source has eofIdx real frames (0..eofIdx-1), fewer than its probed duration
	// implied. A later request for an index at or past eofIdx is clamped to the
	// last real frame rather than seeking past the end.
	eof    bool
	eofIdx int
}

// frameInto copies the frame at source index idx into dst, serving it from the
// window when cached, pre-filling the full source for short clips on first miss,
// extending the window for the next forward frame, and otherwise refilling the
// window so it ends at idx.
func (r *reverseReader) frameInto(ctx context.Context, idx int, dst *clip.Frame) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	// Short-clip: pre-fill the entire source on the first access so every
	// subsequent read is a cache hit with zero FFmpeg restarts.
	if r.total > 0 && !r.prefilled {
		if err := r.prefillFull(); err != nil {
			return err
		}
	}

	// Once a clean early EOF is known, a request for a past-the-end index (the
	// reverse map's "last frame" when the probed duration overcounted) serves the
	// last real frame instead of seeking past the stream end.
	if r.eof && idx >= r.eofIdx {
		if r.eofIdx == 0 {
			return clip.ErrEOF
		}
		idx = r.eofIdx - 1
	}

	if f, ok := r.cached(idx); ok {
		copy(dst.Pix, f.Pix)
		return nil
	}
	// Contiguous forward step: the next frame after the window end, with the
	// decoder already positioned there. Decode one and slide the window.
	end := r.start + len(r.window)
	if len(r.window) > 0 && idx == end && r.dec.Pos() == idx {
		f, err := r.readFrame()
		if err == nil {
			r.appendFrame(f)
			copy(dst.Pix, f.Pix)
			return nil
		}
		if !errors.Is(err, clip.ErrEOF) {
			return err
		}
		// The forward read ran past a clean end of stream: idx does not exist, so
		// clamp to the last real frame (the current window tail).
		r.markEOF(idx)
		return r.serveClamped(dst)
	}
	return r.reload(idx, dst)
}

// markEOF records a clean end of stream at index i: frames 0..i-1 are the real
// extent of the source.
func (r *reverseReader) markEOF(i int) {
	if !r.eof || i < r.eofIdx {
		r.eofIdx = i
	}
	r.eof = true
}

// serveClamped copies the last frame in the window into dst, the highest real
// frame when a request landed past the end of the stream.
func (r *reverseReader) serveClamped(dst *clip.Frame) error {
	if len(r.window) == 0 {
		return clip.ErrEOF
	}
	copy(dst.Pix, r.window[len(r.window)-1].Pix)
	return nil
}

// cached returns the cached frame for idx when it is inside the window.
func (r *reverseReader) cached(idx int) (*clip.Frame, bool) {
	if len(r.window) == 0 || idx < r.start || idx >= r.start+len(r.window) {
		return nil, false
	}
	return r.window[idx-r.start], true
}

// prefillFull decodes the entire clip (r.total frames) in a single forward pass,
// building a complete in-memory buffer. It is only called when total <= max.
// On error the partial buffer is released so a subsequent retry starts clean.
func (r *reverseReader) prefillFull() error {
	if err := r.dec.SeekToFrame(0); err != nil {
		return err
	}
	r.start = 0
	for i := 0; i < r.total; i++ {
		f, err := r.readFrame()
		if errors.Is(err, clip.ErrEOF) {
			// Fewer real frames than the probed duration implied: keep what decoded.
			r.markEOF(i)
			break
		}
		if err != nil {
			r.releaseWindow()
			return err
		}
		r.window = append(r.window, f)
	}
	r.prefilled = true
	return nil
}

// reload discards the window and decodes a fresh one of up to max frames ending
// at idx, in a single forward pass from its start. A clean EOF before idx means
// idx is past the real end: the frames decoded so far are kept and the last is
// served. Any other error releases the partial window before returning.
func (r *reverseReader) reload(idx int, dst *clip.Frame) error {
	r.reloads++
	r.releaseWindow()
	newStart := idx - r.max + 1
	if newStart < 0 {
		newStart = 0
	}
	r.start = newStart
	if err := r.dec.SeekToFrame(newStart); err != nil {
		return err
	}
	for i := newStart; i <= idx; i++ {
		f, err := r.readFrame()
		if errors.Is(err, clip.ErrEOF) {
			// idx is past a clean end of stream; serve the last real frame below.
			r.markEOF(i)
			break
		}
		if err != nil {
			r.releaseWindow() // release frames decoded so far in this pass
			return err
		}
		r.window = append(r.window, f)
	}
	if len(r.window) == 0 {
		// The whole window lies past the end of the stream.
		return clip.ErrEOF
	}
	copy(dst.Pix, r.window[len(r.window)-1].Pix)
	return nil
}

// readFrame decodes the frame at the decoder's current position into a pooled
// buffer.
func (r *reverseReader) readFrame() (*clip.Frame, error) {
	f := r.pool.Get(r.size.W, r.size.H, clip.RGB24)
	if err := r.dec.ReadInto(f); err != nil {
		f.Release()
		return nil, err
	}
	return f, nil
}

// appendFrame adds f to the window, dropping the oldest frame once the cap is
// exceeded so the window slides forward.
func (r *reverseReader) appendFrame(f *clip.Frame) {
	r.window = append(r.window, f)
	if len(r.window) > r.max {
		r.window[0].Release()
		r.window = r.window[1:]
		r.start++
	}
}

// releaseWindow returns every cached frame to the pool and empties the window.
func (r *reverseReader) releaseWindow() {
	for _, f := range r.window {
		f.Release()
	}
	r.window = r.window[:0]
	r.start = -1
}
