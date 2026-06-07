package blend

import (
	"math"
	"testing"
)

// TestBlend1RoundsLikeDivide255 checks the integer divide-by-255 rounding trick
// matches round(fg*a + bg*(255-a) / 255) across the full byte range at the
// endpoints and a sampling of opacities.
func TestBlend1RoundsLikeDivide255(t *testing.T) {
	for _, a := range []byte{0, 1, 64, 127, 128, 200, 254, 255} {
		for _, fg := range []int{0, 1, 127, 200, 255} {
			for _, bg := range []int{0, 55, 128, 255} {
				got := Blend1(byte(fg), byte(bg), a)
				want := math.Round(RefOverOpaque(byte(fg), byte(bg), a))
				if math.Abs(float64(got)-want) > 1 {
					t.Errorf("Blend1(%d,%d,%d)=%d, ref=%g (err>1)", fg, bg, a, got, want)
				}
			}
		}
	}
}

// TestBlend1Endpoints verifies the alpha endpoints are exact: a=255 returns the
// foreground, a=0 returns the background.
func TestBlend1Endpoints(t *testing.T) {
	if got := Blend1(200, 50, 255); got != 200 {
		t.Errorf("a=255 -> %d, want fg 200", got)
	}
	if got := Blend1(200, 50, 0); got != 50 {
		t.Errorf("a=0 -> %d, want bg 50", got)
	}
}

// TestOverOpaqueRowToleranceVsFloat asserts the path-c kernel stays within 1 of
// the float64 reference per channel.
func TestOverOpaqueRowToleranceVsFloat(t *testing.T) {
	const n = 4
	fg := []byte{255, 0, 0 /**/, 0, 255, 0 /**/, 10, 20, 30 /**/, 200, 100, 50}
	dst := []byte{0, 0, 255 /**/, 255, 0, 0 /**/, 60, 50, 40 /**/, 10, 20, 30}
	a := []byte{128, 64, 200, 255}
	want := make([]byte, len(dst))
	copy(want, dst)
	OverOpaqueRow(dst, fg, a, n)
	for x := 0; x < n; x++ {
		for c := 0; c < 3; c++ {
			ref := RefOverOpaque(fg[x*3+c], want[x*3+c], a[x])
			if math.Abs(float64(dst[x*3+c])-ref) > 1 {
				t.Errorf("px %d ch %d = %d, ref %g (err>1)", x, c, dst[x*3+c], ref)
			}
		}
	}
}

// TestOver5040Gives70 reproduces MoviePy's "two layers of 50% and 40% opacity
// compose to 70%, not 90%" mental model.
func TestOver5040Gives70(t *testing.T) {
	// a = 50% (128/255 ~ 0.502), abg = 40% (102/255 = 0.4).
	dstRGB := []byte{0, 0, 0}
	fgRGB := []byte{255, 255, 255}
	aSrc := []byte{128}
	aDst := []byte{102}
	OverRow(dstRGB, fgRGB, aSrc, aDst, 1)
	// final = 0.502 + 0.4*(1-0.502) = 0.701 -> ~179.
	if aDst[0] < 177 || aDst[0] > 181 {
		t.Errorf("final alpha = %d, want ~179 (70%%)", aDst[0])
	}
}

// TestOverRowToleranceVsFloat asserts the path-d kernel (final alpha and the
// recovered RGB) stays within 1 of the float64 reference, including the
// safe-alpha zero guard.
func TestOverRowToleranceVsFloat(t *testing.T) {
	type px struct {
		fg, bg     [3]byte
		aSrc, aDst byte
	}
	cases := []px{
		{[3]byte{255, 255, 255}, [3]byte{0, 0, 0}, 128, 102},
		{[3]byte{200, 100, 50}, [3]byte{10, 20, 30}, 64, 200},
		{[3]byte{0, 0, 0}, [3]byte{0, 0, 0}, 0, 0},       // both transparent -> guard
		{[3]byte{255, 0, 0}, [3]byte{0, 0, 255}, 255, 0}, // src fully opaque
		{[3]byte{30, 60, 90}, [3]byte{200, 150, 100}, 200, 255},
	}
	for i, c := range cases {
		dstRGB := []byte{c.bg[0], c.bg[1], c.bg[2]}
		fgRGB := []byte{c.fg[0], c.fg[1], c.fg[2]}
		aSrc := []byte{c.aSrc}
		aDst := []byte{c.aDst}
		refA, refRGB := RefOver(c.fg, c.bg, c.aSrc, c.aDst)
		OverRow(dstRGB, fgRGB, aSrc, aDst, 1)
		if math.Abs(float64(aDst[0])-refA) > 1 {
			t.Errorf("case %d final alpha = %d, ref %g (err>1)", i, aDst[0], refA)
		}
		if refA == 0 {
			continue // RGB undefined when transparent
		}
		for ch := 0; ch < 3; ch++ {
			if math.Abs(float64(dstRGB[ch])-refRGB[ch]) > 1 {
				t.Errorf("case %d ch %d = %d, ref %g (err>1)", i, ch, dstRGB[ch], refRGB[ch])
			}
		}
	}
}

// TestFillAlphaRow checks the path-b alpha cover writes exactly n bytes of 255.
func TestFillAlphaRow(t *testing.T) {
	dst := make([]byte, 5)
	FillAlphaRow(dst, 3)
	want := []byte{255, 255, 255, 0, 0}
	for i := range want {
		if dst[i] != want[i] {
			t.Fatalf("dst = %v, want %v", dst, want)
		}
	}
}
