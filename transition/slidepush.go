package transition

import (
	"math"

	"github.com/mowshon/moviego/v2/clip"
)

// slidePush composites B translated over A along dir — the shared machinery
// behind Slide and Push. When push is true, A is translated the opposite way so
// B pushes it off the far edge; when false, A is stationary and B slides over
// it. The two source regions are disjoint for push and B-over-A for slide, so B
// is tried first per pixel. bpp lets the same translation drive RGB (3) and the
// alpha sidecar (1), so the moving regions match exactly.
func slidePush(dst, a, b *clip.Frame, dir Dir, p float64, push bool, bpp int) {
	switch dir {
	case Left, Right:
		w := dst.W
		sx := int(math.Round(p * float64(w)))
		var aOff, bOff int
		if dir == Left {
			bOff = w - sx
			if push {
				aOff = -sx
			}
		} else {
			bOff = sx - w
			if push {
				aOff = sx
			}
		}
		for y := 0; y < dst.H; y++ {
			drow := dst.Pix[y*dst.Stride:]
			arow := a.Pix[y*a.Stride:]
			brow := b.Pix[y*b.Stride:]
			for x := 0; x < w; x++ {
				o := x * bpp
				if bs := x - bOff; bs >= 0 && bs < w {
					so := bs * bpp
					copy(drow[o:o+bpp], brow[so:so+bpp])
					continue
				}
				so := clampIndex(x-aOff, w) * bpp
				copy(drow[o:o+bpp], arow[so:so+bpp])
			}
		}
	default: // Up, Down: the shift is whole rows, so copy rows directly.
		h := dst.H
		sy := int(math.Round(p * float64(h)))
		var aOff, bOff int
		if dir == Down {
			bOff = sy - h
			if push {
				aOff = sy
			}
		} else {
			bOff = h - sy
			if push {
				aOff = -sy
			}
		}
		bytesPerRow := dst.W * bpp
		for y := 0; y < h; y++ {
			drow := dst.Pix[y*dst.Stride : y*dst.Stride+bytesPerRow]
			if bs := y - bOff; bs >= 0 && bs < h {
				copy(drow, b.Pix[bs*b.Stride:bs*b.Stride+bytesPerRow])
				continue
			}
			as := clampIndex(y-aOff, h)
			copy(drow, a.Pix[as*a.Stride:as*a.Stride+bytesPerRow])
		}
	}
}

// clampIndex clamps i into [0, n).
func clampIndex(i, n int) int {
	if i < 0 {
		return 0
	}
	if i >= n {
		return n - 1
	}
	return i
}
