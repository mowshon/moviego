package render

import (
	"runtime"

	"github.com/mowshon/moviego/v2/clip"
	"github.com/mowshon/moviego/v2/ffmpeg"
	"github.com/mowshon/moviego/v2/video"
)

// Engine identifies which render engine the planner selected.
type Engine int

const (
	// EngineSequential renders frames one at a time on the calling goroutine.
	// It is the correctness oracle and the engine for random-access graphs.
	EngineSequential Engine = iota
	// EnginePipeline decodes each source sequentially while a worker pool fans
	// out the per-frame pixel work, then reorders for the encoder.
	EnginePipeline
	// EngineFFmpegOnly delegates the whole graph to one FFmpeg invocation via a
	// filtergraph, skipping Go pixels entirely.
	EngineFFmpegOnly
)

func (e Engine) String() string {
	switch e {
	case EnginePipeline:
		return "pipeline"
	case EngineFFmpegOnly:
		return "ffmpeg-only"
	default:
		return "sequential"
	}
}

// Class is the render graph classification that governs the decode strategy.
// It is derived from the root's SourceAccess (the least-linear leaf).
type Class int

const (
	// ClassStatic has no time-dependent source reads (image/color graphs).
	ClassStatic Class = iota
	// ClassLinear reads each source monotonically (file passthrough, subclip).
	ClassLinear
	// ClassBounded reorders source reads within a bounded window.
	ClassBounded
	// ClassRandom reads arbitrary source times (loop, reverse, negative speed).
	ClassRandom
)

func (c Class) String() string {
	switch c {
	case ClassStatic:
		return "static"
	case ClassLinear:
		return "linear-streamable"
	case ClassBounded:
		return "bounded-cache"
	default:
		return "random-access"
	}
}

// planOptions are the planner inputs derived from a public ExportOptions plus
// the resolved output rate. Workers <= 0 means "choose automatically"; a
// non-nil engineOverride forces an engine (used by tests to compare engines).
// enableFusion opts INTO the FFmpeg-only fast path; it is off by default so a
// plain export always uses the Go engines (the correctness oracle) unless the
// caller explicitly asks for fusion. debugf, when set, receives the
// engine/fusion decision for observability.
type planOptions struct {
	rate           clip.Rate
	workers        int
	engineOverride *Engine
	enableFusion   bool
	debugf         func(string, ...any)
}

// Plan is the validated, classified render schedule the engines execute.
type Plan struct {
	Rate        clip.Rate
	Frames      int  // scheduled frame count (floor policy); may stop early at EOF
	Transparent bool // the root carries a mask -> RGBA encode feed
	Size        clip.Size

	Class   Class
	Engine  Engine
	Workers int

	// FusionReason records why the planner did or did not fuse the graph into a
	// single FFmpeg invocation (logged in debug mode, asserted in tests).
	FusionReason string
	// fusion is the assembled filtergraph for an EngineFFmpegOnly plan; nil for
	// the Go engines.
	fusion *ffmpeg.FilterGraph

	// ReorderDepth bounds the encode-feed reorder buffer; MaxInflight bounds the
	// frames in flight across all stages. Budget is the resulting memory ceiling
	// in bytes, exposed for observability and tests.
	ReorderDepth int
	MaxInflight  int
	Budget       int64

	sources []video.FileSource
}

const (
	// ffmpegReserve leaves a core for the FFmpeg encoder so render workers and
	// the encoder do not oversubscribe.
	ffmpegReserve = 1
	// perWorkerScratchFrames approximates the transform scratch a worker holds
	// (child RGB + mask) for the memory budget.
	perWorkerScratchFrames = 2
)

