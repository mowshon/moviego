package video

import (
	"context"
	"errors"
	"sync"

	"github.com/mowshon/moviego/v2/audio"
	"github.com/mowshon/moviego/v2/clip"
	"github.com/mowshon/moviego/v2/ffmpeg"
	videoio "github.com/mowshon/moviego/v2/video/io"
)

// VideoFileNode is a file-backed video source. Its frames are produced by a
// single sequential decoder behind a mutex (AccessLinear, not parallel-safe);
// downstream stages fan the frames out. Size is post-rotation.
type VideoFileNode struct {
	path     string
	size     clip.Size
	rate     clip.Rate
	dur      clip.Time
	start    clip.Time
	rotation int // normalized display rotation (0/90/180/270); decode applies it

	audioClip audio.AudioClip // the file's own audio track, nil when it has none

	r *reader
}

// reader holds the live decode state for one node. It is a pointer so placement
// variants (WithStart/WithDuration/WithEnd) each get an independent decoder
// instead of sharing one mutable owner.
type reader struct {
	mu        sync.Mutex
	dec       *videoio.Decoder
	decCtx    context.Context
	lastIdx   int
	lastFrame *clip.Frame
	closed    bool
}

func newReader() *reader { return &reader{lastIdx: -1} }

// OpenFile probes path and returns a file source. The decoder itself is created
// lazily on the first frame access, bound to that call's context.
func OpenFile(ctx context.Context, path string) (*VideoFileNode, error) {
	info, err := ffmpeg.Probe(ctx, path, ffmpeg.ProbeOptions{})
	if err != nil {
		return nil, err
	}
	if info.Video == nil {
		return nil, clip.Wrap("open "+path, clip.ErrVideoCorrupted)
	}
	var au audio.AudioClip
	if info.Audio != nil && info.Audio.SampleRate > 0 && info.Audio.Channels > 0 {
		au = audio.NewFileNode(path, info.Audio.SampleRate, info.Audio.Channels, info.Duration)
	}
	return &VideoFileNode{
		path:      path,
		size:      info.Video.Size,
		rate:      info.Video.Rate,
		dur:       info.Duration,
		rotation:  info.Video.Rotation,
		audioClip: au,
		r:         newReader(),
	}, nil
}

// Timeline metadata.

func (n *VideoFileNode) Start() clip.Time { return n.start }

func (n *VideoFileNode) Duration() clip.Time { return clip.DurationOr(n.dur, n.dur > 0) }

func (n *VideoFileNode) End() clip.Time { return clip.EndOr(n.start, n.dur, n.dur > 0) }

func (n *VideoFileNode) WithStart(t clip.Time) clip.Clip {
	c := n.placementCopy()
	c.start = t
	if c.audioClip != nil {
		c.audioClip = c.audioClip.WithStart(t).(audio.AudioClip)
	}
	return c
}

func (n *VideoFileNode) WithDuration(d clip.Time) clip.Clip {
	c := n.placementCopy()
	c.dur = d
	if c.audioClip != nil {
		c.audioClip = c.audioClip.WithDuration(d).(audio.AudioClip)
	}
	return c
}

func (n *VideoFileNode) WithEnd(end clip.Time, changeDuration bool) clip.Clip {
	c := n.placementCopy()
	if changeDuration {
		c.dur = end - c.start
	} else if c.dur > 0 {
		c.start = end - c.dur
	}
	if c.audioClip != nil {
		c.audioClip = c.audioClip.WithEnd(end, changeDuration).(audio.AudioClip)
	}
	return c
}

// placementCopy returns a variant with independent decode state; the source
// file metadata is shared by value.
func (n *VideoFileNode) placementCopy() *VideoFileNode {
	return &VideoFileNode{
		path:      n.path,
		size:      n.size,
		rate:      n.rate,
		dur:       n.dur,
		start:     n.start,
		rotation:  n.rotation,
		audioClip: n.audioClip,
		r:         newReader(),
	}
}

// Video metadata.

func (n *VideoFileNode) Size() clip.Size           { return n.size }
func (n *VideoFileNode) Rate() (clip.Rate, bool)   { return n.rate, n.rate.Num != 0 }
func (n *VideoFileNode) HasMask() bool             { return false }
func (n *VideoFileNode) Audio() audio.AudioClip    { return n.audioClip }
func (n *VideoFileNode) ParallelSafe() bool        { return false }
func (n *VideoFileNode) SourceAccess() AccessClass { return AccessLinear }

// FileSource accessors let the render planner set up one sequential decoder per
// distinct source. The reader pointer is the source identity: it is unique per
// node instance (placement copies get their own), so the node reached during
// RenderInto reads from the decoder the planner created for it.

