package imagex

import (
	"math"

	"github.com/mowshon/moviego/v2/clip"
)

// CropInto copies the dst.W x dst.H window of src whose top-left is (x, y) into
// dst. dst and src must share a pixel format, and the window must lie within
// src's bounds. It is a plain memory move and works for any bytes-per-pixel.
func CropInto(dst, src *clip.Frame, x, y int) error {
	if dst.Format != src.Format {
		return clip.Wrap("crop", clip.ErrVideoCorrupted)
	}
	if x < 0 || y < 0 || x+dst.W > src.W || y+dst.H > src.H {
		return clip.Wrap("crop", clip.ErrVideoCorrupted)
	}
	bpp := src.Format.BytesPerPixel()
	rowBytes := dst.W * bpp
	for row := 0; row < dst.H; row++ {
		srcOff := (y+row)*src.Stride + x*bpp
		dstOff := row * dst.Stride
		copy(dst.Pix[dstOff:dstOff+rowBytes], src.Pix[srcOff:srcOff+rowBytes])
	}
	return nil
}

// RotateInto writes src rotated clockwise by quarter*90 degrees into dst.
// quarter is taken modulo 4. For quarter 1 and 3 dst must have src's dimensions
// swapped; for 0 and 2 they must match. It works for any bytes-per-pixel.
func RotateInto(dst, src *clip.Frame, quarter int) error {
	if dst.Format != src.Format {
		return clip.Wrap("rotate", clip.ErrVideoCorrupted)
	}
	q := ((quarter % 4) + 4) % 4
	bpp := src.Format.BytesPerPixel()
	wantW, wantH := src.W, src.H
	if q == 1 || q == 3 {
		wantW, wantH = src.H, src.W
	}
	if dst.W != wantW || dst.H != wantH {
		return clip.Wrap("rotate", clip.ErrVideoCorrupted)
	}
	for sy := 0; sy < src.H; sy++ {
		for sx := 0; sx < src.W; sx++ {
			var dx, dy int
			switch q {
			case 0:
				dx, dy = sx, sy
			case 1: // 90 CW: (x,y) -> (H-1-y, x)
				dx, dy = src.H-1-sy, sx
			case 2: // 180
				dx, dy = src.W-1-sx, src.H-1-sy
			case 3: // 270 CW: (x,y) -> (y, W-1-x)
				dx, dy = sy, src.W-1-sx
			}
			so := sy*src.Stride + sx*bpp
			do := dy*dst.Stride + dx*bpp
			copy(dst.Pix[do:do+bpp], src.Pix[so:so+bpp])
		}
	}
	return nil
}

// FlipHInto writes src mirrored left-to-right into dst. dst and src must share a
// pixel format and dimensions. It works for any bytes-per-pixel.
func FlipHInto(dst, src *clip.Frame) error {
	if dst.Format != src.Format || dst.W != src.W || dst.H != src.H {
		return clip.Wrap("flip", clip.ErrVideoCorrupted)
	}
	bpp := src.Format.BytesPerPixel()
	for y := 0; y < src.H; y++ {
		s := src.Pix[y*src.Stride:]
		d := dst.Pix[y*dst.Stride:]
		for x := 0; x < src.W; x++ {
			copy(d[(src.W-1-x)*bpp:(src.W-x)*bpp], s[x*bpp:(x+1)*bpp])
		}
	}
	return nil
}

// FlipVInto writes src mirrored top-to-bottom into dst. dst and src must share a
// pixel format and dimensions. It is a row-order reversal, so a plain row copy.
func FlipVInto(dst, src *clip.Frame) error {
	if dst.Format != src.Format || dst.W != src.W || dst.H != src.H {
		return clip.Wrap("flip", clip.ErrVideoCorrupted)
	}
	rowBytes := src.W * src.Format.BytesPerPixel()
	for y := 0; y < src.H; y++ {
		s := src.Pix[y*src.Stride : y*src.Stride+rowBytes]
		do := (src.H - 1 - y) * dst.Stride
		copy(dst.Pix[do:do+rowBytes], s)
	}
	return nil
}

