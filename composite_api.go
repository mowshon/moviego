package mgo

import (
	"github.com/moviego/moviego/composite"
	"github.com/moviego/moviego/video"
)

// Position places a clip within a Composite. Build one with the keyword values
// (Center, Left, …), At for absolute pixels, or RelPos for a fraction of the
// free space on each axis.
type Position = composite.Position

// Placement keyword positions. The unspecified axis is centered.
var (
	Center = Position{Keyword: composite.PosCenter}
	Left   = Position{Keyword: composite.PosLeft}
	Right  = Position{Keyword: composite.PosRight}
	Top    = Position{Keyword: composite.PosTop}
	Bottom = Position{Keyword: composite.PosBottom}
)

// At places a clip's top-left at absolute pixel (x, y).
func At(x, y int) Position {
	return Position{X: float64(x), Y: float64(y)}
}

// RelPos places a clip relative to the free space: 0 flush to the start, 1 to
// the end, 0.5 centered, on each axis.
func RelPos(x, y float64) Position {
	return Position{X: x, Y: y, RelX: true, RelY: true}
}

// CompositeOptions configures Composite. A zero Size adopts the first clip's
// size; Transparent makes the canvas alpha-tracked (exported with an alpha
// channel); otherwise BGColor fills the background (default black).
type CompositeOptions struct {
	Size        Size
	BGColor     [3]byte
	Transparent bool
}

// Composite layers clips onto a canvas in z-order (see Layer), filling the
// background with black. Each clip's Position controls placement. The first
// clip sets the canvas size unless overridden.
func Composite(clips ...*Video) *Video {
	return CompositeWith(CompositeOptions{}, clips...)
}

// CompositeWith is Composite with explicit options (size, background color, or
// transparency).
func CompositeWith(opts CompositeOptions, clips ...*Video) *Video {
	if len(clips) == 0 {
		return &Video{err: composite.ErrEmptyConcat}
	}
	children := make([]composite.CompositeChild, 0, len(clips))
	for _, c := range clips {
		if c.err != nil {
			return &Video{err: c.err}
		}
		children = append(children, composite.CompositeChild{
			Clip:  c.inner,
			Start: c.inner.Start(),
			Pos:   c.pos,
			Layer: c.layer,
		})
	}
	n := composite.New(children, composite.Options{
		Size:        opts.Size,
		BGColor:     opts.BGColor,
		Transparent: opts.Transparent,
	})
	return &Video{inner: n}
}

// Concat joins clips end to end. Same-size clips use the fast chain path;
// differently-sized clips are centered on a canvas sized to the largest.
func Concat(clips ...*Video) *Video {
	inners, err := unwrap(clips)
	if err != nil {
		return &Video{err: err}
	}
	chain, chainErr := composite.ConcatChain(inners)
	if chainErr == nil {
		return &Video{inner: chain}
	}
	comp, err := composite.ConcatCompose(inners, composite.ConcatOptions{})
	if err != nil {
		return &Video{err: err}
	}
	return &Video{inner: comp}
}

// unwrap extracts the inner clips from facade handles, returning the first
// recorded build error.
func unwrap(clips []*Video) ([]video.VideoClip, error) {
	out := make([]video.VideoClip, 0, len(clips))
	for _, c := range clips {
		if c.err != nil {
			return nil, c.err
		}
		out = append(out, c.inner)
	}
	return out, nil
}
