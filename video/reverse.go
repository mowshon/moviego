package video

import (
	"time"

	"github.com/mowshon/moviego/v2/audio"
	"github.com/mowshon/moviego/v2/clip"
)

// Reverse plays inner backward: output frame i shows source frame N-1-i, where
// N is the clip's frame count. It reflects the source frame index rather than
// mirroring continuous time, so output frame 0 is exactly the last source frame
// and the final output frame is exactly the first — the round-trip the golden
// tests assert. Reverse reads the source non-monotonically, so it is
// AccessRandom and runs on the sequential engine; the engine installs a
// backward-buffered reader so each reverse step is O(1) amortized. The audio
// sidecar is reversed by a matching sample flip. It needs a known duration and
// returns ErrNoDuration otherwise.
func Reverse(inner VideoClip) (VideoClip, error) {
	srcDur, ok := finiteDuration(inner)
	if !ok {
		return nil, clip.Wrap("reverse", clip.ErrNoDuration)
	}
	var au audio.AudioClip
	if a := inner.Audio(); a != nil {
		au = audio.Reverse(a)
	}
	return &TimeTransformNode{
		inner:     inner,
		mapT:      reverseMap(inner, srcDur),
		dur:       srcDur,
		hasDur:    true,
		access:    AccessRandom,
		audioClip: au,
	}, nil
}

// reverseMap builds the output→source time map that reflects the frame index.
// A rated source reflects exactly (output frame i -> source frame N-1-i); a
// rate-less static source (image/color) ignores the exact time, so a clamped
// continuous mirror is sufficient.
func reverseMap(inner VideoClip, srcDur clip.Time) func(clip.Time) clip.Time {
	rate, ok := inner.Rate()
	if !ok {
		return func(t clip.Time) clip.Time { return clampTime(srcDur-t, 0, srcDur) }
	}
	last := FrameCount(rate, srcDur) - 1
	if last < 0 {
		last = 0
	}
	return func(t clip.Time) clip.Time {
		k := last - rate.TimeToFrame(t)
		if k < 0 {
			k = 0
		} else if k > last {
			k = last
		}
		return rate.FrameTime(k)
	}
}

// FrameCount returns the number of frames the index-driven schedule emits for
// dur at rate: the count of indices i with FrameTime(i) < dur. It mirrors the
// render planner's frame count so a reversed clip's first output frame lands on
// the source's true last frame.
func FrameCount(rate clip.Rate, dur clip.Time) int {
	if dur <= 0 || !clip.Finite(dur) || rate.Num <= 0 || rate.Den <= 0 {
		return 0
	}
	est := int(int64(dur) * int64(rate.Num) / (int64(rate.Den) * int64(time.Second)))
	if est < 0 {
		est = 0
	}
	for rate.FrameTime(est) < dur {
		est++
	}
	for est > 0 && rate.FrameTime(est-1) >= dur {
		est--
	}
	return est
}
