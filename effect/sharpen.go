package effect

import (
	"github.com/mowshon/moviego/v2/clip"
	"github.com/mowshon/moviego/v2/video"
)

// sharpenBlurRadius is the fixed radius of the blur the unsharp mask subtracts.
const sharpenBlurRadius = 2.0

// Sharpen enhances edges with an unsharp mask: it adds Amount times the
// difference between the clip and a blurred copy back onto the clip. Larger
// Amount sharpens more; a zero or negative Amount is a passthrough. It is a
// same-size per-pixel effect that transforms only RGB, rendered on the Go path.
type Sharpen struct {
	// Amount scales the high-frequency detail added back (e.g. 1.5).
	Amount float64
	// Start is when sharpening begins on the clip timeline; before it each frame
	// passes through unsharpened. The zero value sharpens from the beginning.
	Start clip.Time
}

func (s Sharpen) Targets() EffectTargets { return EffectTargets{Video: true} }

func (s Sharpen) ApplyVideo(c video.VideoClip) (video.VideoClip, error) {
	if s.Amount <= 0 {
		return c, nil
	}
	pool := clip.NewFramePool()
	fn := func(_ clip.Time, dst, src *clip.Frame) error {
		return sharpenInto(dst, src, s.Amount, pool)
	}
	return PixelFn{
		Name:  "sharpen",
		Fn:    fn,
		Start: s.Start,
	}.ApplyVideo(c)
}

// sharpenInto writes the unsharp-masked src into dst:
// dst = clamp(src + amount*(src - blur(src))). The blurred copy uses a pooled
// scratch frame.
func sharpenInto(dst, src *clip.Frame, amount float64, pool *clip.FramePool) error {
	blurred := pool.Get(src.W, src.H, src.Format)
	defer blurred.Release()
	if err := blurInto(blurred, src, sharpenBlurRadius, pool); err != nil {
		return err
	}
	for i := range src.Pix {
		detail := int(src.Pix[i]) - int(blurred.Pix[i])
		v := int(src.Pix[i]) + int(amount*float64(detail)+sign(detail)*0.5)
		dst.Pix[i] = clampByte(v)
	}
	return nil
}

func sign(v int) float64 {
	if v < 0 {
		return -1
	}
	return 1
}

func clampByte(v int) byte {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return byte(v)
}
