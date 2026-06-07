package transition

// Dir is the direction shared by the directional transitions (Wipe, Slide,
// Push). For Wipe it is the direction the reveal boundary travels; for Slide and
// Push it is the direction the incoming clip B travels. The zero value is Right.
type Dir int

const (
	// Right moves the action left-to-right (B reveals from the left edge).
	Right Dir = iota
	// Left moves the action right-to-left (B reveals from the right edge).
	Left
	// Up moves the action bottom-to-top (B reveals from the bottom edge).
	Up
	// Down moves the action top-to-bottom (B reveals from the top edge).
	Down
)

// String returns the direction name used in transition names.
func (d Dir) String() string {
	switch d {
	case Left:
		return "left"
	case Up:
		return "up"
	case Down:
		return "down"
	default:
		return "right"
	}
}

// Shape is the aperture outline used by Iris. The zero value is Circle.
type Shape int

const (
	// Circle is an elliptical aperture (scaled to the frame aspect).
	Circle Shape = iota
	// Diamond is a rhombus aperture (the L1 / taxicab metric).
	Diamond
	// Rectangle is an axis-aligned rectangular aperture (the L-infinity metric).
	Rectangle
)

// String returns the shape name used in transition names.
func (s Shape) String() string {
	switch s {
	case Diamond:
		return "diamond"
	case Rectangle:
		return "rectangle"
	default:
		return "circle"
	}
}
