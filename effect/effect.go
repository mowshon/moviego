package effect

import (
	"github.com/mowshon/moviego/v2/audio"
	"github.com/mowshon/moviego/v2/clip"
	"github.com/mowshon/moviego/v2/ffmpeg"
	"github.com/mowshon/moviego/v2/video"
)

// EffectTargets declares which sidecars an effect transforms. It is the
// explicit replacement for MoviePy's apply_to_mask / apply_to_audio /
// audio_video_effect decorators and is verified against the propagation matrix.
type EffectTargets struct {
	Video bool
	Mask  bool
	Audio bool
}

// VideoEffect transforms a video clip into a new video clip.
type VideoEffect interface {
	ApplyVideo(video.VideoClip) (video.VideoClip, error)
	Targets() EffectTargets
}

// AudioEffect transforms an audio clip into a new audio clip.
type AudioEffect interface {
	ApplyAudio(audio.AudioClip) (audio.AudioClip, error)
}

// Chain applies video effects to c in order, returning the first error.
func Chain(c video.VideoClip, effects ...VideoEffect) (video.VideoClip, error) {
	for _, e := range effects {
		next, err := e.ApplyVideo(c)
		if err != nil {
			return nil, err
		}
		c = next
	}
	return c, nil
}

// geometry applies a pure size-changing transform. For static sources it maps
// once at construction (eager); otherwise it wraps the clip in a per-frame
// TransformNode. rgbFn and maskFn are time-independent. filterFn, when non-nil,
// advertises the equivalent FFmpeg filter on the resulting TransformNode so a
// linear graph can fuse; it is ignored on the eager static path (a static
// source bakes its pixels in and is not fused in v1).
func geometry(c video.VideoClip, outSize clip.Size, rgbFn, maskFn func(dst, src *clip.Frame) error, filterFn func(ffmpeg.FilterContext) (ffmpeg.FilterFragment, bool)) (video.VideoClip, error) {
	if sm, ok := c.(video.StaticMapper); ok {
		return sm.MapStatic(outSize, rgbFn, maskFn)
	}
	n := video.NewTransform(c, outSize,
		func(_ clip.Time, dst, src *clip.Frame) error { return rgbFn(dst, src) },
		func(_ clip.Time, dst, src *clip.Frame) error { return maskFn(dst, src) },
	)
	if filterFn != nil {
		n = n.WithFilter(filterFn)
	}
	return n, nil
}
