package render

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/mowshon/moviego/v2/audio"
	"github.com/mowshon/moviego/v2/clip"
	"github.com/mowshon/moviego/v2/composite"
	"github.com/mowshon/moviego/v2/transition"
	"github.com/mowshon/moviego/v2/video"
)

// fakeClip is a configurable leaf video clip for planner tests. It renders
// nothing: BuildPlan only inspects timeline/capability metadata, never pixels.
type fakeClip struct {
	dur      clip.Time
	hasDur   bool
	rate     clip.Rate
	hasRate  bool
	size     clip.Size
	access   video.AccessClass
	parallel bool
}

func (c *fakeClip) Start() clip.Time    { return 0 }
func (c *fakeClip) Duration() clip.Time { return clip.DurationOr(c.dur, c.hasDur) }
func (c *fakeClip) End() clip.Time      { return clip.EndOr(0, c.dur, c.hasDur) }
func (c *fakeClip) WithStart(clip.Time) clip.Clip {
	return c
}
func (c *fakeClip) WithDuration(d clip.Time) clip.Clip { return c }
func (c *fakeClip) WithEnd(clip.Time, bool) clip.Clip  { return c }
func (c *fakeClip) Close() error                       { return nil }
func (c *fakeClip) Size() clip.Size                    { return c.size }
func (c *fakeClip) Rate() (clip.Rate, bool)            { return c.rate, c.hasRate }
func (c *fakeClip) HasMask() bool                      { return false }
func (c *fakeClip) Audio() audio.AudioClip             { return nil }
func (c *fakeClip) ParallelSafe() bool                 { return c.parallel }
func (c *fakeClip) SourceAccess() video.AccessClass    { return c.access }
func (c *fakeClip) RenderInto(context.Context, clip.Time, *clip.Frame, *clip.Frame) (bool, error) {
	return false, nil
}
func (c *fakeClip) FrameInto(context.Context, clip.Time, *clip.Frame) error { return nil }
func (c *fakeClip) MaskInto(context.Context, clip.Time, *clip.Frame) (bool, error) {
	return false, nil
}

// fakeFile is a fakeClip that also presents as a file source (so the planner's
// provider exemption and duplicate-source detection can be exercised). Its
// SourceAccess is linear and it is not parallel-safe, mirroring a real decoder.
type fakeFile struct {
	fakeClip
	key any
}

func newFakeFile(key any, dur clip.Time) *fakeFile {
	return &fakeFile{
		fakeClip: fakeClip{
			dur: dur, hasDur: true,
			rate: clip.Rate{Num: 25, Den: 1}, hasRate: true,
			size: clip.Size{W: 64, H: 48}, access: video.AccessLinear, parallel: false,
		},
		key: key,
	}
}

func (f *fakeFile) SourceKey() any        { return f.key }
func (f *fakeFile) SourcePath() string    { return "fake" }
func (f *fakeFile) SourceSize() clip.Size { return f.size }
func (f *fakeFile) SourceRate() clip.Rate { return f.rate }

// fakeParent wraps children, exposing them for graph traversal. Its metadata is
// taken from the first child and its access is the max child access (mirroring
// the real CompositeNode), which is exactly the shallow signal the planner must
// not trust alone.
type fakeParent struct {
	fakeClip
	children []video.VideoClip
}

func newFakeParent(children ...video.VideoClip) *fakeParent {
	first := children[0]
	r, hr := first.Rate()
	d := first.Duration()
	hd := clip.Finite(d)
	acc := video.AccessStatic
	for _, ch := range children {
		if a := ch.SourceAccess(); a > acc {
			acc = a
		}
	}
	return &fakeParent{
		fakeClip: fakeClip{
			dur: d, hasDur: hd, rate: r, hasRate: hr,
			size: first.Size(), access: acc, parallel: true,
		},
		children: children,
	}
}

func (p *fakeParent) Children() []video.VideoClip { return p.children }

func TestBuildPlanRejectsNoDuration(t *testing.T) {
	c := &fakeClip{hasDur: false, rate: clip.Rate{Num: 30, Den: 1}, hasRate: true}
	if _, err := BuildPlan(c, planOptions{}); !errors.Is(err, clip.ErrNoDuration) {
		t.Fatalf("err = %v, want ErrNoDuration", err)
	}
}

