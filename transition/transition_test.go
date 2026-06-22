package transition_test

import (
	"testing"

	"github.com/mowshon/moviego/v2/clip"
	"github.com/mowshon/moviego/v2/ease"
	"github.com/mowshon/moviego/v2/transition"
)

var (
	red  = [3]byte{255, 0, 0}
	blue = [3]byte{0, 0, 255}
)

// frame builds a solid w x h RGB24 frame filled with col.
func frame(w, h int, col [3]byte) *clip.Frame {
	f := clip.NewFrame(w, h, clip.RGB24)
	for i := 0; i < len(f.Pix); i += 3 {
		f.Pix[i], f.Pix[i+1], f.Pix[i+2] = col[0], col[1], col[2]
	}
	return f
}

func at(f *clip.Frame, x, y int) [3]byte {
	o := y*f.Stride + x*3
	return [3]byte{f.Pix[o], f.Pix[o+1], f.Pix[o+2]}
}

// run renders tr over solid red->blue frames of the given size at progress p.
func run(tr transition.Transition, w, h int, p float64) *clip.Frame {
	a, b := frame(w, h, red), frame(w, h, blue)
	dst := frame(w, h, [3]byte{})
	tr.Frame(p, dst, a, b)
	return dst
}

// assertEndpoints checks the universal contract: p=0 is fully A, p=1 is fully B,
// across the whole frame.
func assertEndpoints(t *testing.T, tr transition.Transition, w, h int) {
	t.Helper()
	z := run(tr, w, h, 0)
	one := run(tr, w, h, 1)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if got := at(z, x, y); got != red {
				t.Fatalf("%s p=0 at (%d,%d) = %v, want red (A)", tr.Name(), x, y, got)
			}
			if got := at(one, x, y); got != blue {
				t.Fatalf("%s p=1 at (%d,%d) = %v, want blue (B)", tr.Name(), x, y, got)
			}
		}
	}
}

func TestCrossFade(t *testing.T) {
	tr := transition.CrossFade{}
	assertEndpoints(t, tr, 4, 4)
	// Midpoint is the rounded per-channel average of red and blue.
	if got := at(run(tr, 4, 4, 0.5), 0, 0); got != [3]byte{127, 0, 128} {
		t.Errorf("crossfade p=0.5 = %v, want {127 0 128}", got)
	}
}

func TestCrossFadeMask(t *testing.T) {
	a := clip.NewFrame(2, 2, clip.Gray8)
	b := clip.NewFrame(2, 2, clip.Gray8)
	for i := range a.Pix {
		a.Pix[i], b.Pix[i] = 0, 200
	}
	dst := clip.NewFrame(2, 2, clip.Gray8)
	transition.CrossFade{}.FrameMask(0.5, dst, a, b)
	if dst.Pix[0] != 100 {
		t.Errorf("mask crossfade p=0.5 = %d, want 100", dst.Pix[0])
	}
}

func TestFadeThroughBlack(t *testing.T) {
	tr := transition.FadeThroughColor{} // zero color = black
	assertEndpoints(t, tr, 4, 4)
	// Fully the color at the midpoint.
	if got := at(run(tr, 4, 4, 0.5), 1, 1); got != [3]byte{0, 0, 0} {
		t.Errorf("fade-through-black p=0.5 = %v, want black", got)
	}
}

func TestDissolve(t *testing.T) {
	tr := transition.Dissolve{}
	assertEndpoints(t, tr, 8, 8)
	// The midpoint stipple shows both A and B pixels.
	f := run(tr, 8, 8, 0.5)
	var sawA, sawB bool
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			switch at(f, x, y) {
			case red:
				sawA = true
			case blue:
				sawB = true
			}
		}
	}
	if !sawA || !sawB {
		t.Errorf("dissolve p=0.5 sawA=%v sawB=%v, want both", sawA, sawB)
	}
}

func TestWipeRight(t *testing.T) {
	tr := transition.Wipe{Dir: transition.Right}
	assertEndpoints(t, tr, 8, 8)
	f := run(tr, 8, 8, 0.5)
	if got := at(f, 0, 0); got != blue {
		t.Errorf("wipe-right p=0.5 left edge = %v, want blue (revealed)", got)
	}
	if got := at(f, 7, 0); got != red {
		t.Errorf("wipe-right p=0.5 right edge = %v, want red (not yet)", got)
	}
}

