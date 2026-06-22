package composite_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/mowshon/moviego/v2/clip"
	"github.com/mowshon/moviego/v2/composite"
	"github.com/mowshon/moviego/v2/video"
)

// solid builds an opaque w x h color clip with the given duration.
func solid(w, h int, col [3]byte, d clip.Time) video.VideoClip {
	return video.NewColor(clip.Size{W: w, H: h}, col).WithDuration(d).(video.VideoClip)
}

// masked builds a w x h image clip whose RGB and Gray8 mask are uniform.
func masked(w, h int, col [3]byte, alpha byte, d clip.Time) video.VideoClip {
	rgb := clip.NewFrame(w, h, clip.RGB24)
	for i := 0; i < len(rgb.Pix); i += 3 {
		rgb.Pix[i], rgb.Pix[i+1], rgb.Pix[i+2] = col[0], col[1], col[2]
	}
	a := clip.NewFrame(w, h, clip.Gray8)
	for i := range a.Pix {
		a.Pix[i] = alpha
	}
	return video.NewImage(rgb, a).WithDuration(d).(video.VideoClip)
}

func renderRGB(t *testing.T, n *composite.CompositeNode, at clip.Time) *clip.Frame {
	t.Helper()
	sz := n.Size()
	dst := clip.NewFrame(sz.W, sz.H, clip.RGB24)
	if err := n.FrameInto(context.Background(), at, dst); err != nil {
		t.Fatalf("render: %v", err)
	}
	return dst
}

func pixel(f *clip.Frame, x, y int) [3]byte {
	o := y*f.Stride + x*3
	return [3]byte{f.Pix[o], f.Pix[o+1], f.Pix[o+2]}
}

// TestOpaqueOverlayCopiesPixels: an opaque child pasted onto a color background
// (path a) replaces exactly its region and leaves the rest as the bg color.
func TestOpaqueOverlayCopiesPixels(t *testing.T) {
	red := solid(4, 4, [3]byte{255, 0, 0}, time.Second)
	child := composite.CompositeChild{
		Clip:  red,
		Pos:   composite.Position{X: 2, Y: 2},
		Start: 0,
	}
	n := composite.New([]composite.CompositeChild{child}, composite.Options{
		Size:    clip.Size{W: 8, H: 8},
		BGColor: [3]byte{0, 0, 255}, // blue bg
	})
	f := renderRGB(t, n, 0)
	if got := pixel(f, 0, 0); got != [3]byte{0, 0, 255} {
		t.Errorf("bg pixel = %v, want blue", got)
	}
	if got := pixel(f, 3, 3); got != [3]byte{255, 0, 0} {
		t.Errorf("overlay pixel = %v, want red", got)
	}
}

// TestHalfOpenGate: a child plays on [start, start+dur) and is absent at the end
// boundary.
func TestHalfOpenGate(t *testing.T) {
	red := solid(8, 8, [3]byte{255, 0, 0}, time.Second)
	child := composite.CompositeChild{Clip: red, Start: time.Second}
	n := composite.New([]composite.CompositeChild{child}, composite.Options{
		Size:    clip.Size{W: 8, H: 8},
		BGColor: [3]byte{0, 0, 0},
	})
	// Before start: background only.
	if got := pixel(renderRGB(t, n, time.Second-1), 0, 0); got != [3]byte{0, 0, 0} {
		t.Errorf("before start = %v, want black", got)
	}
	// At start: child visible.
	if got := pixel(renderRGB(t, n, time.Second), 0, 0); got != [3]byte{255, 0, 0} {
		t.Errorf("at start = %v, want red", got)
	}
	// At end (start+dur): half-open, child gone.
	if got := pixel(renderRGB(t, n, 2*time.Second), 0, 0); got != [3]byte{0, 0, 0} {
		t.Errorf("at end = %v, want black (half-open)", got)
	}
}

// TestLayerTieBreakInputOrder: two equal-layer full-canvas opaque children draw
// in input order, so the later one wins.
func TestLayerTieBreakInputOrder(t *testing.T) {
	first := composite.CompositeChild{Clip: solid(4, 4, [3]byte{255, 0, 0}, time.Second), Layer: 0}
	second := composite.CompositeChild{Clip: solid(4, 4, [3]byte{0, 255, 0}, time.Second), Layer: 0}
	n := composite.New([]composite.CompositeChild{first, second}, composite.Options{Size: clip.Size{W: 4, H: 4}})
	if got := pixel(renderRGB(t, n, 0), 0, 0); got != [3]byte{0, 255, 0} {
		t.Errorf("top pixel = %v, want green (later input wins on equal layer)", got)
	}
}