func TestBuildPlanRejectsNoRate(t *testing.T) {
	c := &fakeClip{dur: time.Second, hasDur: true, hasRate: false}
	if _, err := BuildPlan(c, planOptions{}); !errors.Is(err, clip.ErrNoRate) {
		t.Fatalf("err = %v, want ErrNoRate", err)
	}
}

func TestBuildPlanRejectsZeroDenominatorRate(t *testing.T) {
	c := &fakeClip{dur: time.Second, hasDur: true, rate: clip.Rate{Num: 30, Den: 1}, hasRate: true}
	if _, err := BuildPlan(c, planOptions{rate: clip.Rate{Num: 30, Den: 0}}); !errors.Is(err, clip.ErrNoRate) {
		t.Fatalf("err = %v, want ErrNoRate", err)
	}
}

func TestSingleFileSourcePipelines(t *testing.T) {
	// A lone file leaf is not parallel-safe, but the provider neutralizes its
	// decoder, so a multi-worker linear graph must still pick the pipeline.
	f := newFakeFile("a", 2*time.Second)
	plan, err := BuildPlan(f, planOptions{workers: 4})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if plan.Engine != EnginePipeline {
		t.Errorf("engine = %v, want pipeline", plan.Engine)
	}
	if plan.Class != ClassLinear {
		t.Errorf("class = %v, want linear", plan.Class)
	}
}

func TestSingleWorkerForcesSequential(t *testing.T) {
	f := newFakeFile("a", 2*time.Second)
	plan, err := BuildPlan(f, planOptions{workers: 1})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if plan.Engine != EngineSequential {
		t.Errorf("engine = %v, want sequential", plan.Engine)
	}
	if plan.Workers != 1 {
		t.Errorf("workers = %d, want 1", plan.Workers)
	}
}

func TestRandomAccessForcesSequential(t *testing.T) {
	c := &fakeClip{
		dur: time.Second, hasDur: true,
		rate: clip.Rate{Num: 30, Den: 1}, hasRate: true,
		size: clip.Size{W: 16, H: 16}, access: video.AccessRandom, parallel: true,
	}
	plan, err := BuildPlan(c, planOptions{workers: 8})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if plan.Engine != EngineSequential {
		t.Errorf("engine = %v, want sequential for random access", plan.Engine)
	}
}

func TestBoundedAccessForcesSequential(t *testing.T) {
	// Bounded-cache has no window model yet, so it must use the oracle rather
	// than the pipeline (which would lean on the driver's seek-to-recover).
	c := &fakeClip{
		dur: time.Second, hasDur: true,
		rate: clip.Rate{Num: 30, Den: 1}, hasRate: true,
		size: clip.Size{W: 16, H: 16}, access: video.AccessBounded, parallel: true,
	}
	plan, err := BuildPlan(c, planOptions{workers: 8})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if plan.Class != ClassBounded {
		t.Errorf("class = %v, want bounded", plan.Class)
	}
	if plan.Engine != EngineSequential {
		t.Errorf("engine = %v, want sequential for bounded access", plan.Engine)
	}
}

func TestDuplicateSourceForcesSequential(t *testing.T) {
	// Two graph paths reach the same source key: a single forward decoder can't
	// serve both per output frame, so the planner must avoid the pipeline even
	// though every individual node reports linear access.
	a := newFakeFile("shared", 2*time.Second)
	b := newFakeFile("shared", 2*time.Second)
	root := newFakeParent(a, b)
	if got := maxSourceMultiplicity(root); got != 2 {
		t.Fatalf("multiplicity = %d, want 2", got)
	}
	plan, err := BuildPlan(root, planOptions{workers: 8})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if plan.Engine != EngineSequential {
		t.Errorf("engine = %v, want sequential for a reused source", plan.Engine)
	}
}

func TestDistinctSourcesPipeline(t *testing.T) {
	// Two different files in one composite are each served by their own
	// forward decoder, so the parallel pipeline is safe.
	a := newFakeFile("a", 2*time.Second)
	b := newFakeFile("b", 2*time.Second)
	root := newFakeParent(a, b)
	plan, err := BuildPlan(root, planOptions{workers: 8})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if plan.Engine != EnginePipeline {
		t.Errorf("engine = %v, want pipeline for distinct sources", plan.Engine)
	}
	if len(plan.sources) != 2 {
		t.Errorf("sources = %d, want 2", len(plan.sources))
	}
}

