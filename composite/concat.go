package composite

import (
	"context"
	"errors"

	"github.com/mowshon/moviego/v2/audio"
	"github.com/mowshon/moviego/v2/clip"
	"github.com/mowshon/moviego/v2/video"
)

// ErrEmptyConcat is returned when a concatenation is built with no children.
var ErrEmptyConcat = errors.New("concatenate: no clips")

// ErrSizeMismatch is returned by ConcatChain when its children are not all the
// same size (chain concat does not resize or pad; use ConcatCompose for that).
var ErrSizeMismatch = errors.New("concatenate: chain requires uniform clip size")

// ErrNoDuration is returned when a concatenation child has an unknown duration,
// so its placement on the timeline cannot be resolved.
var ErrNoDuration = errors.New("concatenate: child has unknown duration")

// ConcatChainNode plays children back to back on one track. All children must
// share a size; the node selects the active child by cumulative timing and
// renders it at its local time. If any child has a mask the whole chain has a
// mask, and a maskless child's region is treated as fully opaque.
type ConcatChainNode struct {
	children []video.VideoClip
	timings  []clip.Time // start time of each child; len == len(children)
	size     clip.Size
	dur      clip.Time
	rate     clip.Rate
	hasRate  bool
	hasMask  bool
	access   video.AccessClass
	parallel bool
	start    clip.Time
}

// Compile-time guarantee.
var _ video.VideoClip = (*ConcatChainNode)(nil)

// ConcatChain joins children end to end. It requires at least one child, a
// known duration for every child, and a uniform size.
func ConcatChain(children []video.VideoClip) (*ConcatChainNode, error) {
	if len(children) == 0 {
		return nil, clip.Wrap("concat chain", ErrEmptyConcat)
	}
	size := children[0].Size()
	timings := make([]clip.Time, len(children))
	var acc clip.Time
	parallel := true
	access := video.AccessStatic
	hasMask := false
	for i, c := range children {
		if c.Size() != size {
			return nil, clip.Wrap("concat chain", ErrSizeMismatch)
		}
		d := c.Duration()
		if !clip.Finite(d) {
			return nil, clip.Wrap("concat chain", ErrNoDuration)
		}
		timings[i] = acc
		acc += d
		if c.HasMask() {
			hasMask = true
		}
		if !c.ParallelSafe() {
			parallel = false
		}
		if a := c.SourceAccess(); a > access {
			access = a
		}
	}
	rate, hasRate := maxRate(children)
	return &ConcatChainNode{
		children: children,
		timings:  timings,
		size:     size,
		dur:      acc,
		rate:     rate,
		hasRate:  hasRate,
		hasMask:  hasMask,
		access:   access,
		parallel: parallel,
	}, nil
}

func maxRate(children []video.VideoClip) (clip.Rate, bool) {
	var r clip.Rate
	has := false
	for _, c := range children {
		if cr, ok := c.Rate(); ok {
			if !has {
				r, has = cr, true
			} else {
				r = clip.MaxRate(r, cr)
			}
		}
	}
	return r, has
}

// activeChild returns the index of the child playing at composition time t and
// the child's local time. t at or past the total duration clamps to the last
// child's final frame.
func (n *ConcatChainNode) activeChild(t clip.Time) (int, clip.Time) {
	i := 0
	for j := range n.children {
		if n.timings[j] <= t {
			i = j
		} else {
			break
		}
	}
	return i, t - n.timings[i]
}

// Timeline metadata.

func (n *ConcatChainNode) Start() clip.Time    { return n.start }
func (n *ConcatChainNode) Duration() clip.Time { return n.dur }

func (n *ConcatChainNode) End() clip.Time { return n.start + n.dur }

func (n *ConcatChainNode) WithStart(t clip.Time) clip.Clip {
	c := *n
	c.start = t
	return &c
}

func (n *ConcatChainNode) WithDuration(d clip.Time) clip.Clip {
	c := *n
	c.dur = d
	return &c
}

func (n *ConcatChainNode) WithEnd(end clip.Time, changeDuration bool) clip.Clip {
	c := *n
	if changeDuration {
		c.dur = end - c.start
	} else {
		c.start = end - c.dur
	}
	return &c
}

// Video metadata.

func (n *ConcatChainNode) Size() clip.Size         { return n.size }
func (n *ConcatChainNode) Rate() (clip.Rate, bool) { return n.rate, n.hasRate }
func (n *ConcatChainNode) HasMask() bool           { return n.hasMask }

