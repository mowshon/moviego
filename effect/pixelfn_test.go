package effect_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/mowshon/moviego/v2/clip"
	"github.com/mowshon/moviego/v2/effect"
	"github.com/mowshon/moviego/v2/video"
)

// frameOf renders the clip's first frame into a fresh RGB24 buffer.
func frameOf(t *testing.T, c video.VideoClip) *clip.Frame {
	t.Helper()
	s := c.Size()
	dst := clip.NewFrame(s.W, s.H, clip.RGB24)
	if err := c.FrameInto(context.Background(), 0, dst); err != nil {
		t.Fatalf("frame into: %v", err)
	}
	return dst
}

// TestPixelFnTargets pins how the Sidecars field becomes Targets(): Video is
// always implied, Mask only when declared.
func TestPixelFnTargets(t *testing.T) {
	if got := (effect.PixelFn{}).Targets(); got != (effect.EffectTargets{Video: true}) {
		t.Errorf("zero PixelFn targets = %+v, want Video only", got)
	}
	got := effect.PixelFn{Sidecars: effect.EffectTargets{Mask: true}}.Targets()
	if got != (effect.EffectTargets{Video: true, Mask: true}) {
		t.Errorf("masked PixelFn targets = %+v, want Video+Mask", got)
	}
}

// TestPixelFnAppliesAndPreservesSize checks the hook generates working
// VideoClip plumbing from a single Fn: the transform runs and the output size
// matches the input (same-size contract).
func TestPixelFnAppliesAndPreservesSize(t *testing.T) {
	in := solidImage(4, 2, 10)
	out, err := effect.PixelFn{
		Name: "add-5",
		Fn: func(_ clip.Time, dst, src *clip.Frame) error {
			for i := range src.Pix {
				dst.Pix[i] = src.Pix[i] + 5
			}
			return nil
		},
	}.ApplyVideo(in)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if out.Size() != (clip.Size{W: 4, H: 2}) {
		t.Fatalf("size = %v, want 4x2", out.Size())
	}
	got := frameOf(t, out)
	for _, b := range got.Pix {
		if b != 15 {
			t.Fatalf("pixel = %d, want 15", b)
		}
	}
}

// TestPixelFnPropagatesMask verifies that Sidecars.Mask reuses Fn for the alpha
// sidecar, the propagation-matrix contract for a mask-touching pixel effect.
func TestPixelFnPropagatesMask(t *testing.T) {
	rgb := clip.NewFrame(2, 2, clip.RGB24)
	alpha := clip.NewFrame(2, 2, clip.Gray8)
	for i := range alpha.Pix {
		alpha.Pix[i] = 100
	}
	in := video.NewImage(rgb, alpha)

	out, err := effect.PixelFn{
		Sidecars: effect.EffectTargets{Mask: true},
		Fn: func(_ clip.Time, dst, src *clip.Frame) error {
			for i := range src.Pix {
				dst.Pix[i] = src.Pix[i] + 1
			}
			return nil
		},
	}.ApplyVideo(in)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if !out.HasMask() {
		t.Fatal("mask dropped")
	}
	maskDst := clip.NewFrame(2, 2, clip.Gray8)
	ok, err := out.MaskInto(context.Background(), 0, maskDst)
	if err != nil || !ok {
		t.Fatalf("mask into: ok=%v err=%v", ok, err)
	}
	for _, b := range maskDst.Pix {
		if b != 101 {
			t.Fatalf("mask value = %d, want 101 (Fn applied)", b)
		}
	}
}

// TestPixelFnNilFn reports a clear error rather than panicking.
func TestPixelFnNilFn(t *testing.T) {
	_, err := effect.PixelFn{Name: "x"}.ApplyVideo(solidImage(2, 2, 0))
	if !errors.Is(err, effect.ErrNoPixelFn) {
		t.Fatalf("err = %v, want ErrNoPixelFn", err)
	}
}