// BuildPlan validates the clip, resolves the output rate, classifies the graph,
// enumerates its file sources, and chooses an engine, worker count, and memory
// budget. A clip with no finite duration cannot be scheduled.
func BuildPlan(root video.VideoClip, opt planOptions) (*Plan, error) {
	dur := root.Duration()
	if !clip.Finite(dur) {
		return nil, clip.Wrap("plan", clip.ErrNoDuration)
	}
	rate := opt.rate
	if rate.Num == 0 {
		r, ok := root.Rate()
		if !ok {
			return nil, clip.Wrap("plan", clip.ErrNoRate)
		}
		rate = r
	}
	if rate.Num <= 0 || rate.Den <= 0 {
		return nil, clip.Wrap("plan", clip.ErrNoRate)
	}

	var sources []video.FileSource
	video.WalkSources(root, func(fs video.FileSource) { sources = append(sources, fs) })

	class := classify(root.SourceAccess())
	frames := scheduledFrames(rate, dur)
	size := root.Size()
	transparent := root.HasMask()

	workers := opt.workers
	if workers <= 0 {
		workers = autoWorkers(class)
	}

	// The pipeline is only correct when the decode stage can serve every source
	// with a single forward pass and the per-frame pixel work is safe to run
	// concurrently. Both are graph properties, so they are decided here rather
	// than discovered at runtime: a graph that fails either gate runs on the
	// sequential oracle instead of thrashing a shared decoder or racing an
	// unsafe node. (See sourceProvider for why file leaves are exempt from the
	// parallel-safety gate: their decode is taken over by the provider.)
	eligible := pipelineEligible(root)
	engine := chooseEngine(class, workers, eligible)

	// The FFmpeg-only fast path is opt-in: when the caller enables fusion and the
	// whole graph is expressible as a filtergraph, one FFmpeg invocation avoids
	// the rawvideo round-trip entirely. It is the planner's decision, not
	// the node's. Fusion is off by default so a plain export always uses the Go
	// engines (the correctness oracle); an engineOverride (used by tests to pin
	// an engine) also skips it. A graph that cannot be fused falls back cleanly
	// to the Go engine chosen above, with the reason recorded.
	var fusion *ffmpeg.FilterGraph
	fusionReason := "fusion skipped (engine override)"
	if opt.engineOverride == nil {
		if !opt.enableFusion {
			fusionReason = "Go render: fusion not enabled (opt-in via EnableFusion)"
		} else if g, ok, reason := fuse(root, rate); ok {
			engine, fusion, fusionReason = EngineFFmpegOnly, g, reason
		} else {
			fusionReason = "Go render: " + reason
		}
	}

	if opt.engineOverride != nil {
		engine = *opt.engineOverride
	}
	// The sequential engine is single-goroutine by definition.
	if engine == EngineSequential {
		workers = 1
	}

	if opt.debugf != nil {
		opt.debugf("render plan: class=%s engine=%s workers=%d (%s)", class, engine, workers, fusionReason)
	}

	reorder := workers
	maxInflight := workers + reorder
	budget := memoryBudget(size, transparent, maxInflight, reorder, len(sources), workers)

	return &Plan{
		Rate:         rate,
		Frames:       frames,
		Transparent:  transparent,
		Size:         size,
		Class:        class,
		Engine:       engine,
		Workers:      workers,
		FusionReason: fusionReason,
		fusion:       fusion,
		ReorderDepth: reorder,
		MaxInflight:  maxInflight,
		Budget:       budget,
		sources:      sources,
	}, nil
}

// classify maps a root's least-linear source access to a render class.
func classify(a video.AccessClass) Class {
	switch a {
	case video.AccessStatic:
		return ClassStatic
	case video.AccessLinear:
		return ClassLinear
	case video.AccessBounded:
		return ClassBounded
	default:
		return ClassRandom
	}
}

// chooseEngine routes to the sequential engine unless the graph is a good fit
// for the pipeline. Random access is always sequential. Bounded-cache is too:
// the intended strategy is a slot table sized to the access window, but neither
// the window size nor a request schedule is modeled yet, and falling back to the
// cooperative driver's seek-to-recover is not that strategy. Until a bounded
// window lands in the capability model, bounded graphs use the oracle. (No node
// currently reports AccessBounded, so this is a forward-looking contract guard,
// not a behavior change.) A single worker or a pipeline-ineligible graph is also
// sequential; everything else (static, linear) uses the pipeline.
func chooseEngine(c Class, workers int, eligible bool) Engine {
	if c == ClassRandom || c == ClassBounded || workers <= 1 || !eligible {
		return EngineSequential
	}
	return EnginePipeline
}

