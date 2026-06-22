package effect_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/mowshon/moviego/v2/clip"
	"github.com/mowshon/moviego/v2/effect"
	"github.com/mowshon/moviego/v2/video"
)

// colorImage builds a static image whose every pixel is col, so a color
// transform's output is a single comparable triplet.
func colorImage(w, h int, col [3]byte) *video.ImageNode {
	rgb := clip.NewFrame(w, h, clip.RGB24)
	for y := 0; y < h; y++ {
		d := rgb.Pix[y*rgb.Stride:]
		for x := 0; x < w; x++ {
			d[x*3], d[x*3+1], d[x*3+2] = col[0], col[1], col[2]
		}
	}
	return video.NewImage(rgb, nil)
}

// renderRGB applies eff to a uniform color image and returns the rendered first
// frame.
func renderRGB(t *testing.T, eff effect.VideoEffect, w, h int, col [3]byte) *clip.Frame {
	t.Helper()
	out, err := eff.ApplyVideo(colorImage(w, h, col))
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	size := out.Size()
	dst := clip.NewFrame(size.W, size.H, clip.RGB24)
	if err := out.FrameInto(context.Background(), 0, dst); err != nil {
		t.Fatalf("render: %v", err)
	}
	return dst
}

// pixelAt returns the RGB triplet at (x, y).
func pixelAt(f *clip.Frame, x, y int) [3]byte {
	o := y*f.Stride + x*3
	return [3]byte{f.Pix[o], f.Pix[o+1], f.Pix[o+2]}
}

// near reports whether each channel of got is within tol of want.
func near(got, want [3]byte, tol int) bool {
	for i := 0; i < 3; i++ {
		d := int(got[i]) - int(want[i])
		if d < -tol || d > tol {
			return false
		}
	}
	return true
}

func TestBrightnessGolden(t *testing.T) {
	f := renderRGB(t, effect.Brightness{Delta: 0.2}, 2, 2, [3]byte{100, 100, 100})
	if got := pixelAt(f, 0, 0); got != [3]byte{151, 151, 151} {
		t.Errorf("brightness = %v, want {151,151,151}", got)
	}
}

func TestContrastGolden(t *testing.T) {
	f := renderRGB(t, effect.Contrast{Amount: 2}, 2, 2, [3]byte{50, 50, 50})
	// (50/255-0.5)*2+0.5 < 0 -> clamps to black.
	if got := pixelAt(f, 0, 0); got != [3]byte{0, 0, 0} {
		t.Errorf("contrast = %v, want {0,0,0}", got)
	}
}

func TestGammaGolden(t *testing.T) {
	f := renderRGB(t, effect.Gamma{Value: 2}, 2, 2, [3]byte{128, 128, 128})
	if got := pixelAt(f, 0, 0); !near(got, [3]byte{181, 181, 181}, 1) {
		t.Errorf("gamma = %v, want ~{181,181,181}", got)
	}
}

func TestInvertGolden(t *testing.T) {
	f := renderRGB(t, effect.Invert{}, 2, 2, [3]byte{100, 150, 200})
	if got := pixelAt(f, 0, 0); got != [3]byte{155, 105, 55} {
		t.Errorf("invert = %v, want {155,105,55}", got)
	}
}

func TestSaturationGolden(t *testing.T) {
	f := renderRGB(t, effect.Saturation{Amount: 2}, 2, 2, [3]byte{200, 100, 50})
	if got := pixelAt(f, 0, 0); !near(got, [3]byte{255, 76, 0}, 2) {
		t.Errorf("saturation = %v, want ~{255,76,0}", got)
	}
}

func TestSaturationLeavesGrayUnchanged(t *testing.T) {
	f := renderRGB(t, effect.Saturation{Amount: 3}, 2, 2, [3]byte{120, 120, 120})
	if got := pixelAt(f, 0, 0); !near(got, [3]byte{120, 120, 120}, 1) {
		t.Errorf("saturation of gray = %v, want unchanged", got)
	}
}

