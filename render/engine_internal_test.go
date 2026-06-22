package render

import (
	"bytes"
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mowshon/moviego/v2/audio"
	"github.com/mowshon/moviego/v2/clip"
	"github.com/mowshon/moviego/v2/composite"
	"github.com/mowshon/moviego/v2/internal/genmedia"
	"github.com/mowshon/moviego/v2/transition"
	"github.com/mowshon/moviego/v2/video"
)

// captureSink records a copy of every frame it is asked to write, keyed by
// output index. Buffers are reused/released by the engine after writeFrame
// returns, so it must copy out of them eagerly.
type captureSink struct {
	frames map[int][]byte
}

func newCaptureSink() *captureSink { return &captureSink{frames: make(map[int][]byte)} }

func (c *captureSink) writeFrame(rf clip.RenderedFrame) error {
	cp := make([]byte, len(rf.RGB.Pix))
	copy(cp, rf.RGB.Pix)
	c.frames[rf.Index] = cp
	return nil
}

// renderWith builds a plan for the given engine and runs it, returning the
// captured per-index pixels. Forcing the engine lets the test treat the
// sequential engine as the correctness oracle and assert the pipeline matches
// it byte-for-byte.
func renderWith(t *testing.T, root video.VideoClip, engine Engine, workers int) map[int][]byte {
	t.Helper()
	plan, err := BuildPlan(root, planOptions{workers: workers, engineOverride: &engine})
	if err != nil {
		t.Fatalf("plan (%s): %v", engine, err)
	}
	sink := newCaptureSink()
	switch engine {
	case EnginePipeline:
		err = runPipeline(context.Background(), root, plan, sink)
	default:
		err = runSequential(context.Background(), root, plan, sink)
	}
	if err != nil {
		t.Fatalf("run (%s): %v", engine, err)
	}
	return sink.frames
}

func assertEngineEquivalence(t *testing.T, root video.VideoClip) {
	t.Helper()
	seq := renderWith(t, root, EngineSequential, 1)
	par := renderWith(t, root, EnginePipeline, 4)

	if len(seq) != len(par) {
		t.Fatalf("frame count: sequential=%d pipeline=%d", len(seq), len(par))
	}
	for idx, want := range seq {
		got, ok := par[idx]
		if !ok {
			t.Fatalf("pipeline missing frame %d", idx)
		}
		if !bytes.Equal(want, got) {
			t.Fatalf("frame %d differs between engines (len seq=%d par=%d)", idx, len(want), len(got))
		}
	}
}

// TestEngineEquivalenceFull renders a whole file through both engines and
// asserts identical pixels. Starting at frame 0 keeps the decoders on the same
// forward path, so any divergence is a pipeline bug rather than seek inaccuracy.
func TestEngineEquivalenceFull(t *testing.T) {
	if !genmedia.Available() {
		t.Skip("ffmpeg not available")
	}
	src, err := genmedia.TestPatternVideo(t.TempDir(), "full.mp4", 64, 48, 2, "25")
	if err != nil {
		t.Fatalf("gen: %v", err)
	}
	n, err := video.OpenFile(context.Background(), src)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer n.Close()

	assertEngineEquivalence(t, n)
}

// TestEngineEquivalenceShortEOF schedules past the source end and asserts both
// engines stop at the same frame with identical pixels for the frames produced.
func TestEngineEquivalenceShortEOF(t *testing.T) {
	if !genmedia.Available() {
		t.Skip("ffmpeg not available")
	}
	src, err := genmedia.TestPatternVideo(t.TempDir(), "short.mp4", 64, 48, 1, "25")
	if err != nil {
		t.Fatalf("gen: %v", err)
	}
	n, err := video.OpenFile(context.Background(), src)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer n.Close()

	extended := n.WithDuration(2 * time.Second).(video.VideoClip)
	assertEngineEquivalence(t, extended)
}

// TestEngineEquivalenceTwoFileComposite layers two distinct files. Each source
// gets its own forward decoder, so the graph is pipeline-eligible; both engines
// must produce identical pixels.
func TestEngineEquivalenceTwoFileComposite(t *testing.T) {
	if !genmedia.Available() {
		t.Skip("ffmpeg not available")
	}
	dir := t.TempDir()
	srcA, err := genmedia.TestPatternVideo(dir, "a.mp4", 64, 48, 2, "25")
	if err != nil {
		t.Fatalf("gen a: %v", err)
	}
	srcB, err := genmedia.TestPatternVideo(dir, "b.mp4", 64, 48, 2, "25")
	if err != nil {
		t.Fatalf("gen b: %v", err)
	}
	a, err := video.OpenFile(context.Background(), srcA)
	if err != nil {
		t.Fatalf("open a: %v", err)
	}
	defer a.Close()
	b, err := video.OpenFile(context.Background(), srcB)
	if err != nil {
		t.Fatalf("open b: %v", err)
	}
	defer b.Close()

	root := composite.New([]composite.CompositeChild{
		{Clip: a, Layer: 0},
		{Clip: b, Layer: 1},
	}, composite.Options{})

	if got := maxSourceMultiplicity(root); got != 1 {
		t.Fatalf("multiplicity = %d, want 1 for distinct files", got)
	}
	assertEngineEquivalence(t, root)
}