func TestWipeSoftEdgeBlends(t *testing.T) {
	hard := transition.Wipe{Dir: transition.Right}
	soft := transition.Wipe{Dir: transition.Right, Softness: 0.5}
	// A soft edge must still honor the endpoint contract: the feather band is
	// expanded off-frame at p=0/p=1 so there is no half-blended seam at the edges.
	assertEndpoints(t, soft, 8, 8)
	// At the boundary column the hard wipe is a pure source pixel; the soft wipe
	// blends, so it is neither pure red nor pure blue somewhere in the band.
	f := run(soft, 8, 8, 0.5)
	blended := false
	for x := 0; x < 8; x++ {
		c := at(f, x, 0)
		if c != red && c != blue {
			blended = true
		}
	}
	if !blended {
		t.Error("soft wipe produced no blended band")
	}
	// Sanity: the hard variant has no blended pixels.
	fh := run(hard, 8, 8, 0.5)
	for x := 0; x < 8; x++ {
		if c := at(fh, x, 0); c != red && c != blue {
			t.Errorf("hard wipe blended at x=%d: %v", x, c)
		}
	}
}

func TestSlideRight(t *testing.T) {
	tr := transition.Slide{Dir: transition.Right}
	assertEndpoints(t, tr, 8, 8)
	f := run(tr, 8, 8, 0.5)
	if got := at(f, 0, 0); got != blue {
		t.Errorf("slide-right p=0.5 left = %v, want blue", got)
	}
	if got := at(f, 7, 0); got != red {
		t.Errorf("slide-right p=0.5 right = %v, want red", got)
	}
}

func TestPushDown(t *testing.T) {
	tr := transition.Push{Dir: transition.Down}
	assertEndpoints(t, tr, 8, 8)
	f := run(tr, 8, 8, 0.5)
	if got := at(f, 0, 0); got != blue {
		t.Errorf("push-down p=0.5 top = %v, want blue (entering)", got)
	}
	if got := at(f, 0, 7); got != red {
		t.Errorf("push-down p=0.5 bottom = %v, want red (exiting)", got)
	}
}

func TestIrisIn(t *testing.T) {
	tr := transition.Iris{Shape: transition.Circle}
	assertEndpoints(t, tr, 8, 8)
	f := run(tr, 8, 8, 0.5)
	if got := at(f, 4, 4); got != blue {
		t.Errorf("iris-in p=0.5 center = %v, want blue", got)
	}
	if got := at(f, 0, 0); got != red {
		t.Errorf("iris-in p=0.5 corner = %v, want red", got)
	}
}

func TestIrisOut(t *testing.T) {
	tr := transition.Iris{Shape: transition.Circle, Out: true}
	assertEndpoints(t, tr, 8, 8)
	f := run(tr, 8, 8, 0.5)
	if got := at(f, 4, 4); got != red {
		t.Errorf("iris-out p=0.5 center = %v, want red", got)
	}
	if got := at(f, 0, 0); got != blue {
		t.Errorf("iris-out p=0.5 corner = %v, want blue", got)
	}
}

// grayFrame builds a solid w x h Gray8 frame filled with v.
func grayFrame(w, h int, v byte) *clip.Frame {
	f := clip.NewFrame(w, h, clip.Gray8)
	for i := range f.Pix {
		f.Pix[i] = v
	}
	return f
}

func atGray(f *clip.Frame, x, y int) byte { return f.Pix[y*f.Stride+x] }

// runMask renders tr's alpha over opaque A (255) -> transparent B (0) at p.
func runMask(tr transition.MaskTransition, w, h int, p float64) *clip.Frame {
	a, b := grayFrame(w, h, 255), grayFrame(w, h, 0)
	dst := grayFrame(w, h, 0)
	tr.FrameMask(p, dst, a, b)
	return dst
}

// assertMaskEndpoints checks p=0 is fully A's alpha (opaque) and p=1 is fully
// B's alpha (transparent), across the whole frame.
func assertMaskEndpoints(t *testing.T, tr transition.MaskTransition, w, h int) {
	t.Helper()
	z, one := runMask(tr, w, h, 0), runMask(tr, w, h, 1)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if got := atGray(z, x, y); got != 255 {
				t.Fatalf("%s mask p=0 at (%d,%d) = %d, want 255 (A opaque)", tr.Name(), x, y, got)
			}
			if got := atGray(one, x, y); got != 0 {
				t.Fatalf("%s mask p=1 at (%d,%d) = %d, want 0 (B transparent)", tr.Name(), x, y, got)
			}
		}
	}
}

// TestWipeMaskMatchesShape is the core fix for the review finding: the alpha is
// wiped by the same moving edge as RGB, so it is NOT a uniform half-transparency
// at p=0.5 — the revealed (B) side is transparent, the other side opaque.
func TestWipeMaskMatchesShape(t *testing.T) {
	tr := transition.Wipe{Dir: transition.Right}
	assertMaskEndpoints(t, tr, 8, 8)
	f := runMask(tr, 8, 8, 0.5)
	if got := atGray(f, 0, 0); got != 0 {
		t.Errorf("wipe mask p=0.5 left edge = %d, want 0 (revealed B, transparent)", got)
	}
	if got := atGray(f, 7, 0); got != 255 {
		t.Errorf("wipe mask p=0.5 right edge = %d, want 255 (A, opaque)", got)
	}
}

