package transition

import (
	"github.com/mowshon/moviego/v2/clip"
	"github.com/mowshon/moviego/v2/composite/blend"
)

// CrossFade dissolves A into B by a straight alpha blend across the whole frame
// — the default, most-requested transition. It is the canonical MaskTransition:
// it blends the alpha sidecar the same way it blends RGB, so it is exact for
// transparent clips too.
type CrossFade struct{}

var (
	_ Transition     = CrossFade{}
	_ MaskTransition = CrossFade{}
)

func (CrossFade) Name() string { return "crossfade" }

func (CrossFade) Frame(p float64, dst, a, b *clip.Frame) {
	blend.LerpRow(dst.Pix, a.Pix, b.Pix, dst.W*dst.H*3, unitToByte(p))
}

func (CrossFade) FrameMask(p float64, dst, a, b *clip.Frame) {
	blend.LerpRow(dst.Pix, a.Pix, b.Pix, dst.W*dst.H, unitToByte(p))
}
