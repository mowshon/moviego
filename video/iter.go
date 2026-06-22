package video

import (
	"context"
	"errors"

	"github.com/mowshon/moviego/v2/clip"
)

// IterFrames renders v at its own output rate, calling fn for each frame. See
// IterFramesAt for the schedule policy.
func IterFrames(ctx context.Context, v VideoClip, fn func(clip.RenderedFrame) error) (int, error) {
	rate, ok := v.Rate()
	if !ok {
		return 0, clip.Wrap("iter frames", clip.ErrNoRate)
	}
	return IterFramesAt(ctx, v, rate, fn)
}

// IterFramesAt renders v on an index-driven schedule at the given output rate:
// frame i is emitted while FrameTime(i) < duration (floor frame count, a
// documented divergence from int(duration*fps)). It stops early at EOF and
// returns the number of frames delivered. Each frame's buffers are pooled and
// released after fn returns, so fn must not retain them past the call.
func IterFramesAt(ctx context.Context, v VideoClip, rate clip.Rate, fn func(clip.RenderedFrame) error) (int, error) {
	dur := v.Duration()
	if !clip.Finite(dur) {
		return 0, clip.Wrap("iter frames", clip.ErrNoDuration)
	}
	size := v.Size()
	pool := clip.NewFramePool()
	hasMask := v.HasMask()

	count := 0
	for i := 0; rate.FrameTime(i) < dur; i++ {
		if err := ctx.Err(); err != nil {
			return count, err
		}
		t := rate.FrameTime(i)
		rgb := pool.Get(size.W, size.H, clip.RGB24)
		var alpha *clip.Frame
		if hasMask {
			alpha = pool.Get(size.W, size.H, clip.Gray8)
		}
		hasAlpha, err := v.RenderInto(ctx, t, rgb, alpha)
		if errors.Is(err, clip.ErrEOF) {
			release(rgb, alpha)
			break
		}
		if err != nil {
			release(rgb, alpha)
			return count, err
		}
		rf := clip.RenderedFrame{RGB: rgb, Index: i}
		if hasAlpha {
			rf.Alpha = alpha
		}
		if err := fn(rf); err != nil {
			release(rgb, alpha)
			return count, err
		}
		release(rgb, alpha)
		count++
	}
	return count, nil
}

func release(rgb, alpha *clip.Frame) {
	rgb.Release()
	if alpha != nil {
		alpha.Release()
	}
}
