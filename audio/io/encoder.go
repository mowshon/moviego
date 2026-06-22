package audioio

import (
	"context"
	"encoding/binary"
	"strconv"

	"github.com/mowshon/moviego/v2/clip"
	"github.com/mowshon/moviego/v2/ffmpeg"
)

// quantClamp bounds a normalized sample before scaling, matching MoviePy's
// [-0.99, 0.99] guard so the int conversion never wraps at full scale.
const quantClamp = 0.99

// EncoderOptions configures OpenEncoder. SampleRate and Channels are required.
// Codec selects the output audio codec (e.g. aac, libopus, pcm_s16le); an empty
// Codec lets FFmpeg pick from the output container.
type EncoderOptions struct {
	SampleRate int
	Channels   int
	Codec      string
	Bitrate    string // e.g. "192k"; empty leaves the codec default
}

// Encoder feeds interleaved float32 PCM to FFmpeg over stdin (as quantized
// s16le) and writes an encoded audio file. It is not safe for concurrent use.
type Encoder struct {
	ctx      context.Context
	proc     *ffmpeg.Proc
	channels int
	out      string
	scratch  []byte
	closed   bool
}

// OpenEncoder starts FFmpeg to encode PCM written via WriteFrames into out.
// Call Close to flush and finalize the file.
func OpenEncoder(ctx context.Context, out string, opts EncoderOptions) (*Encoder, error) {
	if opts.SampleRate <= 0 || opts.Channels <= 0 {
		return nil, clip.Wrap("encode audio "+out, clip.ErrNoRate)
	}
	bin, err := ffmpeg.FFmpegPath()
	if err != nil {
		return nil, err
	}
	args := []string{
		"-nostdin", "-loglevel", "error", "-y",
		"-f", "s16le",
		"-ar", strconv.Itoa(opts.SampleRate),
		"-ac", strconv.Itoa(opts.Channels),
		"-i", "-",
	}
	if opts.Codec != "" {
		args = append(args, "-acodec", opts.Codec)
	}
	if opts.Bitrate != "" {
		args = append(args, "-b:a", opts.Bitrate)
	}
	args = append(args, out)

	proc, err := ffmpeg.Start(ctx, ffmpeg.Spec{Path: bin, Args: args, Stdin: true})
	if err != nil {
		return nil, err
	}
	return &Encoder{ctx: ctx, proc: proc, channels: opts.Channels, out: out}, nil
}

// WriteFrames quantizes and writes one buffer of interleaved float32 samples.
// The buffer's channel count must match the encoder's.
func (e *Encoder) WriteFrames(buf *clip.AudioBuffer) error {
	if e.closed {
		return clip.ErrClosed
	}
	if buf.Channels != e.channels {
		return clip.Wrap("encode audio "+e.out, clip.ErrVideoCorrupted)
	}
	n := buf.Count * buf.Channels
	if n == 0 {
		return nil
	}
	if cap(e.scratch) < n*nbytes {
		e.scratch = make([]byte, n*nbytes)
	}
	pcm := e.scratch[:n*nbytes]
	quantizePCM(buf.Samples[:n], pcm)
	if _, err := e.proc.Stdin.Write(pcm); err != nil {
		if e.ctx.Err() != nil {
			return clip.Wrap("encode audio "+e.out, e.ctx.Err())
		}
		return clip.Wrap("encode audio "+e.out, err)
	}
	return nil
}

// Close finishes the input stream, waits for FFmpeg, and surfaces any non-zero
// exit with the stderr tail. It is idempotent.
func (e *Encoder) Close() error {
	if e.closed {
		return nil
	}
	e.closed = true
	_ = e.proc.Stdin.Close()
	if err := e.proc.Wait(); err != nil {
		return clip.Wrap("encode audio "+e.out, err)
	}
	return nil
}

// quantizePCM converts normalized float32 samples to little-endian s16, clamping
// to [-0.99, 0.99] and rounding to nearest before scaling by full scale.
func quantizePCM(src []float32, dst []byte) {
	for i, s := range src {
		if s > quantClamp {
			s = quantClamp
		} else if s < -quantClamp {
			s = -quantClamp
		}
		v := s * pcmFullScale
		if v >= 0 {
			v += 0.5
		} else {
			v -= 0.5
		}
		binary.LittleEndian.PutUint16(dst[i*2:i*2+2], uint16(int16(v)))
	}
}
