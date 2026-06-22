package render

import (
	"context"
	"errors"
	"math"
	"sync"

	"github.com/mowshon/moviego/v2/clip"
	"github.com/mowshon/moviego/v2/video"
	videoio "github.com/mowshon/moviego/v2/video/io"
)

// sourceProvider owns one sequential decoder per distinct file source and
// serves frames by source index. It implements video.FrameProvider: a file node
// reached during a pipelined RenderInto copies its frame out of the matching
// source's index-addressed table instead of seeking its own decoder.
type sourceProvider struct {
	sources map[any]*sourceState
}

// newSourceProvider opens a decoder for each source, bound to ctx so a canceled
// render kills every decoder. A goroutine wakes any blocked reader on ctx.Done.
func newSourceProvider(ctx context.Context, sources []video.FileSource) (*sourceProvider, error) {
	p := &sourceProvider{sources: make(map[any]*sourceState, len(sources))}
	for _, s := range sources {
		dec, err := videoio.OpenDecoder(ctx, s.SourcePath(), videoio.DecoderOptions{
			Size: s.SourceSize(),
			Rate: s.SourceRate(),
		})
		if err != nil {
			p.close()
			return nil, err
		}
		st := &sourceState{
			dec:     dec,
			size:    s.SourceSize(),
			decoded: make(map[int]*clip.Frame),
			active:  make(map[int]int),
			pool:    clip.NewFramePool(),
			eofIdx:  math.MaxInt,
		}
		st.cond = sync.NewCond(&st.mu)
		p.sources[s.SourceKey()] = st
	}
	if len(p.sources) > 0 {
		go p.watchCancel(ctx)
	}
	return p, nil
}

// watchCancel propagates context cancellation to every source so readers blocked
// on the decode condition return promptly.
func (p *sourceProvider) watchCancel(ctx context.Context) {
	<-ctx.Done()
	for _, st := range p.sources {
		st.mu.Lock()
		if st.err == nil {
			st.err = ctx.Err()
		}
		st.cond.Broadcast()
		st.mu.Unlock()
	}
}

// SourceFrameInto serves the frame at source index idx for key.
func (p *sourceProvider) SourceFrameInto(ctx context.Context, key any, idx int, dst *clip.Frame) error {
	st, ok := p.sources[key]
	if !ok {
		// No registered source for this key: should not happen for an enumerated
		// graph. Fail loudly rather than silently producing a black frame.
		return clip.Wrap("render source", errUnknownSource)
	}
	return st.frameInto(ctx, idx, dst)
}

// close stops every decoder and releases any frames that were not evicted
// during the run. It is safe to call once.
func (p *sourceProvider) close() {
	for _, st := range p.sources {
		if st.dec != nil {
			_ = st.dec.Close()
		}
		for _, f := range st.decoded {
			f.Release()
		}
	}
}

var errUnknownSource = errors.New("frame requested for a source not in the plan")

// sourceState is the per-source sequential decoder plus an index-addressed table
// of decoded frames. Workers request frames by source index; one of them drives
// the decoder forward while the others wait on the condition. Reads are forward
// in steady state, not strictly forward by construction: the driver targets the
// lowest *undecoded* index any reader currently needs, so a request that lands
// below the decoder position (a worker-entry race during startup) triggers a
// bounded backward correction to recover it. For the graph classes the planner
// admits to the pipeline this only happens while the initial window settles;
// afterwards every target equals head and no seek occurs. Decoded frames are evicted
// once no in-flight reader needs them, bounding memory to the span of indices
// the workers hold at once.
type sourceState struct {
	dec  *videoio.Decoder
	size clip.Size
	pool *clip.FramePool

	mu       sync.Mutex
	cond     *sync.Cond
	decoded  map[int]*clip.Frame // index -> provider-owned frame
	active   map[int]int         // index -> count of readers awaiting/copying it
	head     int                 // next index the decoder will produce
	floor    int                 // frames below floor have been evicted
	started  bool                // the first read has chosen its start index
	decoding bool                // a reader is currently driving the decoder
	eof      bool
	eofIdx   int // first index at/after the source end (valid once eof)
	err      error
}

// frameInto blocks until the frame at idx is decoded, then copies it into dst.
// Concurrent callers cooperate: one drives the decoder forward while the others
// wait on the condition and take the cache hit once their index lands.
func (s *sourceState) frameInto(ctx context.Context, idx int, dst *clip.Frame) error {
	s.mu.Lock()
	s.active[idx]++
	defer func() {
		if s.active[idx]--; s.active[idx] == 0 {
			delete(s.active, idx)
		}
		s.evictLocked()
		s.cond.Broadcast() // let the driver reconsider its backpressure floor
		s.mu.Unlock()
	}()

	for {
		if s.err != nil {
			return s.err
		}
		if f, ok := s.decoded[idx]; ok {
			copy(dst.Pix, f.Pix)
			return nil
		}
		if s.eof && idx >= s.eofIdx {
			return clip.ErrEOF
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if !s.decoding {
			s.driveLocked()
			continue
		}
		s.cond.Wait()
	}
}

// driveLocked reads the lowest undecoded index any reader currently needs. It is
// called with the lock held and the decoder idle; it releases the lock for the
// slow decode, then reinstalls the result. The decoder advances forward in the
// common case (target == head, no seek); a target below head means a reader
// asked for a frame the decoder already passed (a worker-entry race or an
// evicted frame), so it seeks backward to recover it. This is a bounded
// correction for eligible graphs, not a general random-access read pattern: it
// keeps the stage self-healing regardless of the order workers enter their reads.
func (s *sourceState) driveLocked() {
	target, ok := s.lowestUndecodedActiveLocked()
	if !ok {
		return // every needed frame is already decoded; nothing to drive
	}
	s.decoding = true
	seek := false
	switch {
	case !s.started:
		s.started = true
		s.head = target
		seek = target > 0
	case target != s.head:
		seek = true
		s.head = target
	}
	buf := s.pool.Get(s.size.W, s.size.H, clip.RGB24)
	s.mu.Unlock()

	var err error
	if seek {
		err = s.dec.SeekToFrame(target)
	}
	if err == nil {
		err = s.dec.ReadInto(buf)
	}

	s.mu.Lock()
	s.decoding = false
	switch {
	case err == nil:
		s.decoded[target] = buf
		s.head = target + 1
		s.evictLocked()
	case errors.Is(err, clip.ErrEOF):
		s.eof = true
		if target < s.eofIdx {
			s.eofIdx = target
		}
		buf.Release()
	default:
		s.err = err
		buf.Release()
	}
	s.cond.Broadcast()
}

// lowestUndecodedActiveLocked returns the lowest index a reader currently needs
// that is not already decoded, and whether such an index exists.
func (s *sourceState) lowestUndecodedActiveLocked() (int, bool) {
	min, found := 0, false
	for idx := range s.active {
		if _, ok := s.decoded[idx]; ok {
			continue
		}
		if !found || idx < min {
			min, found = idx, true
		}
	}
	return min, found
}

// evictLocked frees decoded frames below the lowest index a reader still needs.
// While at least one reader is active the floor follows the lowest active index;
// when no reader is active the floor is held in place (it is never raised to the
// decoder head) so a worker that has been handed a job but not yet entered its
// read does not lose a frame it is about to request.
func (s *sourceState) evictLocked() {
	min, found := 0, false
	for idx := range s.active {
		if !found || idx < min {
			min, found = idx, true
		}
	}
	if found {
		s.floor = min
	}
	for idx, f := range s.decoded {
		if idx < s.floor {
			f.Release()
			delete(s.decoded, idx)
		}
	}
}
