package imagex

import (
	"bytes"
	"image"
	_ "image/gif" // register GIF decoder
	_ "image/jpeg"
	_ "image/png"
	"os"

	"github.com/mowshon/moviego/v2/clip"
)

// DecodeFile reads path and decodes it into a clip.Frame. The result is RGBA
// when the source carries a non-opaque alpha channel and RGB24 otherwise, so
// downstream code can cheaply test for transparency by the frame format.
func DecodeFile(path string) (*clip.Frame, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, clip.Wrap("decode "+path, err)
	}
	f, err := DecodeBytes(data)
	if err != nil {
		return nil, clip.Wrap("decode "+path, err)
	}
	return f, nil
}

// DecodeBytes decodes an encoded image from data, returning RGBA when it has a
// non-opaque alpha channel and RGB24 otherwise.
func DecodeBytes(data []byte) (*clip.Frame, error) {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, clip.Wrap("decode image", err)
	}
	return fromImage(img), nil
}

// FromImage converts an in-memory image.Image into a packed clip.Frame,
// choosing RGBA when any pixel is not fully opaque and RGB24 otherwise. It is
// the boundary the text rasterizer uses to hand a rendered canvas to the video
// graph without going through an encode/decode round trip.
func FromImage(img image.Image) *clip.Frame { return fromImage(img) }

// fromImage converts a decoded image into a packed clip.Frame, choosing RGBA
// only when some pixel is not fully opaque.
func fromImage(img image.Image) *clip.Frame {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	nrgba := toNRGBA(img)
	if hasTransparency(nrgba) {
		f := clip.NewFrame(w, h, clip.RGBA)
		for y := 0; y < h; y++ {
			copy(f.Pix[y*f.Stride:y*f.Stride+w*4], nrgba.Pix[y*nrgba.Stride:])
		}
		return f
	}
	f := clip.NewFrame(w, h, clip.RGB24)
	for y := 0; y < h; y++ {
		src := nrgba.Pix[y*nrgba.Stride:]
		dst := f.Pix[y*f.Stride:]
		for x := 0; x < w; x++ {
			dst[x*3], dst[x*3+1], dst[x*3+2] = src[x*4], src[x*4+1], src[x*4+2]
		}
	}
	return f
}

// toNRGBA returns img as an *image.NRGBA, converting only when necessary so the
// common already-NRGBA case is zero-copy.
func toNRGBA(img image.Image) *image.NRGBA {
	if n, ok := img.(*image.NRGBA); ok {
		return n
	}
	b := img.Bounds()
	dst := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			dst.Set(x, y, img.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return dst
}

// hasTransparency reports whether any pixel's alpha is below 255.
func hasTransparency(img *image.NRGBA) bool {
	for y := 0; y < img.Rect.Dy(); y++ {
		row := img.Pix[y*img.Stride:]
		for x := 0; x < img.Rect.Dx(); x++ {
			if row[x*4+3] != 0xff {
				return true
			}
		}
	}
	return false
}