// PadInto fills dst with col, then copies src into it at (x, y). dst must be at
// least as large as src plus the offset, and share src's pixel format. RGB24 and
// RGBA frames take col as the RGB fill (alpha is set opaque for RGBA); a Gray8
// mask is filled transparent (0) so padded margins are not part of the clip.
func PadInto(dst, src *clip.Frame, x, y int, col [3]byte) error {
	if dst.Format != src.Format {
		return clip.Wrap("pad", clip.ErrVideoCorrupted)
	}
	if x < 0 || y < 0 || x+src.W > dst.W || y+src.H > dst.H {
		return clip.Wrap("pad", clip.ErrVideoCorrupted)
	}
	fillFrame(dst, col)
	bpp := src.Format.BytesPerPixel()
	rowBytes := src.W * bpp
	for row := 0; row < src.H; row++ {
		so := row * src.Stride
		do := (y+row)*dst.Stride + x*bpp
		copy(dst.Pix[do:do+rowBytes], src.Pix[so:so+rowBytes])
	}
	return nil
}

// RotateAngleInto writes src rotated clockwise by angle radians into dst,
// sampling each dst pixel from src with bilinear interpolation about the two
// frames' centers. dst may be a different size from src (an expanded canvas that
// holds the rotated bounding box); pixels that map outside src are filled with
// col (a Gray8 mask uses 0, a transparent margin). dst and src must share a
// pixel format. Exact quarter turns should use RotateInto, which is lossless.
func RotateAngleInto(dst, src *clip.Frame, angle float64, col [3]byte) error {
	if dst.Format != src.Format {
		return clip.Wrap("rotate", clip.ErrVideoCorrupted)
	}
	fillFrame(dst, col)
	bpp := src.Format.BytesPerPixel()
	// Inverse map: a dst pixel offset from the dst center rotates by -angle back
	// to a src offset, then shifts to the src center.
	sin, cos := math.Sincos(angle)
	dcx, dcy := float64(dst.W-1)/2, float64(dst.H-1)/2
	scx, scy := float64(src.W-1)/2, float64(src.H-1)/2
	maxX, maxY := float64(src.W-1), float64(src.H-1)
	for dy := 0; dy < dst.H; dy++ {
		ddy := float64(dy) - dcy
		d := dst.Pix[dy*dst.Stride:]
		for dx := 0; dx < dst.W; dx++ {
			ddx := float64(dx) - dcx
			sx := ddx*cos + ddy*sin + scx
			sy := -ddx*sin + ddy*cos + scy
			if sx < 0 || sy < 0 || sx > maxX || sy > maxY {
				continue // outside the source: keep the fill
			}
			sampleBilinear(d[dx*bpp:dx*bpp+bpp], src, sx, sy)
		}
	}
	return nil
}

// sampleBilinear writes the bilinearly interpolated src pixel at (x, y) into px.
// The coordinates are assumed to lie within src; neighbors are clamped to the
// last row/column at the edge.
func sampleBilinear(px []byte, src *clip.Frame, x, y float64) {
	x0, y0 := int(math.Floor(x)), int(math.Floor(y))
	x1, y1 := x0+1, y0+1
	if x1 >= src.W {
		x1 = src.W - 1
	}
	if y1 >= src.H {
		y1 = src.H - 1
	}
	tx, ty := x-float64(x0), y-float64(y0)
	w00, w10 := (1-tx)*(1-ty), tx*(1-ty)
	w01, w11 := (1-tx)*ty, tx*ty
	bpp := src.Format.BytesPerPixel()
	o00 := y0*src.Stride + x0*bpp
	o10 := y0*src.Stride + x1*bpp
	o01 := y1*src.Stride + x0*bpp
	o11 := y1*src.Stride + x1*bpp
	for c := 0; c < bpp; c++ {
		v := float64(src.Pix[o00+c])*w00 +
			float64(src.Pix[o10+c])*w10 +
			float64(src.Pix[o01+c])*w01 +
			float64(src.Pix[o11+c])*w11
		px[c] = byte(v + 0.5)
	}
}

// fillFrame paints every pixel of f. RGB24/RGBA use col (RGBA gets opaque
// alpha); Gray8 is filled with 0 (a transparent mask margin).
func fillFrame(f *clip.Frame, col [3]byte) {
	switch f.Format {
	case clip.Gray8:
		for i := range f.Pix {
			f.Pix[i] = 0
		}
	case clip.RGBA:
		for y := 0; y < f.H; y++ {
			d := f.Pix[y*f.Stride:]
			for x := 0; x < f.W; x++ {
				d[x*4], d[x*4+1], d[x*4+2], d[x*4+3] = col[0], col[1], col[2], 0xff
			}
		}
	default: // RGB24
		for y := 0; y < f.H; y++ {
			d := f.Pix[y*f.Stride:]
			for x := 0; x < f.W; x++ {
				d[x*3], d[x*3+1], d[x*3+2] = col[0], col[1], col[2]
			}
		}
	}
}
