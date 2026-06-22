package composite

import "github.com/mowshon/moviego/v2/clip"

// Rect is an axis-aligned rectangle in pixel coordinates.
type Rect struct {
	X, Y, W, H int
}

// Empty reports whether the rectangle covers no pixels.
func (r Rect) Empty() bool { return r.W <= 0 || r.H <= 0 }

// Overlap clips a child of size childSize, placed with its top-left at (px, py),
// against a canvas of size canvas. It returns the destination rectangle (in
// canvas coordinates) and the source rectangle (in child coordinates) for the
// shared region; both have the same W/H. ok is false when there is no overlap,
// in which case the caller skips the layer.
func Overlap(canvas, childSize clip.Size, px, py int) (dst, src Rect, ok bool) {
	x1 := px
	y1 := py
	x2 := px + childSize.W
	y2 := py + childSize.H
	if x1 < 0 {
		x1 = 0
	}
	if y1 < 0 {
		y1 = 0
	}
	if x2 > canvas.W {
		x2 = canvas.W
	}
	if y2 > canvas.H {
		y2 = canvas.H
	}
	w := x2 - x1
	h := y2 - y1
	if w <= 0 || h <= 0 {
		return Rect{}, Rect{}, false
	}
	dst = Rect{X: x1, Y: y1, W: w, H: h}
	src = Rect{X: x1 - px, Y: y1 - py, W: w, H: h}
	return dst, src, true
}
