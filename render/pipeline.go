package render

import (
	"context"
	"errors"
	"math"
	"sync"
	"sync/atomic"

	"github.com/mowshon/moviego/v2/clip"
	"github.com/mowshon/moviego/v2/video"
)

// frameSink consumes rendered frames in output order. Buffers are owned by the
// engine and released after writeFrame returns, so a sink must not retain them.
type frameSink interface {
	writeFrame(rf clip.RenderedFrame) error
}

// job result moving from the worker pool to the encode-feed stage. eof marks an
// index at or past the source's end (it carries no buffers).
type pipeResult struct {
	idx      int
	rgb      *clip.Frame
	alpha    *clip.Frame
	hasAlpha bool
	eof      bool
}

// runPipeline executes the three-stage pipeline: one sequential decoder per
// source feeds a worker pool that fans out the per-frame pixel work, and a
// single encode-feed goroutine reorders by index and writes in order. It is
// used for static, linear, and bounded graph classes (the planner routes
// random-access graphs to the sequential engine).
func runPipeline(ctx context.Context, root video.VideoClip, plan *Plan, sink frameSink) error {
	pctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Decode stage: a shared sequential decoder per source, injected so file
	// nodes read from it instead of seeking their own.
	prov, err := newSourceProvider(pctx, plan.sources)
	if err != nil {
		return err
	}
	defer prov.close()
	rctx := video.WithFrameProvider(pctx, prov)

	pool := clip.NewFramePool()
	jobs := make(chan int, plan.Workers)
	results := make(chan pipeResult, plan.MaxInflight)

	// minEOF is the lowest output index that ran past the source end; frames at
	// or beyond it are not written (matching the sequential engine's EOF stop).
	var minEOF int64 = math.MaxInt64

	var firstErr error
	var errOnce sync.Once
	fail := func(e error) {
		errOnce.Do(func() { firstErr = e })
		cancel()
	}

	// Dispatcher: schedule indices in order, stopping once an EOF is known.
	go func() {
		defer close(jobs)
		for i := 0; i < plan.Frames; i++ {
			if int64(i) >= atomic.LoadInt64(&minEOF) {
				return
			}
			select {
			case <-pctx.Done():
				return
			case jobs <- i:
			}
		}
	}()

	// Worker pool: render each frame via the (provider-backed) graph.
	var wg sync.WaitGroup
	for w := 0; w < plan.Workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				if int64(i) >= atomic.LoadInt64(&minEOF) {
					if !send(pctx, results, pipeResult{idx: i, eof: true}) {
						return
					}
					continue
				}
				rgb := pool.Get(plan.Size.W, plan.Size.H, clip.RGB24)
				var alpha *clip.Frame
				if plan.Transparent {
					alpha = pool.Get(plan.Size.W, plan.Size.H, clip.Gray8)
				}
				hasA, err := root.RenderInto(rctx, plan.Rate.FrameTime(i), rgb, alpha)
				if err != nil {
					releaseFrames(rgb, alpha)
					if errors.Is(err, clip.ErrEOF) {
						atomicMin(&minEOF, int64(i))
						if !send(pctx, results, pipeResult{idx: i, eof: true}) {
							return
						}
						continue
					}
					fail(err)
					return
				}
				if !send(pctx, results, pipeResult{idx: i, rgb: rgb, alpha: alpha, hasAlpha: hasA}) {
					releaseFrames(rgb, alpha)
					return
				}
			}
		}()
	}
	go func() { wg.Wait(); close(results) }()

	// Encode-feed: reorder by index and write contiguous frames until EOF.
	pending := make(map[int]pipeResult)
	next := 0
	stop := false
	for r := range results {
		pending[r.idx] = r
		for !stop {
			cur, ok := pending[next]
			if !ok {
				break
			}
			if cur.eof || int64(next) >= atomic.LoadInt64(&minEOF) {
				stop = true
				break
			}
			delete(pending, next)
			err := sink.writeFrame(clip.RenderedFrame{RGB: cur.rgb, Alpha: alphaOf(cur), Index: cur.idx})
			releaseFrames(cur.rgb, cur.alpha)
			if err != nil {
				fail(err)
				stop = true
				break
			}
			next++
		}
	}
	// Release any frames buffered past the stop point.
	for _, r := range pending {
		releaseFrames(r.rgb, r.alpha)
	}

	if firstErr != nil {
		return firstErr
	}
	return ctx.Err()
}

// send delivers r on out, unblocking if the render context is canceled. It
// returns false when the caller should stop (context done).
func send(ctx context.Context, out chan<- pipeResult, r pipeResult) bool {
	select {
	case out <- r:
		return true
	case <-ctx.Done():
		return false
	}
}

// alphaOf returns the alpha sidecar only when the frame actually carried one;
// the pooled alpha buffer is released separately regardless.
func alphaOf(r pipeResult) *clip.Frame {
	if r.hasAlpha {
		return r.alpha
	}
	return nil
}

func releaseFrames(rgb, alpha *clip.Frame) {
	if rgb != nil {
		rgb.Release()
	}
	if alpha != nil {
		alpha.Release()
	}
}

// atomicMin lowers *p to v if v is smaller.
func atomicMin(p *int64, v int64) {
	for {
		old := atomic.LoadInt64(p)
		if v >= old || atomic.CompareAndSwapInt64(p, old, v) {
			return
		}
	}
}