func TestUnsafeNonFileLeafForcesSequential(t *testing.T) {
	// A non-file leaf that is not parallel-safe cannot be neutralized by the
	// provider; the worker pool would race it, so it must run sequentially.
	unsafe := &fakeClip{
		dur: time.Second, hasDur: true,
		rate: clip.Rate{Num: 30, Den: 1}, hasRate: true,
		size: clip.Size{W: 16, H: 16}, access: video.AccessLinear, parallel: false,
	}
	root := newFakeParent(unsafe)
	plan, err := BuildPlan(root, planOptions{workers: 8})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if plan.Engine != EngineSequential {
		t.Errorf("engine = %v, want sequential for an unsafe non-file leaf", plan.Engine)
	}
}

func TestScheduledFramesBoundary(t *testing.T) {
	rate := clip.Rate{Num: 25, Den: 1}
	cases := []struct {
		dur  clip.Time
		want int
	}{
		{0, 0},
		{time.Second, 25}, // frames at t=0..0.96s
		{2 * time.Second, 50},
		{40 * time.Millisecond, 1},           // exactly one frame interval
		{39 * time.Millisecond, 1},           // frame 0 only (FrameTime(0)=0 < 39ms)
		{time.Second + time.Millisecond, 26}, // one past the boundary
	}
	for _, tc := range cases {
		if got := scheduledFrames(rate, tc.dur); got != tc.want {
			t.Errorf("scheduledFrames(25fps, %v) = %d, want %d", tc.dur, got, tc.want)
		}
	}
}

func TestMemoryBudgetScales(t *testing.T) {
	size := clip.Size{W: 320, H: 240}
	small := memoryBudget(size, false, 4, 2, 1, 2)
	if small <= 0 {
		t.Fatalf("budget = %d, want > 0", small)
	}
	// More in-flight frames must not reduce the ceiling.
	more := memoryBudget(size, false, 8, 4, 1, 4)
	if more <= small {
		t.Errorf("budget did not grow with inflight: %d <= %d", more, small)
	}
	// Transparency widens every frame from RGB to RGBA, raising the ceiling.
	rgba := memoryBudget(size, true, 4, 2, 1, 2)
	if rgba <= small {
		t.Errorf("transparent budget %d not larger than opaque %d", rgba, small)
	}
}

// TestTransitionTimelineForcesSequential locks in the §2.5 finding: even with
// two *distinct* file sources, a crossfade timeline drops to the sequential
// engine, because each clip is reached by both its straight-through body and the
// shared transition (multiplicity >= 2 per source).
func TestTransitionTimelineForcesSequential(t *testing.T) {
	a := newFakeFile("a", 2*time.Second)
	b := newFakeFile("b", 2*time.Second)
	seq, err := composite.Sequence([]video.VideoClip{a, b},
		[]*composite.TransitionSpec{{T: transition.CrossFade{}, Dur: time.Second}})
	if err != nil {
		t.Fatalf("sequence: %v", err)
	}
	if got := maxSourceMultiplicity(seq); got < 2 {
		t.Fatalf("multiplicity = %d, want >= 2 (body + transition share each source)", got)
	}
	plan, err := BuildPlan(seq, planOptions{workers: 4})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if plan.Engine != EngineSequential {
		t.Errorf("engine = %v, want sequential for a shared-source transition timeline", plan.Engine)
	}
}

// TestHardCutConcatStaysPipeline is the contrast: the same two sources joined by
// a hard cut (no transition) are each reached once, so the timeline remains
// pipeline-eligible — isolating the transition node as the cause of the
// sequential drop above.
func TestHardCutConcatStaysPipeline(t *testing.T) {
	a := newFakeFile("a", 2*time.Second)
	b := newFakeFile("b", 2*time.Second)
	seq, err := composite.Sequence([]video.VideoClip{a, b}, []*composite.TransitionSpec{nil})
	if err != nil {
		t.Fatalf("sequence: %v", err)
	}
	if got := maxSourceMultiplicity(seq); got != 1 {
		t.Fatalf("multiplicity = %d, want 1 for a hard-cut concat", got)
	}
	plan, err := BuildPlan(seq, planOptions{workers: 4})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if plan.Engine != EnginePipeline {
		t.Errorf("engine = %v, want pipeline for distinct-source hard cut", plan.Engine)
	}
}