// Audio mixes each child's audio sidecar placed at its cumulative start time, so
// the tracks play back to back in sync with the video (RenderInto runs child i
// at local time t-timings[i]). Because the children abut and the mix gate is
// half-open, there is no overlap at a boundary. A child without audio leaves its
// span silent; the result is nil only when no child has audio.
func (n *ConcatChainNode) Audio() audio.AudioClip {
	tracks := make([]audio.AudioClip, 0, len(n.children))
	for i, c := range n.children {
		a := c.Audio()
		if a == nil {
			continue
		}
		tracks = append(tracks, a.WithStart(n.timings[i]).(audio.AudioClip))
	}
	if len(tracks) == 0 {
		return nil
	}
	// A chain always has a known duration (overridable via WithDuration/WithEnd);
	// mirror it onto the audio so a nested, shortened chain truncates its mix to
	// match video gating instead of over-playing the full sequence.
	return audio.Mix(tracks...).WithDuration(n.dur).(audio.AudioClip)
}

func (n *ConcatChainNode) ParallelSafe() bool              { return n.parallel }
func (n *ConcatChainNode) SourceAccess() video.AccessClass { return n.access }

// Children exposes the concatenated clips for planner graph traversal.
func (n *ConcatChainNode) Children() []video.VideoClip { return n.children }

// Rendering delegates to the active child. When the chain has a mask but the
// active child does not, the child's region is filled opaque (alpha 255).

func (n *ConcatChainNode) RenderInto(ctx context.Context, t clip.Time, rgbDst, alphaDst *clip.Frame) (bool, error) {
	i, local := n.activeChild(t)
	child := n.children[i]
	if !child.HasMask() {
		if err := child.FrameInto(ctx, local, rgbDst); err != nil {
			return false, err
		}
		if n.hasMask && alphaDst != nil {
			fillOpaque(alphaDst)
			return true, nil
		}
		return false, nil
	}
	return child.RenderInto(ctx, local, rgbDst, alphaDst)
}

func (n *ConcatChainNode) FrameInto(ctx context.Context, t clip.Time, dst *clip.Frame) error {
	i, local := n.activeChild(t)
	return n.children[i].FrameInto(ctx, local, dst)
}

func (n *ConcatChainNode) MaskInto(ctx context.Context, t clip.Time, dst *clip.Frame) (bool, error) {
	if !n.hasMask {
		return false, nil
	}
	i, local := n.activeChild(t)
	child := n.children[i]
	if !child.HasMask() {
		fillOpaque(dst)
		return true, nil
	}
	return child.MaskInto(ctx, local, dst)
}

// Close releases nothing: a concat never owns the children it was given.
func (n *ConcatChainNode) Close() error { return nil }

// ConcatOptions tune ConcatCompose. Padding is the gap between consecutive
// clips; a negative padding overlaps them (useful for crossfades).
type ConcatOptions struct {
	Padding clip.Time
}

// ConcatComposeNode joins differently-sized clips by centering each on a canvas
// sized to the largest width and height and placing them at cumulative start
// times. It is a CompositeNode underneath, so it shares the blend paths and the
// one-pass RenderInto.
type ConcatComposeNode struct {
	*CompositeNode
}

// ConcatCompose joins children with centering and optional padding. Each child
// must have a known duration. The canvas is the max child width and height.
func ConcatCompose(children []video.VideoClip, opts ConcatOptions) (*ConcatComposeNode, error) {
	if len(children) == 0 {
		return nil, clip.Wrap("concat compose", ErrEmptyConcat)
	}
	maxW, maxH := 0, 0
	for _, c := range children {
		s := c.Size()
		if s.W > maxW {
			maxW = s.W
		}
		if s.H > maxH {
			maxH = s.H
		}
	}
	canvas := clip.Size{W: maxW, H: maxH}

	cs := make([]CompositeChild, len(children))
	var start clip.Time
	for i, c := range children {
		d := c.Duration()
		if !clip.Finite(d) {
			return nil, clip.Wrap("concat compose", ErrNoDuration)
		}
		cs[i] = CompositeChild{
			Clip:  c,
			Start: start,
			Pos:   Position{Keyword: PosCenter},
			Layer: i,
		}
		start += d + opts.Padding
	}
	comp := New(cs, Options{Size: canvas})
	return &ConcatComposeNode{CompositeNode: comp}, nil
}

// fillOpaque sets every byte of a Gray8 mask to 255.
func fillOpaque(a *clip.Frame) {
	for i := range a.Pix {
		a.Pix[i] = 255
	}
}
