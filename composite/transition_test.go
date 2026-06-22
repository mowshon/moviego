package composite_test

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/mowshon/moviego/v2/clip"
	"github.com/mowshon/moviego/v2/composite"
	"github.com/mowshon/moviego/v2/transition"
	"github.com/mowshon/moviego/v2/video"
)

var (
	cRed   = [3]byte{255, 0, 0}
	cGreen = [3]byte{0, 255, 0}
	cBlue  = [3]byte{0, 0, 255}
)

func TestTransitionNodeOverlapWindow(t *testing.T) {
	a := solid(4, 4, cRed, 2*time.Second)
	b := solid(4, 4, cBlue, 2*time.Second)
	n, err := composite.NewTransition(a, b, transition.CrossFade{}, time.Second, composite.CrossfadeEqualPower)
	if err != nil {
		t.Fatalf("new transition: %v", err)
	}
	if d := n.Duration(); d != time.Second {
		t.Errorf("duration = %v, want 1s", d)
	}
	if got := pixel(chainFrame(t, n, 0), 0, 0); got != cRed {
		t.Errorf("p=0 = %v, want red (A)", got)
	}
	if got := pixel(chainFrame(t, n, time.Second), 0, 0); got != cBlue {
		t.Errorf("p=1 = %v, want blue (B)", got)
	}
	if got := pixel(chainFrame(t, n, 500*time.Millisecond), 0, 0); got != [3]byte{127, 0, 128} {
		t.Errorf("p=0.5 = %v, want {127 0 128}", got)
	}
}

func TestTransitionNodeRejectsBadInput(t *testing.T) {
	a := solid(4, 4, cRed, time.Second)
	// Overlap longer than the clip.
	if _, err := composite.NewTransition(a, solid(4, 4, cBlue, time.Second), transition.CrossFade{}, 2*time.Second, 0); err == nil {
		t.Error("expected overlap-too-long error")
	}
	// Size mismatch.
	if _, err := composite.NewTransition(a, solid(8, 8, cBlue, time.Second), transition.CrossFade{}, 500*time.Millisecond, 0); err == nil {
		t.Error("expected size-mismatch error")
	}
	// Nil transition.
	if _, err := composite.NewTransition(a, solid(4, 4, cBlue, time.Second), nil, 500*time.Millisecond, 0); err == nil {
		t.Error("expected nil-transition error")
	}
}

func TestSequenceDurationAndContent(t *testing.T) {
	a := solid(4, 4, cRed, 2*time.Second)
	b := solid(4, 4, cGreen, 2*time.Second)
	c := solid(4, 4, cBlue, 2*time.Second)
	step := func() *composite.TransitionSpec {
		return &composite.TransitionSpec{T: transition.CrossFade{}, Dur: 500 * time.Millisecond}
	}
	seq, err := composite.Sequence([]video.VideoClip{a, b, c}, []*composite.TransitionSpec{step(), step()})
	if err != nil {
		t.Fatalf("sequence: %v", err)
	}
	// 6s of media minus two 0.5s overlaps = 5s.
	if d := seq.Duration(); d != 5*time.Second {
		t.Errorf("duration = %v, want 5s", d)
	}
	if got := pixel(chainFrame(t, seq, 0), 0, 0); got != cRed {
		t.Errorf("start = %v, want red", got)
	}
	if got := pixel(chainFrame(t, seq, 4900*time.Millisecond), 0, 0); got != cBlue {
		t.Errorf("end = %v, want blue", got)
	}
	// Midpoint of the first crossfade (body A' is 1.5s, overlap is 1.5s..2.0s).
	mid := pixel(chainFrame(t, seq, 1750*time.Millisecond), 0, 0)
	if mid == cRed || mid == cGreen {
		t.Errorf("mid-transition = %v, want a red/green blend", mid)
	}
}

func TestSequenceHardCut(t *testing.T) {
	a := solid(4, 4, cRed, time.Second)
	b := solid(4, 4, cBlue, time.Second)
	// A nil spec is a hard cut: no overlap, full durations.
	seq, err := composite.Sequence([]video.VideoClip{a, b}, []*composite.TransitionSpec{nil})
	if err != nil {
		t.Fatalf("sequence: %v", err)
	}
	if d := seq.Duration(); d != 2*time.Second {
		t.Errorf("duration = %v, want 2s (no overlap)", d)
	}
}

func TestSequenceOverlapEqualsClip(t *testing.T) {
	// A crossfade as long as both equal clips consumes them entirely: the bodies
	// vanish and the timeline is the single overlap node (duration == overlap),
	// not a replay of either full clip.
	a := solid(4, 4, cRed, time.Second)
	b := solid(4, 4, cBlue, time.Second)
	step := &composite.TransitionSpec{T: transition.CrossFade{}, Dur: time.Second}
	seq, err := composite.Sequence([]video.VideoClip{a, b}, []*composite.TransitionSpec{step})
	if err != nil {
		t.Fatalf("sequence: %v", err)
	}
	if d := seq.Duration(); d != time.Second {
		t.Errorf("duration = %v, want 1s (just the overlap)", d)
	}
	if got := pixel(chainFrame(t, seq, 0), 0, 0); got != cRed {
		t.Errorf("start = %v, want red (A)", got)
	}
}

