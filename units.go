package mgo

import "time"

// Sec converts a floating-point number of seconds into a Time.
func Sec(s float64) Time {
	return Time(s * float64(time.Second))
}
