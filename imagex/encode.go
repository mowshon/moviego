package imagex

import (
	"image"
	"image/jpeg"
	"image/png"
	"io"

	"github.com/mowshon/moviego/v2/clip"
)

// EncodePNG writes f to w as a PNG. RGBA frames keep their alpha; RGB24 and
// Gray8 frames are written opaque.
func EncodePNG(w io.Writer, f *clip.Frame) error {
	if err := png.Encode(w, toGoImage(f)); err != nil {
		return clip.Wrap("encode png", err)
	}
	return nil
}

// EncodeJPEG writes f to w as a JPEG at the given quality (1..100). Alpha is
// discarded, as JPEG has no alpha channel.
func EncodeJPEG(w io.Writer, f *clip.Frame, quality int) error {
	if err := jpeg.Encode(w, toGoImage(f), &jpeg.Options{Quality: quality}); err != nil {
		return clip.Wrap("encode jpeg", err)
	}
	return nil
}

// ToImage wraps a frame as a standard library image.Image (a copy), so a single
// decoded frame can be handed to a caller without exposing the packed buffer.
func ToImage(f *clip.Frame) image.Image { return toGoImage(f) }

// toGoImage wraps a frame as a standard image for the stdlib encoders.
func toGoImage(f *clip.Frame) image.Image {
	switch f.Format {
	case clip.Gray8:
		g := image.NewGray(image.Rect(0, 0, f.W, f.H))
		for y := 0; y < f.H; y++ {
			copy(g.Pix[y*g.Stride:y*g.Stride+f.W], f.Pix[y*f.Stride:])
		}
		return g
	case clip.RGBA:
		n := image.NewNRGBA(image.Rect(0, 0, f.W, f.H))
		for y := 0; y < f.H; y++ {
			copy(n.Pix[y*n.Stride:y*n.Stride+f.W*4], f.Pix[y*f.Stride:])
		}
		return n
	default: // RGB24
		n := image.NewNRGBA(image.Rect(0, 0, f.W, f.H))
		for y := 0; y < f.H; y++ {
			src := f.Pix[y*f.Stride:]
			dst := n.Pix[y*n.Stride:]
			for x := 0; x < f.W; x++ {
				dst[x*4], dst[x*4+1], dst[x*4+2], dst[x*4+3] = src[x*3], src[x*3+1], src[x*3+2], 0xff
			}
		}
		return n
	}
}
