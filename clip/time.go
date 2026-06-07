package clip

import (
	"math"
	"strconv"
	"strings"
	"time"
)

// Time is the internal time representation; strings/floats are parsed only at
// the API boundary.
type Time = time.Duration

// Infinite is the sentinel Duration/End for a clip whose length is unknown or
// unbounded (a generated source before WithDuration, an unbounded loop).
// Duration() and End() return it instead of a separate ok flag. Export requires
// a finite duration and rejects Infinite with ErrNoDuration.
const Infinite Time = Time(math.MaxInt64)

// Finite reports whether d is a real, bounded duration (not Infinite).
func Finite(d Time) bool { return d != Infinite }

// ParseTime parses a timestamp into a Time, mirroring MoviePy's
// convert_to_seconds: a bare "33.5" is seconds, "1:33,5" is min:sec (comma
// decimals allowed), "01:01:33.045" is hr:min:sec. Units beyond hours are
// dropped, matching MoviePy's zip truncation.
func ParseTime(s string) (Time, error) {
	parts := strings.Split(s, ":")
	factors := []float64{1, 60, 3600}
	n := len(parts)
	var total float64
	for i, p := range parts {
		j := n - 1 - i
		if j >= len(factors) {
			continue
		}
		p = strings.ReplaceAll(strings.TrimSpace(p), ",", ".")
		v, err := strconv.ParseFloat(p, 64)
		if err != nil {
			return 0, Wrap("parse time", err)
		}
		total += factors[j] * v
	}
	return Time(total * float64(time.Second)), nil
}