func TestSequenceSingleClip(t *testing.T) {
	a := solid(4, 4, cRed, time.Second)
	seq, err := composite.Sequence([]video.VideoClip{a}, nil)
	if err != nil {
		t.Fatalf("sequence: %v", err)
	}
	if seq != a {
		t.Error("single-clip sequence should return the clip unchanged")
	}
}

func TestSequenceInteriorTooShort(t *testing.T) {
	a := solid(4, 4, cRed, 2*time.Second)
	b := solid(4, 4, cGreen, 500*time.Millisecond) // feeds 0.5s on both sides
	c := solid(4, 4, cBlue, 2*time.Second)
	step := &composite.TransitionSpec{T: transition.CrossFade{}, Dur: 500 * time.Millisecond}
	if _, err := composite.Sequence([]video.VideoClip{a, b, c}, []*composite.TransitionSpec{step, step}); err == nil {
		t.Error("expected error: interior clip too short for both overlaps")
	}
}

func TestSequenceCustomTransitionRenders(t *testing.T) {
	// A custom transition must actually run when the assembled overlap frame is
	// rendered — not merely build. The Func paints a sentinel color, and we read
	// it back from a frame inside the overlap window.
	var called int
	sentinel := [3]byte{7, 8, 9}
	custom := transition.Func("sentinel", func(p float64, dst, a, b *clip.Frame) {
		called++
		for i := 0; i+2 < len(dst.Pix); i += 3 {
			dst.Pix[i], dst.Pix[i+1], dst.Pix[i+2] = sentinel[0], sentinel[1], sentinel[2]
		}
	})
	a := solid(4, 4, cRed, 2*time.Second)
	b := solid(4, 4, cBlue, 2*time.Second)
	seq, err := composite.Sequence([]video.VideoClip{a, b},
		[]*composite.TransitionSpec{{T: custom, Dur: time.Second}})
	if err != nil {
		t.Fatalf("sequence: %v", err)
	}
	// Body A is [0,1s); the overlap is [1s,2s). Sample inside the overlap.
	if got := pixel(chainFrame(t, seq, 1500*time.Millisecond), 0, 0); got != sentinel {
		t.Fatalf("overlap frame = %v, want sentinel %v (custom transition not run)", got, sentinel)
	}
	if called == 0 {
		t.Fatal("custom transition Frame was never invoked")
	}
}

func TestTransitionMaskFollowsShapeEndToEnd(t *testing.T) {
	// Opaque A handing over to a fully transparent B through a wipe: the rendered
	// alpha sidecar must follow the wipe edge (revealed side transparent, other
	// side opaque), not a uniform half-transparency.
	a := solid(8, 8, cRed, 2*time.Second)
	b := masked(8, 8, cBlue, 0, 2*time.Second) // alpha 0 everywhere
	n, err := composite.NewTransition(a, b, transition.Wipe{Dir: transition.Right}, time.Second, 0)
	if err != nil {
		t.Fatalf("new transition: %v", err)
	}
	if !n.HasMask() {
		t.Fatal("transition over a transparent input should carry a mask")
	}
	rgb := clip.NewFrame(8, 8, clip.RGB24)
	alpha := clip.NewFrame(8, 8, clip.Gray8)
	ok, err := n.RenderInto(context.Background(), 500*time.Millisecond, rgb, alpha)
	if err != nil || !ok {
		t.Fatalf("render: ok=%v err=%v", ok, err)
	}
	if got := alpha.Pix[0]; got != 0 { // left edge: revealed B, transparent
		t.Errorf("left alpha = %d, want 0 (revealed transparent B)", got)
	}
	if got := alpha.Pix[7]; got != 255 { // right edge: A, opaque
		t.Errorf("right alpha = %d, want 255 (opaque A)", got)
	}
}

func TestTransitionAudioCrossfade(t *testing.T) {
	rate := 48000
	a := video.WithAudio(solid(4, 4, cRed, 2*time.Second), newMarker(1, 2*time.Second, rate))
	b := video.WithAudio(solid(4, 4, cBlue, 2*time.Second), newMarker(1, 2*time.Second, rate))
	n, err := composite.NewTransition(a, b, transition.CrossFade{}, time.Second, composite.CrossfadeEqualPower)
	if err != nil {
		t.Fatalf("new transition: %v", err)
	}
	au := n.Audio()
	if au == nil {
		t.Fatal("transition audio is nil")
	}
	buf := &clip.AudioBuffer{}
	if err := au.SamplesInto(context.Background(), 0, rate, buf); err != nil {
		t.Fatalf("samples: %v", err)
	}
	// At the overlap midpoint both equal-power gains are ~0.707, so two identical
	// unit signals sum to ~sqrt(2).
	if mid := buf.Samples[rate/2]; math.Abs(float64(mid)-math.Sqrt2) > 0.02 {
		t.Errorf("midpoint sample = %f, want ~%f", mid, math.Sqrt2)
	}
}