func TestSlideMaskMatchesShape(t *testing.T) {
	tr := transition.Slide{Dir: transition.Right}
	assertMaskEndpoints(t, tr, 8, 8)
	f := runMask(tr, 8, 8, 0.5)
	if atGray(f, 0, 0) != 0 || atGray(f, 7, 0) != 255 {
		t.Errorf("slide mask p=0.5 = [%d..%d], want left 0 (B) / right 255 (A)", atGray(f, 0, 0), atGray(f, 7, 0))
	}
}

func TestIrisMaskMatchesShape(t *testing.T) {
	tr := transition.Iris{}
	assertMaskEndpoints(t, tr, 8, 8)
	f := runMask(tr, 8, 8, 0.5)
	if got := atGray(f, 4, 4); got != 0 {
		t.Errorf("iris mask p=0.5 center = %d, want 0 (revealed B)", got)
	}
	if got := atGray(f, 0, 0); got != 255 {
		t.Errorf("iris mask p=0.5 corner = %d, want 255 (A)", got)
	}
}

func TestDissolveMaskMatchesShape(t *testing.T) {
	tr := transition.Dissolve{}
	assertMaskEndpoints(t, tr, 8, 8)
	f := runMask(tr, 8, 8, 0.5)
	var saw0, saw255 bool
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			switch atGray(f, x, y) {
			case 0:
				saw0 = true
			case 255:
				saw255 = true
			}
		}
	}
	if !saw0 || !saw255 {
		t.Errorf("dissolve mask p=0.5 saw0=%v saw255=%v, want both", saw0, saw255)
	}
}

func TestFadeThroughColorMask(t *testing.T) {
	tr := transition.FadeThroughColor{} // black, opaque
	assertMaskEndpoints(t, tr, 4, 4)
	// At the midpoint the opaque color covers the frame, so alpha is fully opaque.
	if got := atGray(runMask(tr, 4, 4, 0.5), 0, 0); got != 255 {
		t.Errorf("fade-through-color mask p=0.5 = %d, want 255 (opaque color)", got)
	}
}

// TestIrisOffCenterEndpoints is the second review finding: with the aperture
// off-center, p=1 must still cover every corner (and p=0 show none of B). This
// fails if the max metric is a fixed center-based constant.
func TestIrisOffCenterEndpoints(t *testing.T) {
	for _, s := range []transition.Shape{transition.Circle, transition.Diamond, transition.Rectangle} {
		assertEndpoints(t, transition.Iris{Shape: s, CenterX: 0.2, CenterY: 0.8}, 16, 9)
	}
}

// TestIrisOddFrameEndpoints guards the off-by-half case: on an odd-sized frame
// the default center lands on a pixel sample (metric 0 there), which must not
// leak B into the p=0 frame nor leave A at p=1. Covers iris-in, iris-out, and
// the alpha sidecar.
func TestIrisOddFrameEndpoints(t *testing.T) {
	assertEndpoints(t, transition.Iris{}, 9, 9)
	assertEndpoints(t, transition.Iris{Out: true}, 9, 9)
	assertMaskEndpoints(t, transition.Iris{}, 9, 9)
}

func TestFuncAdaptor(t *testing.T) {
	called := false
	tr := transition.Func("noop", func(p float64, dst, a, b *clip.Frame) { called = true })
	if tr.Name() != "noop" {
		t.Errorf("name = %q, want noop", tr.Name())
	}
	tr.Frame(0.5, frame(2, 2, red), frame(2, 2, red), frame(2, 2, blue))
	if !called {
		t.Error("Func body not invoked")
	}
}

func TestEasedRemapsProgress(t *testing.T) {
	base := transition.CrossFade{}
	eased := transition.Eased(base, ease.EaseIn)
	// EaseIn(0.5) = 0.25, so the eased midpoint equals the linear quarter point.
	got := at(run(eased, 4, 4, 0.5), 0, 0)
	want := at(run(base, 4, 4, 0.25), 0, 0)
	if got != want {
		t.Errorf("eased p=0.5 = %v, want crossfade p=0.25 = %v", got, want)
	}
}

func TestReversedFlipsDirection(t *testing.T) {
	base := transition.Wipe{Dir: transition.Right}
	rev := transition.Reversed(base)
	if rev.Name() != "wipe-right-reversed" {
		t.Errorf("name = %q", rev.Name())
	}
	// Endpoints are preserved (p=0 still A, p=1 still B)...
	assertEndpoints(t, rev, 8, 8)
	// ...but the reveal comes from the opposite edge: at p=0.5 the right edge is
	// the revealed (B) side, mirroring the forward wipe's left edge.
	f := run(rev, 8, 8, 0.5)
	if got := at(f, 7, 0); got != blue {
		t.Errorf("reversed wipe p=0.5 right edge = %v, want blue", got)
	}
	if got := at(f, 0, 0); got != red {
		t.Errorf("reversed wipe p=0.5 left edge = %v, want red", got)
	}
}