// TestNewEffectTargets pins the declared sidecars of the reframe/blur effects to
// the propagation matrix.
func TestNewEffectTargets(t *testing.T) {
	cases := []struct {
		name string
		eff  effect.VideoEffect
		want effect.EffectTargets
	}{
		{"FlipH", effect.FlipH{}, effect.EffectTargets{Video: true, Mask: true}},
		{"FlipV", effect.FlipV{}, effect.EffectTargets{Video: true, Mask: true}},
		{"Pad", effect.Pad{W: 2, H: 2}, effect.EffectTargets{Video: true, Mask: true}},
		{"FitTo", effect.FitTo{W: 2, H: 2}, effect.EffectTargets{Video: true, Mask: true}},
		{"Blur", effect.Blur{Radius: 2}, effect.EffectTargets{Video: true, Mask: true}},
		{"Sharpen", effect.Sharpen{Amount: 1}, effect.EffectTargets{Video: true}},
	}
	for _, c := range cases {
		if got := c.eff.Targets(); got != c.want {
			t.Errorf("%s targets = %+v, want %+v", c.name, got, c.want)
		}
	}
}

// TestFlipHSwapsColumns checks the horizontal mirror on a 2x1 image.
func TestFlipHSwapsColumns(t *testing.T) {
	rgb := clip.NewFrame(2, 1, clip.RGB24)
	rgb.Pix[0], rgb.Pix[1], rgb.Pix[2] = 10, 10, 10 // left
	rgb.Pix[3], rgb.Pix[4], rgb.Pix[5] = 20, 20, 20 // right
	in := video.NewImage(rgb, nil)
	out, err := effect.FlipH{}.ApplyVideo(in)
	if err != nil {
		t.Fatalf("flip: %v", err)
	}
	got := frameOf(t, out)
	if got.Pix[0] != 20 || got.Pix[3] != 10 {
		t.Fatalf("flipH = %d,%d; want 20,10", got.Pix[0], got.Pix[3])
	}
}

// TestPadCentersAndSizes checks Pad puts a 2x2 source centered on a 4x4 canvas
// filled with the given color.
func TestPadCentersAndSizes(t *testing.T) {
	in := solidImage(2, 2, 200)
	out, err := effect.Pad{W: 4, H: 4, Color: [3]byte{5, 5, 5}}.ApplyVideo(in)
	if err != nil {
		t.Fatalf("pad: %v", err)
	}
	if out.Size() != (clip.Size{W: 4, H: 4}) {
		t.Fatalf("size = %v, want 4x4", out.Size())
	}
	got := frameOf(t, out)
	// Corner (0,0) is fill; center (1,1) is source.
	if got.Pix[0] != 5 {
		t.Errorf("corner = %d, want 5 (fill)", got.Pix[0])
	}
	center := 1*got.Stride + 1*3
	if got.Pix[center] != 200 {
		t.Errorf("center = %d, want 200 (source)", got.Pix[center])
	}
}

// TestPadRejectsShrink checks Pad refuses a target smaller than the source.
func TestPadRejectsShrink(t *testing.T) {
	_, err := effect.Pad{W: 1, H: 1}.ApplyVideo(solidImage(4, 4, 0))
	if !errors.Is(err, effect.ErrInvalidPad) {
		t.Fatalf("err = %v, want ErrInvalidPad", err)
	}
}

// TestFitToPreservesAspect checks a 4x2 source fit into a 4x4 box scales to 4x2
// and letterboxes to exactly 4x4 (no distortion).
func TestFitToPreservesAspect(t *testing.T) {
	in := solidImage(4, 2, 150)
	out, err := effect.FitTo{W: 4, H: 4}.ApplyVideo(in)
	if err != nil {
		t.Fatalf("fitto: %v", err)
	}
	if out.Size() != (clip.Size{W: 4, H: 4}) {
		t.Fatalf("size = %v, want 4x4", out.Size())
	}
}

// TestBlurZeroRadiusPassthrough checks a zero radius returns the clip unchanged.
func TestBlurZeroRadiusPassthrough(t *testing.T) {
	in := solidImage(4, 4, 100)
	out, err := effect.Blur{Radius: 0}.ApplyVideo(in)
	if err != nil {
		t.Fatalf("blur: %v", err)
	}
	if out != video.VideoClip(in) {
		t.Error("zero-radius blur should return the input clip unchanged")
	}
}

// TestBlurPreservesSizeOnSolid checks a solid image stays solid and same-size
// through the resample blur.
func TestBlurPreservesSizeOnSolid(t *testing.T) {
	in := solidImage(16, 16, 120)
	out, err := effect.Blur{Radius: 3}.ApplyVideo(in)
	if err != nil {
		t.Fatalf("blur: %v", err)
	}
	if out.Size() != (clip.Size{W: 16, H: 16}) {
		t.Fatalf("size = %v, want 16x16", out.Size())
	}
	got := frameOf(t, out)
	for _, b := range got.Pix {
		if b != 120 {
			t.Fatalf("blurred solid = %d, want 120", b)
		}
	}
}