// TestPlanReusedSourceRoutesSequential builds a composite whose two layers are
// trims of the *same* opened file. One forward decoder cannot serve both per
// frame, so the planner must route it to the sequential engine; the export must
// then complete (the old design would thrash seeks here).
func TestPlanReusedSourceRoutesSequential(t *testing.T) {
	if !genmedia.Available() {
		t.Skip("ffmpeg not available")
	}
	src, err := genmedia.TestPatternVideo(t.TempDir(), "src.mp4", 64, 48, 3, "25")
	if err != nil {
		t.Fatalf("gen: %v", err)
	}
	n, err := video.OpenFile(context.Background(), src)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer n.Close()

	root := composite.New([]composite.CompositeChild{
		{Clip: video.Subclip(n, 0, time.Second), Layer: 0},
		{Clip: video.Subclip(n, time.Second, 2*time.Second), Layer: 1},
	}, composite.Options{})

	plan, err := BuildPlan(root, planOptions{workers: 4})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if plan.Engine != EngineSequential {
		t.Fatalf("engine = %v, want sequential for a reused source", plan.Engine)
	}

	// And it actually renders without deadlocking on the shared decoder.
	frames := renderWith(t, root, EngineSequential, 1)
	if len(frames) == 0 {
		t.Fatal("no frames rendered")
	}
}

// errAfter is a parallel-safe, file-free leaf that fails on the Nth RenderInto.
// It lets the worker-error path be tested deterministically without ffmpeg.
type errAfter struct {
	calls int64
	fail  int64
}

func (e *errAfter) Start() clip.Time                  { return 0 }
func (e *errAfter) Duration() clip.Time               { return time.Second }
func (e *errAfter) End() clip.Time                    { return time.Second }
func (e *errAfter) WithStart(clip.Time) clip.Clip     { return e }
func (e *errAfter) WithDuration(clip.Time) clip.Clip  { return e }
func (e *errAfter) WithEnd(clip.Time, bool) clip.Clip { return e }
func (e *errAfter) Close() error                      { return nil }
func (e *errAfter) Size() clip.Size                   { return clip.Size{W: 16, H: 16} }
func (e *errAfter) Rate() (clip.Rate, bool)           { return clip.Rate{Num: 30, Den: 1}, true }
func (e *errAfter) HasMask() bool                     { return false }
func (e *errAfter) Audio() audio.AudioClip            { return nil }
func (e *errAfter) ParallelSafe() bool                { return true }
func (e *errAfter) SourceAccess() video.AccessClass   { return video.AccessLinear }
func (e *errAfter) FrameInto(context.Context, clip.Time, *clip.Frame) error {
	return nil
}

func (e *errAfter) MaskInto(context.Context, clip.Time, *clip.Frame) (bool, error) {
	return false, nil
}

var errBoom = errors.New("boom")

func (e *errAfter) RenderInto(context.Context, clip.Time, *clip.Frame, *clip.Frame) (bool, error) {
	if atomic.AddInt64(&e.calls, 1) > e.fail {
		return false, errBoom
	}
	return false, nil
}

// TestPipelinePropagatesWorkerError asserts a worker render error aborts the
// pipeline and surfaces, rather than being swallowed or hanging.
func TestPipelinePropagatesWorkerError(t *testing.T) {
	root := &errAfter{fail: 3}
	plan, err := BuildPlan(root, planOptions{workers: 4})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if plan.Engine != EnginePipeline {
		t.Fatalf("engine = %v, want pipeline", plan.Engine)
	}
	err = runPipeline(context.Background(), root, plan, newCaptureSink())
	if !errors.Is(err, errBoom) {
		t.Fatalf("err = %v, want errBoom", err)
	}
}

// TestEngineEquivalenceTransition renders a two-file crossfade through both
// engines and asserts identical pixels. A bare transition node reads each source
// once (its tail / its head), so it is pipeline-eligible; forcing the pipeline
// against the sequential oracle catches any divergence in the two-input,
// offset-window read path.
func TestEngineEquivalenceTransition(t *testing.T) {
	if !genmedia.Available() {
		t.Skip("ffmpeg not available")
	}
	dir := t.TempDir()
	srcA, err := genmedia.TestPatternVideo(dir, "txa.mp4", 64, 48, 2, "25")
	if err != nil {
		t.Fatalf("gen a: %v", err)
	}
	srcB, err := genmedia.TestPatternVideo(dir, "txb.mp4", 64, 48, 2, "25")
	if err != nil {
		t.Fatalf("gen b: %v", err)
	}
	a, err := video.OpenFile(context.Background(), srcA)
	if err != nil {
		t.Fatalf("open a: %v", err)
	}
	defer a.Close()
	b, err := video.OpenFile(context.Background(), srcB)
	if err != nil {
		t.Fatalf("open b: %v", err)
	}
	defer b.Close()

	root, err := composite.NewTransition(a, b, transition.CrossFade{}, time.Second, composite.CrossfadeEqualPower)
	if err != nil {
		t.Fatalf("new transition: %v", err)
	}
	if got := maxSourceMultiplicity(root); got != 1 {
		t.Fatalf("multiplicity = %d, want 1 for a bare two-file transition", got)
	}
	assertEngineEquivalence(t, root)
}
