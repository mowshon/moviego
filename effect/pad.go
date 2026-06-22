package effect

import (
	"errors"

	"github.com/mowshon/moviego/v2/clip"
	"github.com/mowshon/moviego/v2/imagex"
	"github.com/mowshon/moviego/v2/video"
)

// ErrInvalidPad reports a Pad target smaller than the source on some axis: there
// is no room to letterbox, so the request is rejected rather than cropping.
var ErrInvalidPad = errors.New("pad: target must be at least the source size on each axis")

// Pad centers the clip on a W x H canvas filled with Color, the common
// letterbox/pillarbox op for aspect conversion. The output size is exactly
// W x H. A clip with a mask keeps it (padded margins are transparent); an opaque
// clip's margins are the solid Color. The target must be at least the source
// size on each axis.
type Pad struct {
	W, H  int
	Color [3]byte
}

func (p Pad) Targets() EffectTargets { return EffectTargets{Video: true, Mask: true} }

func (p Pad) ApplyVideo(c video.VideoClip) (video.VideoClip, error) {
	in := c.Size()
	if p.W < in.W || p.H < in.H {
		return nil, ErrInvalidPad
	}
	out := clip.Size{W: p.W, H: p.H}
	offX, offY := (p.W-in.W)/2, (p.H-in.H)/2
	fn := func(dst, src *clip.Frame) error { return imagex.PadInto(dst, src, offX, offY, p.Color) }
	// No faithful single-filter form here (FFmpeg's pad takes its own offsets and
	// fill color), so the node renders on the Go path.
	return geometry(c, out, fn, fn, nil)
}

// FitTo scales the clip to fit within W x H preserving aspect ratio, then
// letterboxes it to exactly W x H with Color. It composes Resize (aspect-
// preserving) and Pad, so a portrait clip fits a landscape frame without
// distortion. The output size is exactly W x H.
type FitTo struct {
	W, H  int
	Color [3]byte
}

func (f FitTo) Targets() EffectTargets { return EffectTargets{Video: true, Mask: true} }

func (f FitTo) ApplyVideo(c video.VideoClip) (video.VideoClip, error) {
	if f.W < 1 || f.H < 1 {
		return nil, ErrInvalidResize
	}
	in := c.Size()
	fw, fh := fitSize(in.W, in.H, f.W, f.H)
	return Chain(c, Resize{Width: fw, Height: fh}, Pad{W: f.W, H: f.H, Color: f.Color})
}

// fitSize returns the largest width/height that fits inside (maxW, maxH) while
// keeping the (w, h) aspect ratio. Each dimension is at least 1.
func fitSize(w, h, maxW, maxH int) (int, int) {
	scale := float64(maxW) / float64(w)
	if s := float64(maxH) / float64(h); s < scale {
		scale = s
	}
	fw, fh := round(float64(w)*scale), round(float64(h)*scale)
	if fw < 1 {
		fw = 1
	}
	if fh < 1 {
		fh = 1
	}
	if fw > maxW {
		fw = maxW
	}
	if fh > maxH {
		fh = maxH
	}
	return fw, fh
}