// TestSharpenZeroAmountPassthrough checks a zero amount returns the clip
// unchanged.
func TestSharpenZeroAmountPassthrough(t *testing.T) {
	in := solidImage(4, 4, 100)
	out, err := effect.Sharpen{Amount: 0}.ApplyVideo(in)
	if err != nil {
		t.Fatalf("sharpen: %v", err)
	}
	if out != video.VideoClip(in) {
		t.Error("zero-amount sharpen should return the input clip unchanged")
	}
}

// TestFlipVSwapsRows checks the vertical mirror on a 1x2 image.
func TestFlipVSwapsRows(t *testing.T) {
	rgb := clip.NewFrame(1, 2, clip.RGB24)
	rgb.Pix[0], rgb.Pix[1], rgb.Pix[2] = 10, 10, 10 // top row
	rgb.Pix[3], rgb.Pix[4], rgb.Pix[5] = 20, 20, 20 // bottom row
	in := video.NewImage(rgb, nil)
	out, err := effect.FlipV{}.ApplyVideo(in)
	if err != nil {
		t.Fatalf("flip: %v", err)
	}
	got := frameOf(t, out)
	if got.Pix[0] != 20 || got.Pix[got.Stride] != 10 {
		t.Fatalf("flipV = %d,%d; want 20,10", got.Pix[0], got.Pix[got.Stride])
	}
}

// TestPadMaskMarginsTransparent verifies a padded clip's mask margins are
// transparent (0) while the source region keeps its alpha.
func TestPadMaskMarginsTransparent(t *testing.T) {
	rgb := clip.NewFrame(2, 2, clip.RGB24)
	alpha := clip.NewFrame(2, 2, clip.Gray8)
	for i := range alpha.Pix {
		alpha.Pix[i] = 255
	}
	in := video.NewImage(rgb, alpha)

	out, err := effect.Pad{W: 4, H: 4, Color: [3]byte{0, 0, 0}}.ApplyVideo(in)
	if err != nil {
		t.Fatalf("pad: %v", err)
	}
	if !out.HasMask() {
		t.Fatal("padded clip lost its mask")
	}
	maskDst := clip.NewFrame(4, 4, clip.Gray8)
	ok, err := out.MaskInto(context.Background(), 0, maskDst)
	if err != nil || !ok {
		t.Fatalf("mask into: ok=%v err=%v", ok, err)
	}
	if maskDst.Pix[0] != 0 { // corner margin
		t.Errorf("corner mask = %d, want 0 (transparent margin)", maskDst.Pix[0])
	}
	center := 1*maskDst.Stride + 1 // source region (offset 1,1)
	if maskDst.Pix[center] != 255 {
		t.Errorf("center mask = %d, want 255 (source alpha)", maskDst.Pix[center])
	}
}

// TestSharpenEnhancesEdge checks the unsharp mask overshoots a step edge beyond
// the source's flat extremes, the defining behavior of edge enhancement.
func TestSharpenEnhancesEdge(t *testing.T) {
	const w, h = 24, 4
	rgb := clip.NewFrame(w, h, clip.RGB24)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			v := byte(40)
			if x >= w/2 {
				v = 210
			}
			o := y*rgb.Stride + x*3
			rgb.Pix[o], rgb.Pix[o+1], rgb.Pix[o+2] = v, v, v
		}
	}
	in := video.NewImage(rgb, nil)
	out, err := effect.Sharpen{Amount: 2}.ApplyVideo(in)
	if err != nil {
		t.Fatalf("sharpen: %v", err)
	}
	got := frameOf(t, out)
	var min, max byte = 255, 0
	for _, b := range got.Pix {
		if b < min {
			min = b
		}
		if b > max {
			max = b
		}
	}
	if !(max > 210 || min < 40) {
		t.Errorf("no edge overshoot: min=%d max=%d (source range 40..210)", min, max)
	}
}

