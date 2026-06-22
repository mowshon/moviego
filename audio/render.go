package audio

import (
	"context"
	"errors"

	audioio "github.com/mowshon/moviego/v2/audio/io"
	"github.com/mowshon/moviego/v2/clip"
)

// renderChunkFrames is the sample-frame chunk size pulled per SamplesInto call.
// 65536 frames (~1.5s at 44.1 kHz) amortizes the per-call overhead while keeping
// the working buffer small.
const renderChunkFrames = 1 << 16

// RenderToTemp renders root over [0, dur) and encodes it to out with codec. The
// output timeline (dur, from the caller — normally the video duration) is
// authoritative: the audio is trimmed or zero-padded to exactly dur so it stays
// in sync with the video. A non-positive dur or a root with no channels is an
// error.
//
// The render samples root from time 0, so root.Start() (composition placement)
// is NOT applied here — a sidecar attached with a non-zero Start plays from 0,
// not from its placement offset. This matches MoviePy's write_audiofile, where
// placement only matters inside a composite; to honor a start offset, wrap the
// clip in a Mix (which gates each child to its [start, end) window) before
// rendering.
func RenderToTemp(ctx context.Context, root AudioClip, out, codec, bitrate string, dur clip.Time) error {
	sr, ch := root.SampleRate(), root.Channels()
	if sr <= 0 || ch <= 0 {
		return clip.Wrap("render audio", clip.ErrNoRate)
	}
	if dur <= 0 {
		return clip.Wrap("render audio", clip.ErrNoDuration)
	}
	total := clip.SampleIndex(dur, sr)
	if total <= 0 {
		return clip.Wrap("render audio", clip.ErrNoDuration)
	}

	enc, err := audioio.OpenEncoder(ctx, out, audioio.EncoderOptions{
		SampleRate: sr,
		Channels:   ch,
		Codec:      codec,
		Bitrate:    bitrate,
	})
	if err != nil {
		return err
	}

	buf := &clip.AudioBuffer{}
	var runErr error
	for off := 0; off < total; off += renderChunkFrames {
		if err := ctx.Err(); err != nil {
			runErr = err
			break
		}
		n := renderChunkFrames
		if rem := total - off; rem < n {
			n = rem
		}
		start := clip.SampleTime(off, sr)
		if err := root.SamplesInto(ctx, start, n, buf); err != nil {
			runErr = err
			break
		}
		if err := enc.WriteFrames(buf); err != nil {
			runErr = err
			break
		}
	}

	// Join both errors: when a write fails on a broken pipe, Close carries the
	// FFmpeg stderr tail that explains why (mirroring the video export).
	closeErr := enc.Close()
	return errors.Join(runErr, closeErr)
}