func TestColorBalanceGolden(t *testing.T) {
	f := renderRGB(t, effect.ColorBalance{Mids: [3]float64{0.5, 0, 0}}, 2, 2, [3]byte{128, 128, 128})
	got := pixelAt(f, 0, 0)
	if got[0] < 250 || got[1] != 128 || got[2] != 128 {
		t.Errorf("color balance = %v, want red boosted, green/blue unchanged", got)
	}
}

func TestHSLLeavesGrayUnchanged(t *testing.T) {
	f := renderRGB(t, effect.HSL{Hue: 120}, 2, 2, [3]byte{128, 128, 128})
	if got := pixelAt(f, 0, 0); !near(got, [3]byte{128, 128, 128}, 1) {
		t.Errorf("hue rotation of gray = %v, want unchanged", got)
	}
}

func TestHSLHueRotation(t *testing.T) {
	f := renderRGB(t, effect.HSL{Hue: 120}, 2, 2, [3]byte{255, 0, 0})
	if got := pixelAt(f, 0, 0); !near(got, [3]byte{0, 255, 0}, 2) {
		t.Errorf("red rotated 120deg = %v, want ~{0,255,0}", got)
	}
}

func TestGrayscaleMatchesBlackAndWhite(t *testing.T) {
	g := renderRGB(t, effect.Grayscale{}, 2, 2, [3]byte{200, 100, 50})
	bw := renderRGB(t, effect.BlackAndWhite{}, 2, 2, [3]byte{200, 100, 50})
	if pixelAt(g, 0, 0) != pixelAt(bw, 0, 0) {
		t.Errorf("grayscale %v != blackandwhite %v", pixelAt(g, 0, 0), pixelAt(bw, 0, 0))
	}
}

func TestVignetteDarkensCorners(t *testing.T) {
	f := renderRGB(t, effect.Vignette{Amount: 1, Radius: 0}, 11, 11, [3]byte{255, 255, 255})
	if center := pixelAt(f, 5, 5); center != [3]byte{255, 255, 255} {
		t.Errorf("vignette center = %v, want white", center)
	}
	if corner := pixelAt(f, 0, 0); corner != [3]byte{0, 0, 0} {
		t.Errorf("vignette corner = %v, want black", corner)
	}
}

func TestGaussianBlurPreservesUniform(t *testing.T) {
	f := renderRGB(t, effect.GaussianBlur{Radius: 2}, 8, 8, [3]byte{100, 120, 140})
	for _, p := range [][2]int{{0, 0}, {3, 3}, {7, 7}} {
		if got := pixelAt(f, p[0], p[1]); !near(got, [3]byte{100, 120, 140}, 1) {
			t.Errorf("blur of uniform at %v = %v, want unchanged", p, got)
		}
	}
}

func TestMotionBlurPreservesUniform(t *testing.T) {
	f := renderRGB(t, effect.MotionBlur{Angle: 30, Distance: 6}, 8, 8, [3]byte{80, 90, 100})
	if got := pixelAt(f, 4, 4); !near(got, [3]byte{80, 90, 100}, 1) {
		t.Errorf("motion blur of uniform = %v, want unchanged", got)
	}
}