// pipelineEligible reports whether the graph can run on the parallel pipeline.
// Two properties must hold by construction:
//
//   - Single forward pass per source. The decode stage runs one sequential
//     decoder per distinct source and serves frames by index, advancing forward
//     in steady state. If a source is reached by more than one path (e.g. the
//     same opened file feeds two composite layers), a single output frame can
//     demand two different source indices at once, which would make the decoder
//     seek back and forth every frame. Such graphs run sequentially.
//   - Concurrent-safe pixel work. The worker pool calls RenderInto concurrently,
//     so every leaf must be safe to render concurrently. File leaves are the one
//     exception: their only unsafe state is the shared decoder, which the
//     provider replaces, so they are treated as safe here.
func pipelineEligible(root video.VideoClip) bool {
	return maxSourceMultiplicity(root) <= 1 && !hasUnsafeNonFileLeaf(root)
}

// maxSourceMultiplicity returns the largest number of distinct graph paths that
// reach any single source key. A value above 1 means one decoder would have to
// serve interleaved indices for one output frame.
func maxSourceMultiplicity(root video.VideoClip) int {
	counts := make(map[any]int)
	var walk func(video.VideoClip)
	walk = func(c video.VideoClip) {
		if fs, ok := c.(video.FileSource); ok {
			counts[fs.SourceKey()]++
			return
		}
		if p, ok := c.(video.Parent); ok {
			for _, child := range p.Children() {
				walk(child)
			}
		}
	}
	walk(root)
	max := 0
	for _, n := range counts {
		if n > max {
			max = n
		}
	}
	return max
}

// hasUnsafeNonFileLeaf reports whether the graph contains a leaf that is not
// safe to render concurrently and is not a file source. Interior nodes
// (transforms, composites) only inherit their children's safety, so the
// decision is made at the leaves: a non-parallel-safe leaf that the provider
// cannot neutralize (anything but a FileSource) forces the sequential engine.
func hasUnsafeNonFileLeaf(root video.VideoClip) bool {
	found := false
	var walk func(video.VideoClip)
	walk = func(c video.VideoClip) {
		if found {
			return
		}
		if p, ok := c.(video.Parent); ok {
			for _, child := range p.Children() {
				walk(child)
			}
			return
		}
		// Leaf node.
		if _, isFile := c.(video.FileSource); isFile {
			return
		}
		if !c.ParallelSafe() {
			found = true
		}
	}
	walk(root)
	return found
}

// autoWorkers picks a worker count from GOMAXPROCS, reserving a core for the
// FFmpeg encoder. A static graph has no decoder competing for cores.
func autoWorkers(c Class) int {
	n := runtime.GOMAXPROCS(0)
	if c != ClassStatic {
		n -= ffmpegReserve
	}
	if n < 1 {
		n = 1
	}
	return n
}

// scheduledFrames returns the floor frame count: the number of indices i with
// FrameTime(i) < dur. It delegates to video.FrameCount so the planner and the
// reverse map always agree on what the "last frame" index is.
func scheduledFrames(rate clip.Rate, dur clip.Time) int {
	return video.FrameCount(rate, dur)
}

// memoryBudget computes the memory ceiling in bytes: in-flight source frames per
// source, the reorder buffer, the alpha interleave scratch, and per-worker
// transform scratch.
func memoryBudget(size clip.Size, transparent bool, maxInflight, reorder, sourceCount, workers int) int64 {
	rgb := int64(size.W) * int64(size.H) * int64(clip.RGB24.BytesPerPixel())
	frameBytes := rgb
	if transparent {
		frameBytes = int64(size.W) * int64(size.H) * int64(clip.RGBA.BytesPerPixel())
	}
	budget := int64(maxInflight) * frameBytes * int64(max1(sourceCount))
	budget += int64(reorder) * frameBytes
	if transparent {
		budget += int64(size.W) * int64(size.H) * int64(clip.RGBA.BytesPerPixel()) // interleave scratch
	}
	budget += int64(workers) * perWorkerScratchFrames * rgb
	return budget
}

func max1(n int) int {
	if n < 1 {
		return 1
	}
	return n
}
