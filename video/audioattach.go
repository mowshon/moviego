package video

import (
	"context"

	"github.com/mowshon/moviego/v2/audio"
	"github.com/mowshon/moviego/v2/clip"
)

// audioAttachNode wraps a video clip and replaces its audio sidecar. Every video
// operation delegates to the inner clip; only Audio() is overridden. Timeline
// With* changes propagate to both the video and the attached audio so placement
// stays in sync.
type audioAttachNode struct {
	inner VideoClip
	audio audio.AudioClip
}

// WithAudio returns a video clip identical to v but carrying a as its audio
// sidecar, replacing any audio v already had. Passing a nil audio removes it.
func WithAudio(v VideoClip, a audio.AudioClip) VideoClip {
	return &audioAttachNode{inner: v, audio: a}
}

// WithoutAudio returns a video clip identical to v with no audio sidecar.
func WithoutAudio(v VideoClip) VideoClip {
	return &audioAttachNode{inner: v, audio: nil}
}

// Timeline metadata delegates to the inner clip, re-wrapping With* results and
// mirroring the timeline change onto the attached audio.

func (n *audioAttachNode) Start() clip.Time    { return n.inner.Start() }
func (n *audioAttachNode) Duration() clip.Time { return n.inner.Duration() }
func (n *audioAttachNode) End() clip.Time      { return n.inner.End() }

func (n *audioAttachNode) WithStart(t clip.Time) clip.Clip {
	return &audioAttachNode{inner: n.inner.WithStart(t).(VideoClip), audio: withAudioStart(n.audio, t)}
}

func (n *audioAttachNode) WithDuration(d clip.Time) clip.Clip {
	return &audioAttachNode{inner: n.inner.WithDuration(d).(VideoClip), audio: withAudioDuration(n.audio, d)}
}

func (n *audioAttachNode) WithEnd(end clip.Time, changeDuration bool) clip.Clip {
	a := n.audio
	if a != nil {
		a = a.WithEnd(end, changeDuration).(audio.AudioClip)
	}
	return &audioAttachNode{inner: n.inner.WithEnd(end, changeDuration).(VideoClip), audio: a}
}

// Video metadata.

func (n *audioAttachNode) Size() clip.Size           { return n.inner.Size() }
func (n *audioAttachNode) Rate() (clip.Rate, bool)   { return n.inner.Rate() }
func (n *audioAttachNode) HasMask() bool             { return n.inner.HasMask() }
func (n *audioAttachNode) Audio() audio.AudioClip    { return n.audio }
func (n *audioAttachNode) ParallelSafe() bool        { return n.inner.ParallelSafe() }
func (n *audioAttachNode) SourceAccess() AccessClass { return n.inner.SourceAccess() }

// Children exposes the wrapped video clip for planner graph traversal. The audio
// sidecar is not a video child.
func (n *audioAttachNode) Children() []VideoClip { return []VideoClip{n.inner} }

// Rendering delegates entirely to the inner clip.

func (n *audioAttachNode) RenderInto(ctx context.Context, t clip.Time, rgbDst, alphaDst *clip.Frame) (bool, error) {
	return n.inner.RenderInto(ctx, t, rgbDst, alphaDst)
}

func (n *audioAttachNode) FrameInto(ctx context.Context, t clip.Time, dst *clip.Frame) error {
	return n.inner.FrameInto(ctx, t, dst)
}

func (n *audioAttachNode) MaskInto(ctx context.Context, t clip.Time, dst *clip.Frame) (bool, error) {
	return n.inner.MaskInto(ctx, t, dst)
}

// Close releases nothing: the wrapper owns neither the inner video nor the audio.
func (n *audioAttachNode) Close() error { return nil }

func withAudioStart(a audio.AudioClip, t clip.Time) audio.AudioClip {
	if a == nil {
		return nil
	}
	return a.WithStart(t).(audio.AudioClip)
}

func withAudioDuration(a audio.AudioClip, d clip.Time) audio.AudioClip {
	if a == nil {
		return nil
	}
	return a.WithDuration(d).(audio.AudioClip)
}