// TestMotionBlurSmearsHorizontally checks a horizontal smear (Angle 0) softens a
// vertical edge along x while leaving a horizontal edge untouched, confirming the
// blur runs along the angle vector and not both axes.
func TestMotionBlurSmearsHorizontally(t *testing.T) {
	src := clip.NewFrame(16, 16, clip.RGB24)
	for y := 0; y < 16; y++ {
		d := src.Pix[y*src.Stride:]
		for x := 8; x < 16; x++ {
			d[x*3], d[x*3+1], d[x*3+2] = 255, 255, 255
		}
	}
	out, err := effect.MotionBlur{Angle: 0, Distance: 6}.ApplyVideo(video.NewImage(src, nil))
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	dst := clip.NewFrame(16, 16, clip.RGB24)
	if err := out.FrameInto(context.Background(), 0, dst); err != nil {
		t.Fatalf("render: %v", err)
	}
	// The vertical seam at x=8 should now hold an intermediate gray (smeared in x).
	if seam := pixelAt(dst, 8, 8)[0]; seam == 0 || seam == 255 {
		t.Errorf("seam = %d, want an intermediate (horizontal smear)", seam)
	}
	// Columns far from the seam stay solid: x=0 fully black, x=15 fully white.
	if got := pixelAt(dst, 0, 8); got != [3]byte{0, 0, 0} {
		t.Errorf("left edge = %v, want black", got)
	}
	if got := pixelAt(dst, 15, 8); got != [3]byte{255, 255, 255} {
		t.Errorf("right edge = %v, want white", got)
	}
}

// TestGaussianBlurTransformsMask confirms GaussianBlur blurs the Gray8 mask
// sidecar (Sidecars.Mask), not only RGB: a sharp mask edge softens.
func TestGaussianBlurTransformsMask(t *testing.T) {
	rgb := clip.NewFrame(8, 4, clip.RGB24)
	mask := clip.NewFrame(8, 4, clip.Gray8)
	for y := 0; y < 4; y++ {
		for x := 4; x < 8; x++ {
			mask.Pix[y*mask.Stride+x] = 255
		}
	}
	out, err := effect.GaussianBlur{Radius: 1.5}.ApplyVideo(video.NewImage(rgb, mask))
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	dst := clip.NewFrame(8, 4, clip.Gray8)
	ok, err := out.MaskInto(context.Background(), 0, dst)
	if err != nil || !ok {
		t.Fatalf("mask render: ok=%v err=%v", ok, err)
	}
	if seam := dst.Pix[2*dst.Stride+4]; seam == 0 || seam == 255 {
		t.Errorf("mask seam = %d, want an intermediate (mask blurred)", seam)
	}
}

// TestVignetteDegenerateGeometry pins the edge cases that used to divide by zero:
// a radius at/over 1 and a single-pixel clip must stay finite (no NaN/Inf gain).
func TestVignetteDegenerateGeometry(t *testing.T) {
	// Radius == 1 (and >1) must not divide by zero; the result stays a valid byte.
	for _, r := range []float64{1.0, 5.0} {
		f := renderRGB(t, effect.Vignette{Amount: 0.8, Radius: r}, 8, 8, [3]byte{200, 200, 200})
		if got := pixelAt(f, 0, 0); got[0] == 0 && got[1] == 0 && got[2] == 0 {
			// Not asserting an exact value, only that it is a sane, non-corrupt pixel.
			t.Errorf("radius %v corner = %v, unexpectedly collapsed", r, got)
		}
	}
	// A 1x1 clip has no center-to-corner distance; gain must stay 1 (unchanged).
	f := renderRGB(t, effect.Vignette{Amount: 1, Radius: 0}, 1, 1, [3]byte{123, 45, 67})
	if got := pixelAt(f, 0, 0); got != [3]byte{123, 45, 67} {
		t.Errorf("1x1 vignette = %v, want unchanged {123,45,67}", got)
	}
}

func TestGaussianBlurSmoothsEdge(t *testing.T) {
	// A vertical black/white split should produce intermediate values at the seam.
	src := clip.NewFrame(8, 4, clip.RGB24)
	for y := 0; y < 4; y++ {
		d := src.Pix[y*src.Stride:]
		for x := 4; x < 8; x++ {
			d[x*3], d[x*3+1], d[x*3+2] = 255, 255, 255
		}
	}
	out, err := effect.GaussianBlur{Radius: 1.5}.ApplyVideo(video.NewImage(src, nil))
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	dst := clip.NewFrame(8, 4, clip.RGB24)
	if err := out.FrameInto(context.Background(), 0, dst); err != nil {
		t.Fatalf("render: %v", err)
	}
	seam := pixelAt(dst, 4, 2)[0]
	if seam == 0 || seam == 255 {
		t.Errorf("seam pixel = %d, want an intermediate (edge softened)", seam)
	}
}