func (n *VideoFileNode) SourceKey() any        { return n.r }
func (n *VideoFileNode) SourcePath() string    { return n.path }
func (n *VideoFileNode) SourceSize() clip.Size { return n.size }
func (n *VideoFileNode) SourceRate() clip.Rate { return n.rate }

// FilterInput advertises this file as a plain "-i path" input for filtergraph
// fusion. Trimming and pixel work are advertised by the wrapper nodes above the
// source, so the input itself is unconditional — except for a rotated source.
//
// The Go decode path applies the container's display-matrix rotation
// automatically (FFmpeg autorotation), and Size() is post-rotation. A fused
// graph instead wires the raw "[0:v]" stream into -filter_complex, where FFmpeg
// does NOT auto-insert that rotation, so the fused frames would have the wrong
// orientation and dimensions versus the planner's post-rotation Size. Until the
// fused path inserts an explicit transpose and is parity-gated, a rotated source
// declines fusion and falls back to the Go engine (which rotates correctly).
func (n *VideoFileNode) FilterInput(ffmpeg.FilterContext) (ffmpeg.Input, bool) {
	if n.rotation != 0 {
		return ffmpeg.Input{}, false
	}
	return ffmpeg.Input{Name: n.path}, true
}

// Rendering.

func (n *VideoFileNode) RenderInto(ctx context.Context, t clip.Time, rgbDst, _ *clip.Frame) (bool, error) {
	return false, n.frameInto(ctx, t, rgbDst)
}

func (n *VideoFileNode) FrameInto(ctx context.Context, t clip.Time, dst *clip.Frame) error {
	return n.frameInto(ctx, t, dst)
}

func (n *VideoFileNode) MaskInto(context.Context, clip.Time, *clip.Frame) (bool, error) {
	return false, nil
}

// frameInto reads the source frame for output time t into dst, applying the
// idx==lastRead cache so a repeated index never re-reads the decoder.
func (n *VideoFileNode) frameInto(ctx context.Context, t clip.Time, dst *clip.Frame) error {
	// Honor cancellation before any work, including a cache hit: a canceled or
	// deadline-exceeded export context must never succeed, even when the
	// requested frame happens to be the one already buffered.
	if err := ctx.Err(); err != nil {
		return err
	}
	idx := n.rate.TimeToFrame(t)
	if idx < 0 {
		idx = 0
	}
	// Pipelined render: read from the shared sequential decoder the planner set
	// up for this source instead of seeking our own. The provider is keyed by
	// the same reader pointer the planner enumerated (SourceKey).
	if p := frameProviderFrom(ctx); p != nil {
		return p.SourceFrameInto(ctx, n.r, idx, dst)
	}
	r := n.r
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return clip.ErrClosed
	}
	if idx == r.lastIdx && r.lastFrame != nil {
		copyPixels(dst, r.lastFrame)
		return nil
	}
	// Bind the decoder to the caller's context, reopening if it changed since
	// the last access. The decoder's FFmpeg process is killed when its context
	// is canceled, so a decoder opened under an interactive context must not be
	// reused for a cancelable export—otherwise cancellation could not stop it.
	if r.dec == nil || r.decCtx != ctx {
		if r.dec != nil {
			_ = r.dec.Close()
		}
		dec, err := videoio.OpenDecoder(ctx, n.path, videoio.DecoderOptions{Size: n.size, Rate: n.rate})
		if err != nil {
			return err
		}
		r.dec, r.decCtx = dec, ctx
	}
	if err := r.dec.SeekToFrame(idx); err != nil {
		return err
	}
	if err := r.dec.ReadInto(dst); err != nil {
		return err
	}
	if r.lastFrame == nil {
		r.lastFrame = clip.NewFrame(n.size.W, n.size.H, clip.RGB24)
	}
	copyPixels(r.lastFrame, dst)
	r.lastIdx = idx
	return nil
}

// Close stops the video decoder and the audio sidecar this node created. A file
// with an audio track owns the AudioFileNode built in OpenFile, so it must close
// it here; otherwise the audio
// decoder's FFmpeg process, lazily opened during an export that never reaches
// EOF, would leak until the context is canceled or the process exits. It is
// idempotent: the audio sidecar's own Close is idempotent too.
func (n *VideoFileNode) Close() error {
	var audioErr error
	if n.audioClip != nil {
		audioErr = n.audioClip.Close()
	}
	r := n.r
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return audioErr
	}
	r.closed = true
	if r.dec != nil {
		err := r.dec.Close()
		r.dec = nil
		return errors.Join(err, audioErr)
	}
	return audioErr
}
