package transition

import "github.com/mowshon/moviego/v2/clip"

// Dissolve is a film-style dissolve: an ordered (Bayer) dither selects, per
// pixel, whether to show A or B, so the blend appears as a growing stipple of B
// rather than a uniform fade. It avoids the mid-point luminance dip of a linear
// crossfade. The alpha sidecar follows the identical per-pixel selection.
type Dissolve struct{}

var (
	_ Transition     = Dissolve{}
	_ MaskTransition = Dissolve{}
)

func (Dissolve) Name() string { return "dissolve" }

func (Dissolve) Frame(p float64, dst, a, b *clip.Frame)     { dissolveSelect(p, dst, a, b, 3) }
func (Dissolve) FrameMask(p float64, dst, a, b *clip.Frame) { dissolveSelect(p, dst, a, b, 1) }

// dissolveSelect copies, per pixel, A or B (bpp bytes each) by the Bayer
// threshold. Shared by Frame (RGB, bpp 3) and FrameMask (alpha, bpp 1) so the
// visible stipple and the alpha stipple are identical.
func dissolveSelect(p float64, dst, a, b *clip.Frame, bpp int) {
	for y := 0; y < dst.H; y++ {
		drow := dst.Pix[y*dst.Stride:]
		arow := a.Pix[y*a.Stride:]
		brow := b.Pix[y*b.Stride:]
		for x := 0; x < dst.W; x++ {
			src := arow
			if p > bayer8[y&7][x&7] {
				src = brow
			}
			o := x * bpp
			copy(drow[o:o+bpp], src[o:o+bpp])
		}
	}
}

// bayer8 is the normalized 8x8 ordered-dither threshold matrix: entry
// (m+0.5)/64 for the classic recursive Bayer values m in [0,63].
var bayer8 = func() [8][8]float64 {
	base := [8][8]int{
		{0, 32, 8, 40, 2, 34, 10, 42},
		{48, 16, 56, 24, 50, 18, 58, 26},
		{12, 44, 4, 36, 14, 46, 6, 38},
		{60, 28, 52, 20, 62, 30, 54, 22},
		{3, 35, 11, 43, 1, 33, 9, 41},
		{51, 19, 59, 27, 49, 17, 57, 25},
		{15, 47, 7, 39, 13, 45, 5, 37},
		{63, 31, 55, 23, 61, 29, 53, 21},
	}
	var m [8][8]float64
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			m[y][x] = (float64(base[y][x]) + 0.5) / 64
		}
	}
	return m
}()
