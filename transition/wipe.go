package transition

import (
	"github.com/mowshon/moviego/v2/clip"
	"github.com/mowshon/moviego/v2/composite/blend"
)

// Wipe reveals B behind a moving straight edge travelling in Dir. Softness is
// the width of the feathered blend band as a fraction of the swept dimension
// (0 is a hard edge; ~0.1 a soft one). The zero value is a hard left-to-right
// wipe. The alpha sidecar is wiped by the identical moving edge.
type Wipe struct {
	Dir      Dir
	Softness float64
}

var (
	_ Transition     = Wipe{}
	_ MaskTransition = Wipe{}
)

func (w Wipe) Name() string { return "wipe-" + w.Dir.String() }

func (w Wipe) Frame(p float64, dst, a, b *clip.Frame)     { w.blend(p, dst, a, b, 3) }
func (w Wipe) FrameMask(p float64, dst, a, b *clip.Frame) { w.blend(p, dst, a, b, 1) }

// blend wipes A→B with the moving (optionally feathered) edge, bpp bytes per
// pixel. Shared by Frame (RGB) and FrameMask (alpha) so the edge position and
// softness match exactly.
func (w Wipe) blend(p float64, dst, a, b *clip.Frame, bpp int) {
	horizontal := w.Dir == Left || w.Dir == Right
	dim := dst.W
	if !horizontal {
		dim = dst.H
	}
	for y := 0; y < dst.H; y++ {
		drow := dst.Pix[y*dst.Stride:]
		arow := a.Pix[y*a.Stride:]
		brow := b.Pix[y*b.Stride:]
		if !horizontal {
			// The weight is constant across a row, so blend the whole row at once.
			t := unitToByte(wipeWeight(w.Dir, y, dim, p, w.Softness))
			blend.LerpRow(drow, arow, brow, dst.W*bpp, t)
			continue
		}
		for x := 0; x < dst.W; x++ {
			t := unitToByte(wipeWeight(w.Dir, x, dim, p, w.Softness))
			o := x * bpp
			for c := 0; c < bpp; c++ {
				drow[o+c] = blend.Blend1(brow[o+c], arow[o+c], t)
			}
		}
	}
}

// wipeWeight returns the 0..1 weight toward B for a pixel at coordinate c along
// the swept axis (length dim) at progress p. softness is the band width as a
// fraction of dim; a non-positive softness yields a hard step.
func wipeWeight(dir Dir, c, dim int, p, softness float64) float64 {
	pos := (float64(c) + 0.5) / float64(dim)
	soft := softness
	if soft < 0 {
		soft = 0
	}
	// The feather band is centered on the moving boundary, so a raw sweep from 0
	// to 1 leaves the band half on-screen at the endpoints — a half-blended seam
	// at p=0/p=1. Expanding the boundary travel by the band width (from -soft/2 to
	// 1+soft/2) clears the band off the frame at both ends, so p=0 is fully A and
	// p=1 fully B. With soft==0 this collapses to a boundary at p, the hard wipe.
	var delta float64
	switch dir {
	case Left, Up:
		bc := (1-p)*(1+soft) - soft/2
		delta = pos - bc
	default: // Right, Down
		bc := p*(1+soft) - soft/2
		delta = bc - pos
	}
	if soft <= 0 {
		if delta >= 0 {
			return 1
		}
		return 0
	}
	return clampUnit(delta/soft + 0.5)
}
