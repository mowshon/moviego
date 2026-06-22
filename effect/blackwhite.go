package effect

import (
	"github.com/mowshon/moviego/v2/clip"
	"github.com/mowshon/moviego/v2/video"
)

// BlackAndWhite converts RGB frames to grayscale from Start onward. It
// transforms only the RGB channel; the mask and audio are untouched.
type BlackAndWhite struct {
	// Start is when the grayscale conversion begins on the clip timeline. The
	// zero value applies the filter from the beginning.
	Start clip.Time
}

func (b BlackAndWhite) Targets() EffectTargets { return EffectTargets{Video: true} }

func (b BlackAndWhite) ApplyVideo(c video.VideoClip) (video.VideoClip, error) {
	start := b.Start
	rgbFn := func(t clip.Time, dst, src *clip.Frame) error {
		if t < start {
			copyFrameInto(dst, src)
			return nil
		}
		blackAndWhiteInto(dst, src)
		return nil
	}
	return video.NewTransform(c, c.Size(), rgbFn, nil), nil
}

func copyFrameInto(dst, src *clip.Frame) {
	rowBytes := src.W * src.Format.BytesPerPixel()
	for y := 0; y < src.H; y++ {
		copy(dst.Pix[y*dst.Stride:y*dst.Stride+rowBytes], src.Pix[y*src.Stride:y*src.Stride+rowBytes])
	}
}

func blackAndWhiteInto(dst, src *clip.Frame) {
	for y := 0; y < src.H; y++ {
		s := src.Pix[y*src.Stride:]
		d := dst.Pix[y*dst.Stride:]
		for x := 0; x < src.W; x++ {
			i := x * 3
			gray := byte((77*int(s[i]) + 150*int(s[i+1]) + 29*int(s[i+2]) + 128) >> 8)
			d[i], d[i+1], d[i+2] = gray, gray, gray
		}
	}
}