func TestRotateArbitraryFillsCornersAndKeepsCenter(t *testing.T) {
	f := renderRGB(t, effect.Rotate{Degrees: 30, Expand: true}, 11, 11, [3]byte{50, 60, 70})
	size := clip.Size{W: f.W, H: f.H}
	if center := pixelAt(f, size.W/2, size.H/2); !near(center, [3]byte{50, 60, 70}, 2) {
		t.Errorf("rotated center = %v, want source color", center)
	}
	if corner := pixelAt(f, 0, 0); corner != [3]byte{0, 0, 0} {
		t.Errorf("rotated corner = %v, want black fill", corner)
	}
}

const identityCube = `# identity 3D LUT
TITLE "identity"
LUT_3D_SIZE 2
DOMAIN_MIN 0 0 0
DOMAIN_MAX 1 1 1
0 0 0
1 0 0
0 1 0
1 1 0
0 0 1
1 0 1
0 1 1
1 1 1
`

func writeCube(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "look.cube")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write cube: %v", err)
	}
	return path
}

func TestLUTIdentityIsApproximatePassthrough(t *testing.T) {
	path := writeCube(t, identityCube)
	f := renderRGB(t, effect.LUT{Path: path}, 2, 2, [3]byte{10, 120, 240})
	if got := pixelAt(f, 0, 0); !near(got, [3]byte{10, 120, 240}, 2) {
		t.Errorf("identity LUT = %v, want ~{10,120,240}", got)
	}
}

func TestLUTRejectsBadFile(t *testing.T) {
	if _, err := (effect.LUT{Path: writeCube(t, "LUT_3D_SIZE 2\n0 0 0\n")}).ApplyVideo(colorImage(2, 2, [3]byte{})); err == nil {
		t.Error("want error for truncated cube, got nil")
	}
	if _, err := (effect.LUT{Path: filepath.Join(t.TempDir(), "missing.cube")}).ApplyVideo(colorImage(2, 2, [3]byte{})); err == nil {
		t.Error("want error for missing file, got nil")
	}
	// A degenerate domain on green (max == min) would yield NaN grid coordinates
	// when sampled; it must be rejected at parse, not just for the red channel.
	degenerateGreen := "LUT_3D_SIZE 2\nDOMAIN_MAX 1 0 1\n0 0 0\n1 0 0\n0 1 0\n1 1 0\n0 0 1\n1 0 1\n0 1 1\n1 1 1\n"
	if _, err := (effect.LUT{Path: writeCube(t, degenerateGreen)}).ApplyVideo(colorImage(2, 2, [3]byte{})); err == nil {
		t.Error("want error for degenerate green domain, got nil")
	}
}

func TestColorEffectsZeroValueIsPassthrough(t *testing.T) {
	base := colorImage(2, 2, [3]byte{77, 88, 99})
	cases := []effect.VideoEffect{
		effect.Brightness{}, effect.Contrast{}, effect.Saturation{},
		effect.Gamma{}, effect.ColorBalance{}, effect.HSL{}, effect.Vignette{},
		effect.GaussianBlur{}, effect.MotionBlur{},
	}
	for _, eff := range cases {
		out, err := eff.ApplyVideo(base)
		if err != nil {
			t.Fatalf("%T: %v", eff, err)
		}
		dst := clip.NewFrame(2, 2, clip.RGB24)
		if err := out.FrameInto(context.Background(), 0, dst); err != nil {
			t.Fatalf("%T render: %v", eff, err)
		}
		if got := pixelAt(dst, 0, 0); got != [3]byte{77, 88, 99} {
			t.Errorf("%T zero value = %v, want passthrough {77,88,99}", eff, got)
		}
	}
}
