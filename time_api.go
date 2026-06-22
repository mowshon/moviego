package mgo

import "github.com/mowshon/moviego/v2/clip"

// ParseTime parses a timestamp into a Time. It accepts a bare number of seconds
// ("33.5"), min:sec ("1:33.5", with comma decimals allowed), or
// hr:min:sec ("01:23:45.678"), mirroring MoviePy's convert_to_seconds. It
// re-exports clip.ParseTime so callers need only import the facade.
func ParseTime(s string) (Time, error) { return clip.ParseTime(s) }
