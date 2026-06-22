package video

import (
	"errors"
	"sort"

	"github.com/mowshon/moviego/v2/audio"
	"github.com/mowshon/moviego/v2/clip"
	"github.com/mowshon/moviego/v2/ease"
)

// ErrTimeRemapPoints reports a TimeRemap call whose control points cannot define
// a curve: fewer than two points, a negative output time, or output times that
// do not strictly increase.
var ErrTimeRemapPoints = errors.New("time remap: need >= 2 points with strictly increasing, non-negative Out times")

// TimePoint pins an output (playback) time to a source (input) time. A sequence
// of them defines a piecewise time-remap curve: between consecutive points the
// source time advances (slow/normal/fast motion) or rewinds (reverse), shaped by
// an easing curve. Out is the playback timeline and must strictly increase; In
// may move in either direction.
type TimePoint struct {
	Out clip.Time
	In  clip.Time
}

// TimeRemap warps inner onto a new timeline: output time t shows the source at
// the piecewise curve through points, easing within each segment (nil curve
// means linear). The output duration is the last point's Out.
//
// Access is derived from the curve: an all-non-decreasing source (slow-mo, speed
// ramps, freeze) stays AccessLinear and keeps the clip on the parallel pipeline;
// a segment that rewinds the source is AccessRandom, routing the clip to the
// sequential engine (one forward decoder cannot serve a backward read cheaply).
// No faithful single FFmpeg filter expresses an arbitrary curve, so the node
// renders on the Go path. The audio sidecar is remapped by the same curve.
func TimeRemap(inner VideoClip, points []TimePoint, e ease.Func) (VideoClip, error) {
	pts, err := normalizeTimePoints(points)
	if err != nil {
		return nil, err
	}
	if e == nil {
		e = ease.Linear
	}
	dur := pts[len(pts)-1].Out
	mapT := remapFunc(pts, e)
	access := AccessLinear
	for i := 0; i+1 < len(pts); i++ {
		if pts[i+1].In < pts[i].In {
			access = AccessRandom
			break
		}
	}
	return newTimeRemap(inner, mapT, dur, access), nil
}

// Freeze holds the frame at source time at for hold, then resumes playback from
// at. The held source time never decreases, so the clip stays AccessLinear and
// pipeline-eligible; the forward decoder's last-frame cache serves the repeated
// reads. It needs a known duration and returns ErrNoDuration otherwise.
func Freeze(inner VideoClip, at, hold clip.Time) (VideoClip, error) {
	srcDur, ok := finiteDuration(inner)
	if !ok {
		return nil, clip.Wrap("freeze", clip.ErrNoDuration)
	}
	at = clampTime(at, 0, srcDur)
	if hold < 0 {
		hold = 0
	}
	mapT := func(t clip.Time) clip.Time {
		switch {
		case t < at:
			return t
		case t < at+hold:
			return at
		default:
			return t - hold
		}
	}
	return newTimeRemap(inner, mapT, srcDur+hold, AccessLinear), nil
}

// FreezeStart holds frame 0 for hold before the clip plays. It stays
// AccessLinear and pipeline-eligible.
func FreezeStart(inner VideoClip, hold clip.Time) (VideoClip, error) {
	srcDur, ok := finiteDuration(inner)
	if !ok {
		return nil, clip.Wrap("freeze start", clip.ErrNoDuration)
	}
	if hold < 0 {
		hold = 0
	}
	mapT := func(t clip.Time) clip.Time {
		if t < hold {
			return 0
		}
		return t - hold
	}
	return newTimeRemap(inner, mapT, srcDur+hold, AccessLinear), nil
}

// FreezeEnd holds the last frame for hold after the clip plays. It stays
// AccessLinear and pipeline-eligible.
func FreezeEnd(inner VideoClip, hold clip.Time) (VideoClip, error) {
	srcDur, ok := finiteDuration(inner)
	if !ok {
		return nil, clip.Wrap("freeze end", clip.ErrNoDuration)
	}
	if hold < 0 {
		hold = 0
	}
	last := lastFrameTime(inner, srcDur)
	mapT := func(t clip.Time) clip.Time {
		if t < srcDur {
			return t
		}
		return last
	}
	return newTimeRemap(inner, mapT, srcDur+hold, AccessLinear), nil
}

// newTimeRemap builds the TimeTransformNode shared by TimeRemap and the Freeze
// family, wiring the audio sidecar through the same output→source map.
func newTimeRemap(inner VideoClip, mapT func(clip.Time) clip.Time, dur clip.Time, access AccessClass) VideoClip {
	var au audio.AudioClip
	if a := inner.Audio(); a != nil {
		au = audio.TimeRemap(a, mapT, dur)
	}
	return &TimeTransformNode{
		inner:     inner,
		mapT:      mapT,
		dur:       dur,
		hasDur:    true,
		access:    access,
		audioClip: au,
	}
}

// remapFunc returns the output→source map for the normalized points: it clamps
// to the endpoints outside the curve and eases-interpolates In within the
// bracketing segment.
func remapFunc(pts []TimePoint, e ease.Func) func(clip.Time) clip.Time {
	first, last := pts[0], pts[len(pts)-1]
	return func(t clip.Time) clip.Time {
		if t <= first.Out {
			return first.In
		}
		if t >= last.Out {
			return last.In
		}
		i := 0
		for i+1 < len(pts) && pts[i+1].Out <= t {
			i++
		}
		a, b := pts[i], pts[i+1]
		span := b.Out - a.Out
		if span <= 0 {
			return b.In
		}
		p := e(float64(t-a.Out) / float64(span))
		return a.In + clip.Time(float64(b.In-a.In)*p)
	}
}

// normalizeTimePoints copies, sorts, and validates the control points: at least
// two, non-negative Out, strictly increasing Out, non-negative In.
func normalizeTimePoints(points []TimePoint) ([]TimePoint, error) {
	if len(points) < 2 {
		return nil, ErrTimeRemapPoints
	}
	pts := append([]TimePoint(nil), points...)
	sort.SliceStable(pts, func(i, j int) bool { return pts[i].Out < pts[j].Out })
	if pts[0].Out < 0 {
		return nil, ErrTimeRemapPoints
	}
	for i := range pts {
		if pts[i].In < 0 {
			return nil, ErrTimeRemapPoints
		}
		if i > 0 && pts[i].Out <= pts[i-1].Out {
			return nil, ErrTimeRemapPoints
		}
	}
	return pts, nil
}

// finiteDuration reports the clip's duration when it is known and bounded.
func finiteDuration(c VideoClip) (clip.Time, bool) {
	d := c.Duration()
	return d, clip.Finite(d)
}

// lastFrameTime returns a time that resolves to the clip's final frame: one
// frame-tick inside the duration when the rate is known (so a frame-index lookup
// floors to the last frame rather than one past it), else the duration itself.
// For rate-unknown sources this returns srcDur, which is one past the half-open
// source range. That is intentional: static sources (image, color) ignore the
// exact time, so any value is safe; file sources always carry a known rate and
// never reach this branch.
func lastFrameTime(c VideoClip, dur clip.Time) clip.Time {
	if rate, ok := c.Rate(); ok {
		if t := dur - rate.FrameTime(1); t >= 0 {
			return t
		}
		return 0
	}
	return dur
}