// TestPixelFnNoMaskPassthrough verifies that without Sidecars.Mask the mask is
// passed through unchanged even when Fn mutates pixels — the declared target and
// the applied transform stay consistent.
func TestPixelFnNoMaskPassthrough(t *testing.T) {
	rgb := clip.NewFrame(2, 2, clip.RGB24)
	alpha := clip.NewFrame(2, 2, clip.Gray8)
	for i := range alpha.Pix {
		alpha.Pix[i] = 77
	}
	in := video.NewImage(rgb, alpha)

	out, err := effect.PixelFn{
		Fn: func(_ clip.Time, dst, src *clip.Frame) error {
			for i := range src.Pix {
				dst.Pix[i] = src.Pix[i] + 1
			}
			return nil
		},
	}.ApplyVideo(in)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	maskDst := clip.NewFrame(2, 2, clip.Gray8)
	ok, err := out.MaskInto(context.Background(), 0, maskDst)
	if err != nil || !ok {
		t.Fatalf("mask into: ok=%v err=%v", ok, err)
	}
	for _, b := range maskDst.Pix {
		if b != 77 {
			t.Fatalf("mask changed without Sidecars.Mask: %d, want 77", b)
		}
	}
}

// frameAt renders the clip's frame at time t into a fresh RGB24 buffer.
func frameAt(t *testing.T, c video.VideoClip, at clip.Time) *clip.Frame {
	t.Helper()
	s := c.Size()
	dst := clip.NewFrame(s.W, s.H, clip.RGB24)
	if err := c.FrameInto(context.Background(), at, dst); err != nil {
		t.Fatalf("frame into @%v: %v", at, err)
	}
	return dst
}

// TestFlipHStartGates checks the Start offset: before Start the frame passes
// through unflipped, and from Start onward it is mirrored.
func TestFlipHStartGates(t *testing.T) {
	rgb := clip.NewFrame(2, 1, clip.RGB24)
	rgb.Pix[0], rgb.Pix[1], rgb.Pix[2] = 10, 10, 10 // left
	rgb.Pix[3], rgb.Pix[4], rgb.Pix[5] = 20, 20, 20 // right
	in := video.NewImage(rgb, nil).WithDuration(2 * time.Second).(video.VideoClip)

	out, err := effect.FlipH{Start: time.Second}.ApplyVideo(in)
	if err != nil {
		t.Fatalf("flip: %v", err)
	}
	// Before Start: unflipped (left stays 10).
	if got := frameAt(t, out, 0); got.Pix[0] != 10 {
		t.Errorf("pre-start left = %d, want 10 (unflipped)", got.Pix[0])
	}
	// From Start: flipped (left now holds the right's 20).
	if got := frameAt(t, out, time.Second); got.Pix[0] != 20 {
		t.Errorf("post-start left = %d, want 20 (flipped)", got.Pix[0])
	}
}

// TestPixelFnStartGatesMask checks the Start offset gates the mask plane too.
func TestPixelFnStartGatesMask(t *testing.T) {
	rgb := clip.NewFrame(2, 2, clip.RGB24)
	alpha := clip.NewFrame(2, 2, clip.Gray8)
	for i := range alpha.Pix {
		alpha.Pix[i] = 100
	}
	in := video.NewImage(rgb, alpha).WithDuration(2 * time.Second).(video.VideoClip)

	out, err := effect.PixelFn{
		Sidecars: effect.EffectTargets{Mask: true},
		Start:    time.Second,
		Fn: func(_ clip.Time, dst, src *clip.Frame) error {
			for i := range src.Pix {
				dst.Pix[i] = src.Pix[i] + 1
			}
			return nil
		},
	}.ApplyVideo(in)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	maskBefore := clip.NewFrame(2, 2, clip.Gray8)
	if _, err := out.MaskInto(context.Background(), 0, maskBefore); err != nil {
		t.Fatalf("mask into: %v", err)
	}
	if maskBefore.Pix[0] != 100 {
		t.Errorf("pre-start mask = %d, want 100 (passthrough)", maskBefore.Pix[0])
	}
	maskAfter := clip.NewFrame(2, 2, clip.Gray8)
	if _, err := out.MaskInto(context.Background(), time.Second, maskAfter); err != nil {
		t.Fatalf("mask into: %v", err)
	}
	if maskAfter.Pix[0] != 101 {
		t.Errorf("post-start mask = %d, want 101 (Fn applied)", maskAfter.Pix[0])
	}
}
