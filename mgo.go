package mgo

import "github.com/mowshon/moviego/v2/clip"

// Re-exported core value types so callers need only import the facade.
type (
	// Time is a timeline duration; build one with Sec or clip.ParseTime.
	Time = clip.Time
	// Rate is a rational frame rate.
	Rate = clip.Rate
	// Size is a pixel width/height.
	Size = clip.Size
)

// Infinite is the sentinel Duration/End reported by a clip whose length is
// unknown or unbounded. Compare a Duration against it, or use Finite.
const Infinite = clip.Infinite

// Finite reports whether d is a real, bounded duration (not Infinite).
func Finite(d Time) bool { return clip.Finite(d) }