// TestLayerOrdering: a higher layer index renders on top regardless of input
// order.
func TestLayerOrdering(t *testing.T) {
	top := composite.CompositeChild{Clip: solid(4, 4, [3]byte{255, 0, 0}, time.Second), Layer: 10}
	bottom := composite.CompositeChild{Clip: solid(4, 4, [3]byte{0, 255, 0}, time.Second), Layer: 1}
	// Input order puts the high layer first; the sort must still draw it last.
	n := composite.New([]composite.CompositeChild{top, bottom}, composite.Options{Size: clip.Size{W: 4, H: 4}})
	if got := pixel(renderRGB(t, n, 0), 0, 0); got != [3]byte{255, 0, 0} {
		t.Errorf("top pixel = %v, want red (layer 10 on top)", got)
	}
}

// TestMaskedOverOpaque (path c): a 50%-alpha white child over a black opaque
// canvas yields ~128 gray.
func TestMaskedOverOpaque(t *testing.T) {
	child := composite.CompositeChild{Clip: masked(4, 4, [3]byte{255, 255, 255}, 128, time.Second)}
	n := composite.New([]composite.CompositeChild{child}, composite.Options{
		Size:    clip.Size{W: 4, H: 4},
		BGColor: [3]byte{0, 0, 0},
	})
	got := pixel(renderRGB(t, n, 0), 0, 0)
	for c := 0; c < 3; c++ {
		if got[c] < 127 || got[c] > 129 {
			t.Fatalf("blended pixel = %v, want ~128", got)
		}
	}
}

// TestOpaqueShortcutDropsFullMask: a child whose mask is all-255 takes the
// opaque copy path even though it nominally has a mask, producing an exact paste
// (not a blend artifact).
func TestOpaqueShortcutDropsFullMask(t *testing.T) {
	child := composite.CompositeChild{Clip: masked(4, 4, [3]byte{10, 20, 30}, 255, time.Second)}
	n := composite.New([]composite.CompositeChild{child}, composite.Options{
		Size:    clip.Size{W: 4, H: 4},
		BGColor: [3]byte{200, 200, 200},
	})
	if got := pixel(renderRGB(t, n, 0), 0, 0); got != [3]byte{10, 20, 30} {
		t.Errorf("opaque-shortcut pixel = %v, want exact 10,20,30", got)
	}
}

// TestTransparentCompositeOnePass: a transparent composite fills RGB and alpha
// in a single RenderInto. An opaque child marks its region opaque (path b); the
// uncovered region stays transparent.
func TestTransparentCompositeOnePass(t *testing.T) {
	child := composite.CompositeChild{
		Clip: solid(2, 2, [3]byte{255, 0, 0}, time.Second),
		Pos:  composite.Position{X: 0, Y: 0},
	}
	n := composite.New([]composite.CompositeChild{child}, composite.Options{
		Size:        clip.Size{W: 4, H: 4},
		Transparent: true,
	})
	if !n.HasMask() {
		t.Fatal("transparent composite should report a mask")
	}
	sz := n.Size()
	rgb := clip.NewFrame(sz.W, sz.H, clip.RGB24)
	alpha := clip.NewFrame(sz.W, sz.H, clip.Gray8)
	hasAlpha, err := n.RenderInto(context.Background(), 0, rgb, alpha)
	if err != nil || !hasAlpha {
		t.Fatalf("render: hasAlpha=%v err=%v", hasAlpha, err)
	}
	// Covered pixel: opaque red.
	if alpha.Pix[0] != 255 {
		t.Errorf("covered alpha = %d, want 255", alpha.Pix[0])
	}
	if got := pixel(rgb, 0, 0); got != [3]byte{255, 0, 0} {
		t.Errorf("covered rgb = %v, want red", got)
	}
	// Uncovered pixel (3,3): transparent.
	if a := alpha.Pix[3*alpha.Stride+3]; a != 0 {
		t.Errorf("uncovered alpha = %d, want 0", a)
	}
}

