package video

import (
	"context"

	"github.com/mowshon/moviego/v2/audio"
	"github.com/mowshon/moviego/v2/clip"
	"github.com/mowshon/moviego/v2/imagex"
)

// ImageNode is a static image source. It decodes its pixels once at
// construction and serves every frame from that buffer; SourceAccess is
// AccessStatic and it is parallel-safe. A clip with a non-opaque source carries
// a Gray8 mask sidecar split from the alpha channel.
//
// The cached buffers are never handed out by reference: RenderInto/FrameInto/
// MaskInto always copy into the caller's destination, so a mutated frame can
// never corrupt the node's cache.
type ImageNode struct {
	rgb   *clip.Frame // RGB24, owned, immutable after construction
	alpha *clip.Frame // Gray8 mask, or nil when fully opaque
	size  clip.Size

	start   clip.Time
	dur     clip.Time
	hasDur  bool
	rate    clip.Rate
	hasRate bool
}

// NewImage builds an image source from a decoded RGB frame and an optional
// Gray8 mask. It takes ownership of the buffers; callers must not mutate them
// afterward.
func NewImage(rgb, alpha *clip.Frame) *ImageNode {
	return &ImageNode{rgb: rgb, alpha: alpha, size: clip.Size{W: rgb.W, H: rgb.H}}
}

// OpenImage decodes path into an image source, splitting a transparent image's
// alpha channel into a mask sidecar.
func OpenImage(path string) (*ImageNode, error) {
	f, err := imagex.DecodeFile(path)
	if err != nil {
		return nil, err
	}
	if f.Format == clip.RGBA {
		rgb, alpha, err := imagex.SplitAlpha(f)
		if err != nil {
			return nil, err
		}
		return NewImage(rgb, alpha), nil
	}
	return NewImage(f, nil), nil
}

// Timeline metadata. A static image has no intrinsic duration or rate; both are
// set by the editing API (WithDuration) or injected by the export planner.

func (n *ImageNode) Start() clip.Time { return n.start }

func (n *ImageNode) Duration() clip.Time { return clip.DurationOr(n.dur, n.hasDur) }

func (n *ImageNode) End() clip.Time { return clip.EndOr(n.start, n.dur, n.hasDur) }

func (n *ImageNode) WithStart(t clip.Time) clip.Clip {
	c := *n
	c.start = t
	return &c
}

func (n *ImageNode) WithDuration(d clip.Time) clip.Clip {
	c := *n
	c.dur, c.hasDur = d, true
	return &c
}

func (n *ImageNode) WithEnd(end clip.Time, changeDuration bool) clip.Clip {
	c := *n
	if changeDuration {
		c.dur, c.hasDur = end-c.start, true
	} else if c.hasDur {
		c.start = end - c.dur
	}
	return &c
}

// WithRate sets the frame rate reported by Rate (used by the facade and the
// export planner to give a static source a concrete output rate).
func (n *ImageNode) WithRate(r clip.Rate) *ImageNode {
	c := *n
	c.rate, c.hasRate = r, true
	return &c
}

// Video metadata.

func (n *ImageNode) Size() clip.Size           { return n.size }
func (n *ImageNode) Rate() (clip.Rate, bool)   { return n.rate, n.hasRate }
func (n *ImageNode) HasMask() bool             { return n.alpha != nil }
func (n *ImageNode) Audio() audio.AudioClip    { return nil }
func (n *ImageNode) ParallelSafe() bool        { return true }
func (n *ImageNode) SourceAccess() AccessClass { return AccessStatic }

// Rendering copies the cached buffers into the caller's destinations.

func (n *ImageNode) RenderInto(_ context.Context, _ clip.Time, rgbDst, alphaDst *clip.Frame) (bool, error) {
	copyPixels(rgbDst, n.rgb)
	if n.alpha != nil && alphaDst != nil {
		copyPixels(alphaDst, n.alpha)
		return true, nil
	}
	return false, nil
}

func (n *ImageNode) FrameInto(_ context.Context, _ clip.Time, dst *clip.Frame) error {
	copyPixels(dst, n.rgb)
	return nil
}

func (n *ImageNode) MaskInto(_ context.Context, _ clip.Time, dst *clip.Frame) (bool, error) {
	if n.alpha == nil {
		return false, nil
	}
	copyPixels(dst, n.alpha)
	return true, nil
}

// MapStatic applies a pure geometric transform once and returns a new static
// image of outSize, preserving the timeline and rate.
func (n *ImageNode) MapStatic(outSize clip.Size, rgbFn, maskFn func(dst, src *clip.Frame) error) (VideoClip, error) {
	rgb := clip.NewFrame(outSize.W, outSize.H, clip.RGB24)
	if err := rgbFn(rgb, n.rgb); err != nil {
		return nil, err
	}
	var alpha *clip.Frame
	if n.alpha != nil {
		alpha = clip.NewFrame(outSize.W, outSize.H, clip.Gray8)
		if maskFn != nil {
			if err := maskFn(alpha, n.alpha); err != nil {
				return nil, err
			}
		} else {
			copyPixels(alpha, n.alpha)
		}
	}
	c := *n
	c.rgb, c.alpha, c.size = rgb, alpha, outSize
	return &c, nil
}

// Close releases nothing: an image source owns only memory.
func (n *ImageNode) Close() error { return nil }
