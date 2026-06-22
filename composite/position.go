package composite

import "github.com/mowshon/moviego/v2/clip"

// ComputePosition resolves a child's top-left pixel coordinate within a canvas
// at composition time t. The result is int-truncated, matching MoviePy's
// compute_position (the final int() cast). Negative results are allowed: the
// caller clips the child against the canvas (see Overlap).
func ComputePosition(child, canvas clip.Size, pos Position, t clip.Time) (x, y int) {
	if pos.Animated != nil {
		ax, ay := pos.Animated(t)
		return int(ax), int(ay)
	}
	cx := (canvas.W - child.W) / 2
	cy := (canvas.H - child.H) / 2
	switch pos.Keyword {
	case PosCenter:
		return cx, cy
	case PosLeft:
		return 0, cy
	case PosRight:
		return canvas.W - child.W, cy
	case PosTop:
		return cx, 0
	case PosBottom:
		return cx, canvas.H - child.H
	}
	x = resolveAxis(pos.X, pos.RelX, canvas.W-child.W)
	y = resolveAxis(pos.Y, pos.RelY, canvas.H-child.H)
	return x, y
}

// resolveAxis maps one Position axis to integer pixels. A relative value is a
// fraction of the free space (canvasDim - childDim); an absolute value is
// truncated directly.
func resolveAxis(v float64, rel bool, free int) int {
	if rel {
		return int(v * float64(free))
	}
	return int(v)
}
