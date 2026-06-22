package audio

import (
	"context"
	"sync"

	audioio "github.com/mowshon/moviego/v2/audio/io"
	"github.com/mowshon/moviego/v2/clip"
	"github.com/mowshon/moviego/v2/ffmpeg"
)

// AudioFileNode is a file-backed audio source. Samples are produced by a single
// sequential decoder behind a mutex; the chunked audio render pulls forward
// ranges, which decode without seeking. Placement variants (WithStart/…) each
// get an independent decoder.
type AudioFileNode struct {
	path       string
	sampleRate int
	channels   int
	dur        clip.Time
	start      clip.Time

	r *audioReader
}

// audioReader holds one node's live decode state behind a mutex.
type audioReader struct {
	mu     sync.Mutex
	dec    *audioio.Decoder
	decCtx context.Context
	closed bool
}

// OpenFile probes path and returns an audio source. The decoder is created
// lazily on the first SamplesInto call, bound to that call's context.
func OpenFile(ctx context.Context, path string) (*AudioFileNode, error) {
	info, err := ffmpeg.Probe(ctx, path, ffmpeg.ProbeOptions{})
	if err != nil {
		return nil, err
	}
	if info.Audio == nil || info.Audio.SampleRate <= 0 || info.Audio.Channels <= 0 {
		return nil, clip.Wrap("open audio "+path, clip.ErrVideoCorrupted)
	}
	return &AudioFileNode{
		path:       path,
		sampleRate: info.Audio.SampleRate,
		channels:   info.Audio.Channels,
		dur:        info.Duration,
		r:          &audioReader{},
	}, nil
}

// NewFileNode builds a file source from already-probed metadata, avoiding a
// second probe. It is used by VideoFileNode to expose a file's own audio track.
func NewFileNode(path string, sampleRate, channels int, dur clip.Time) *AudioFileNode {
	return &AudioFileNode{
		path:       path,
		sampleRate: sampleRate,
		channels:   channels,
		dur:        dur,
		r:          &audioReader{},
	}
}

// Timeline metadata.

func (n *AudioFileNode) Start() clip.Time { return n.start }

func (n *AudioFileNode) Duration() clip.Time { return clip.DurationOr(n.dur, n.dur > 0) }

func (n *AudioFileNode) End() clip.Time { return clip.EndOr(n.start, n.dur, n.dur > 0) }

func (n *AudioFileNode) WithStart(t clip.Time) clip.Clip {
	c := n.placementCopy()
	c.start = t
	return c
}

func (n *AudioFileNode) WithDuration(d clip.Time) clip.Clip {
	c := n.placementCopy()
	c.dur = d
	return c
}

func (n *AudioFileNode) WithEnd(end clip.Time, changeDuration bool) clip.Clip {
	c := n.placementCopy()
	if changeDuration {
		c.dur = end - c.start
	} else if c.dur > 0 {
		c.start = end - c.dur
	}
	return c
}

func (n *AudioFileNode) placementCopy() *AudioFileNode {
	return &AudioFileNode{
		path:       n.path,
		sampleRate: n.sampleRate,
		channels:   n.channels,
		dur:        n.dur,
		start:      n.start,
		r:          &audioReader{},
	}
}

// Audio metadata.

func (n *AudioFileNode) SampleRate() int { return n.sampleRate }
func (n *AudioFileNode) Channels() int   { return n.channels }

// SamplesInto fills dst with count sample frames starting at composition time
// start. The window is mapped to a sample index (floor), and the decoder
// zero-pads any range before 0 or past the end of the stream.
func (n *AudioFileNode) SamplesInto(ctx context.Context, start clip.Time, count int, dst *clip.AudioBuffer) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	dst.Reset(count, n.channels)
	idx := clip.SampleIndex(start, n.sampleRate)

	r := n.r
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return clip.ErrClosed
	}
	// Bind the decoder to the caller's context, reopening if it changed so a
	// cancelable render can always kill the FFmpeg process (see VideoFileNode).
	if r.dec == nil || r.decCtx != ctx {
		if r.dec != nil {
			_ = r.dec.Close()
		}
		dec, err := audioio.OpenDecoder(ctx, n.path, audioio.DecoderOptions{
			SampleRate: n.sampleRate,
			Channels:   n.channels,
		})
		if err != nil {
			return err
		}
		r.dec, r.decCtx = dec, ctx
	}
	return r.dec.ReadInto(idx, count, dst.Samples)
}

// Close stops the decoder. It is idempotent.
func (n *AudioFileNode) Close() error {
	r := n.r
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil
	}
	r.closed = true
	if r.dec != nil {
		err := r.dec.Close()
		r.dec = nil
		return err
	}
	return nil
}
