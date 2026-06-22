package video

import (
	"context"

	"github.com/mowshon/moviego/v2/audio"
	"github.com/mowshon/moviego/v2/clip"
)

// ColorNode is a generated solid-color source, commonly a composite background.
// It precomputes its buffer once and copies it into each destination, so it is
// parallel-safe and AccessStatic. Like an image, it has no intrinsic duration
// or rate; both come from the editing API or the export planner.
type ColorNode struct {
	rgb  *clip.Frame // precomputed solid buffer, immutable after construction
	size clip.Size
	col  [3]byte

	start   clip.Time
	dur     clip.Time
	hasDur  bool
	rate    clip.Rate
	hasRate bool
}

// NewColor builds a size-sized solid color source.
func NewColor(size clip.Size, col [3]byte) *ColorNode {
	rgb := clip.NewFrame(size.W, size.H, clip.RGB24)
	for i := 0; i < len(rgb.Pix); i += 3 {
		rgb.Pix[i], rgb.Pix[i+1], rgb.Pix[i+2] = col[0], col[1], col[2]
	}
	return &ColorNode{rgb: rgb, size: size, col: col}
}

// Timeline metadata.

func (n *ColorNode) Start() clip.Time { return n.start }

func (n *ColorNode) Duration() clip.Time { return clip.DurationOr(n.dur, n.hasDur) }

func (n *ColorNode) End() clip.Time { return clip.EndOr(n.start, n.dur, n.hasDur) }

func (n *ColorNode) WithStart(t clip.Time) clip.Clip {
	c := *n
	c.start = t
	return &c
}

func (n *ColorNode) WithDuration(d clip.Time) clip.Clip {
	c := *n
	c.dur, c.hasDur = d, true
	return &c
}

func (n *ColorNode) WithEnd(end clip.Time, changeDuration bool) clip.Clip {
	c := *n
	if changeDuration {
		c.dur, c.hasDur = end-c.start, true
	} else if c.hasDur {
		c.start = end - c.dur
	}
	return &c
}

// WithRate sets the frame rate reported by Rate.
func (n *ColorNode) WithRate(r clip.Rate) *ColorNode {
	c := *n
	c.rate, c.hasRate = r, true
	return &c
}

// Video metadata.

func (n *ColorNode) Size() clip.Size           { return n.size }
func (n *ColorNode) Rate() (clip.Rate, bool)   { return n.rate, n.hasRate }
func (n *ColorNode) HasMask() bool             { return false }
func (n *ColorNode) Audio() audio.AudioClip    { return nil }
func (n *ColorNode) ParallelSafe() bool        { return true }
func (n *ColorNode) SourceAccess() AccessClass { return AccessStatic }

// Rendering copies the precomputed solid buffer.

func (n *ColorNode) RenderInto(_ context.Context, _ clip.Time, rgbDst, _ *clip.Frame) (bool, error) {
	copyPixels(rgbDst, n.rgb)
	return false, nil
}

func (n *ColorNode) FrameInto(_ context.Context, _ clip.Time, dst *clip.Frame) error {
	copyPixels(dst, n.rgb)
	return nil
}

func (n *ColorNode) MaskInto(context.Context, clip.Time, *clip.Frame) (bool, error) {
	return false, nil
}

// MapStatic applies a geometric transform once and returns a new static image
// of outSize. The result is an ImageNode because a transformed solid is no
// longer guaranteed to be uniform if ColorNode grows new fill modes. maskFn is
// unused: a color source has no mask.
func (n *ColorNode) MapStatic(outSize clip.Size, rgbFn, _ func(dst, src *clip.Frame) error) (VideoClip, error) {
	rgb := clip.NewFrame(outSize.W, outSize.H, clip.RGB24)
	if err := rgbFn(rgb, n.rgb); err != nil {
		return nil, err
	}
	img := &ImageNode{
		rgb:     rgb,
		size:    outSize,
		start:   n.start,
		dur:     n.dur,
		hasDur:  n.hasDur,
		rate:    n.rate,
		hasRate: n.hasRate,
	}
	return img, nil
}

// Close releases nothing: a color source owns only memory.
func (n *ColorNode) Close() error { return nil }
