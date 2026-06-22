package imagex

import (
	"image"

	xdraw "golang.org/x/image/draw"

	"github.com/mowshon/moviego/v2/clip"
)

// Resize returns a new frame of size, resampling src with the CatmullRom
// kernel. The output keeps src's pixel format.
func Resize(src *clip.Frame, size clip.Size) (*clip.Frame, error) {
	dst := clip.NewFrame(size.W, size.H, src.Format)
	if err := ResizeInto(dst, src); err != nil {
		return nil, err
	}
	return dst, nil
}

// ResizeInto resamples src into dst (whose W/H define the target), using
// CatmullRom. dst and src must share a pixel format. A no-op fast path copies
// when the sizes already match.
func ResizeInto(dst, src *clip.Frame) error {
	if dst.Format != src.Format {
		return clip.Wrap("resize", clip.ErrVideoCorrupted)
	}
	if dst.W == src.W && dst.H == src.H {
		copy(dst.Pix, src.Pix)
		return nil
	}
	switch src.Format {
	case clip.Gray8:
		s := grayView(src)
		d := image.NewGray(image.Rect(0, 0, dst.W, dst.H))
		xdraw.CatmullRom.Scale(d, d.Bounds(), s, s.Bounds(), xdraw.Over, nil)
		copyGrayOut(dst, d)
	default: // RGB24 / RGBA both resample through straight-alpha NRGBA
		s := nrgbaView(src)
		d := image.NewNRGBA(image.Rect(0, 0, dst.W, dst.H))
		xdraw.CatmullRom.Scale(d, d.Bounds(), s, s.Bounds(), xdraw.Over, nil)
		copyNRGBAOut(dst, d)
	}
	return nil
}

// nrgbaView returns an NRGBA image backed by a copy of src's pixels (RGB24 gets
// an opaque alpha). A copy is unavoidable because x/image needs a contiguous
// NRGBA source.
func nrgbaView(src *clip.Frame) *image.NRGBA {
	n := image.NewNRGBA(image.Rect(0, 0, src.W, src.H))
	if src.Format == clip.RGBA {
		for y := 0; y < src.H; y++ {
			copy(n.Pix[y*n.Stride:y*n.Stride+src.W*4], src.Pix[y*src.Stride:])
		}
		return n
	}
	for y := 0; y < src.H; y++ {
		s := src.Pix[y*src.Stride:]
		d := n.Pix[y*n.Stride:]
		for x := 0; x < src.W; x++ {
			d[x*4], d[x*4+1], d[x*4+2], d[x*4+3] = s[x*3], s[x*3+1], s[x*3+2], 0xff
		}
	}
	return n
}

func copyNRGBAOut(dst *clip.Frame, n *image.NRGBA) {
	if dst.Format == clip.RGBA {
		for y := 0; y < dst.H; y++ {
			copy(dst.Pix[y*dst.Stride:], n.Pix[y*n.Stride:y*n.Stride+dst.W*4])
		}
		return
	}
	for y := 0; y < dst.H; y++ {
		s := n.Pix[y*n.Stride:]
		d := dst.Pix[y*dst.Stride:]
		for x := 0; x < dst.W; x++ {
			d[x*3], d[x*3+1], d[x*3+2] = s[x*4], s[x*4+1], s[x*4+2]
		}
	}
}

func grayView(src *clip.Frame) *image.Gray {
	g := image.NewGray(image.Rect(0, 0, src.W, src.H))
	for y := 0; y < src.H; y++ {
		copy(g.Pix[y*g.Stride:y*g.Stride+src.W], src.Pix[y*src.Stride:])
	}
	return g
}

func copyGrayOut(dst *clip.Frame, g *image.Gray) {
	for y := 0; y < dst.H; y++ {
		copy(dst.Pix[y*dst.Stride:], g.Pix[y*g.Stride:y*g.Stride+dst.W])
	}
}
