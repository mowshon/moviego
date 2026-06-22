package transition

import "github.com/mowshon/moviego/v2/clip"

// Slide slides B in over a stationary A, entering from the edge opposite Dir's
// travel. The zero value slides B in from the left moving right. The alpha
// sidecar slides identically.
type Slide struct{ Dir Dir }

var (
	_ Transition     = Slide{}
	_ MaskTransition = Slide{}
)

func (s Slide) Name() string { return "slide-" + s.Dir.String() }

func (s Slide) Frame(p float64, dst, a, b *clip.Frame)     { slidePush(dst, a, b, s.Dir, p, false, 3) }
func (s Slide) FrameMask(p float64, dst, a, b *clip.Frame) { slidePush(dst, a, b, s.Dir, p, false, 1) }