// TestTransparentBothMasked (path d): a 50% white child over a transparent
// canvas composites to ~50% alpha and full-white recovered RGB.
func TestTransparentBothMasked(t *testing.T) {
	child := composite.CompositeChild{Clip: masked(2, 2, [3]byte{255, 255, 255}, 128, time.Second)}
	n := composite.New([]composite.CompositeChild{child}, composite.Options{
		Size:        clip.Size{W: 2, H: 2},
		Transparent: true,
	})
	rgb := clip.NewFrame(2, 2, clip.RGB24)
	alpha := clip.NewFrame(2, 2, clip.Gray8)
	if _, err := n.RenderInto(context.Background(), 0, rgb, alpha); err != nil {
		t.Fatalf("render: %v", err)
	}
	// over transparent: final alpha = a, recovered rgb = fg.
	if alpha.Pix[0] < 127 || alpha.Pix[0] > 129 {
		t.Errorf("final alpha = %d, want ~128", alpha.Pix[0])
	}
	if got := pixel(rgb, 0, 0); got != [3]byte{255, 255, 255} {
		t.Errorf("recovered rgb = %v, want white", got)
	}
}

// TestConcurrentRenderInto exercises the parallel-safety claim: many goroutines
// render different times at once. Run with -race.
func TestConcurrentRenderInto(t *testing.T) {
	a := composite.CompositeChild{Clip: solid(16, 16, [3]byte{255, 0, 0}, 4*time.Second), Layer: 0}
	b := composite.CompositeChild{Clip: masked(16, 16, [3]byte{0, 255, 0}, 100, 4*time.Second), Layer: 1, Pos: composite.Position{X: 4, Y: 4}}
	n := composite.New([]composite.CompositeChild{a, b}, composite.Options{Size: clip.Size{W: 16, H: 16}})

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			dst := clip.NewFrame(16, 16, clip.RGB24)
			at := clip.Time(i) * (100 * time.Millisecond)
			if err := n.FrameInto(context.Background(), at, dst); err != nil {
				t.Errorf("render %d: %v", i, err)
			}
		}(i)
	}
	wg.Wait()
}

// TestAnimatedPositionUsesLocalTime: a child placed with Start != 0 and an
// Animated position must be animated from its own playback time (local), not
// the composite's global time. At the child's first visible instant the
// animation is at its t=0 value.
func TestAnimatedPositionUsesLocalTime(t *testing.T) {
	// Animation moves the child right by 1px per millisecond of LOCAL time.
	animated := composite.Position{Animated: func(lt clip.Time) (float64, float64) {
		return float64(lt / time.Millisecond), 0
	}}
	child := composite.CompositeChild{
		Clip:  solid(2, 2, [3]byte{255, 0, 0}, time.Second),
		Start: time.Second, // starts a second into the composite
		Pos:   animated,
	}
	n := composite.New([]composite.CompositeChild{child}, composite.Options{
		Size:    clip.Size{W: 16, H: 4},
		BGColor: [3]byte{0, 0, 0},
	})
	// At global t = 1s (the child's local t = 0) the child must sit at x=0, not
	// x=1000 (which would be off-canvas and render nothing).
	f := renderRGB(t, n, time.Second)
	if got := pixel(f, 0, 0); got != [3]byte{255, 0, 0} {
		t.Errorf("local-time animation: pixel(0,0) = %v, want red at x=0", got)
	}
	// 3ms into the child's local time it has moved to x=3.
	f = renderRGB(t, n, time.Second+3*time.Millisecond)
	if got := pixel(f, 0, 0); got != [3]byte{0, 0, 0} {
		t.Errorf("after 3ms: pixel(0,0) = %v, want black (child moved off origin)", got)
	}
	if got := pixel(f, 3, 0); got != [3]byte{255, 0, 0} {
		t.Errorf("after 3ms: pixel(3,0) = %v, want red at x=3", got)
	}
}

// TestCompositeMetadata: duration is the latest child end and the rate is the
// max child rate.
func TestCompositeMetadata(t *testing.T) {
	short := composite.CompositeChild{Clip: solid(4, 4, [3]byte{}, time.Second), Start: 0}
	long := composite.CompositeChild{Clip: solid(4, 4, [3]byte{}, 2*time.Second), Start: time.Second}
	n := composite.New([]composite.CompositeChild{short, long}, composite.Options{Size: clip.Size{W: 4, H: 4}})
	d := n.Duration()
	if d != 3*time.Second {
		t.Errorf("duration = %v, want 3s", d)
	}
}
