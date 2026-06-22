package transition

import "github.com/mowshon/moviego/v2/clip"

// Push slides B in while pushing A out the far side, both moving together in
// Dir. The zero value pushes left-to-right. The alpha sidecar is pushed
// identically.
type Push struct{ Dir Dir }

var (
	_ Transition     = Push{}
	_ MaskTransition = Push{}
)

func (s Push) Name() string { return "push-" + s.Dir.String() }

func (s Push) Frame(p float64, dst, a, b *clip.Frame)     { slidePush(dst, a, b, s.Dir, p, true, 3) }
func (s Push) FrameMask(p float64, dst, a, b *clip.Frame) { slidePush(dst, a, b, s.Dir, p, true, 1) }
