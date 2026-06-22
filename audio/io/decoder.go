package audioio

import (
	"context"
	"encoding/binary"
	"io"
	"strconv"
	"time"

	"github.com/mowshon/moviego/v2/clip"
	"github.com/mowshon/moviego/v2/ffmpeg"
)

// nbytes is the PCM sample width we decode to (s16le). 16-bit is enough for
// editing fidelity and halves the pipe traffic versus s32le.
const nbytes = 2

// pcmFullScale is 2^(8*nbytes-1): the divisor that normalizes a signed 16-bit
// sample to the [-1, 1) float range.
const pcmFullScale = 1 << (8*nbytes - 1)

// restartThreshold is the forward seek distance (in sample frames) beyond which
// the decoder restarts FFmpeg rather than skipping. MoviePy uses 1,000,000 for
// audio (far larger than the 100-frame video threshold) because PCM skips are
// cheap relative to a process restart.
const restartThreshold = 1_000_000

// DecoderOptions configures OpenDecoder. SampleRate and Channels are the decode
// target; FFmpeg resamples/remixes to them, so a render can normalize every
// source to one rate at the (explicit) decode boundary. Zero values fall back
// to the file's native sample rate / channel count from a probe.
type DecoderOptions struct {
	SampleRate int
	Channels   int
}

// Decoder reads interleaved float32 PCM from a media file in sample-frame order.
// It owns one FFmpeg subprocess and is not safe for concurrent use; an audio
// render gives each decoder to a single goroutine.
type Decoder struct {
	ctx        context.Context
	path       string
	sampleRate int
	channels   int
	frameBytes int // bytes per sample frame = nbytes * channels

	proc    *ffmpeg.Proc
	pos     int    // index of the next sample frame ReadInto will return
	scratch []byte // reusable skip/read buffer
	eof     bool
	closed  bool

	restarts int // test hook: number of FFmpeg restarts
}

// OpenDecoder starts decoding path from sample frame 0 at the requested (or
// native) sample rate and channel count.
func OpenDecoder(ctx context.Context, path string, opts DecoderOptions) (*Decoder, error) {
	sr, ch := opts.SampleRate, opts.Channels
	if sr <= 0 || ch <= 0 {
		info, err := ffmpeg.Probe(ctx, path, ffmpeg.ProbeOptions{})
		if err != nil {
			return nil, err
		}
		if info.Audio == nil {
			return nil, clip.Wrap("open audio "+path, clip.ErrVideoCorrupted)
		}
		if sr <= 0 {
			sr = info.Audio.SampleRate
		}
		if ch <= 0 {
			ch = info.Audio.Channels
		}
	}
	if sr <= 0 || ch <= 0 {
		return nil, clip.Wrap("open audio "+path, clip.ErrVideoCorrupted)
	}
	d := &Decoder{
		ctx:        ctx,
		path:       path,
		sampleRate: sr,
		channels:   ch,
		frameBytes: nbytes * ch,
	}
	if err := d.launch(0); err != nil {
		return nil, err
	}
	// Register for render-scoped reaping: a decoder buried in a transient mix
	// graph has no other owner to kill and reap its FFmpeg process at end of
	// render (see Reaper). Without a reaper in ctx this is a no-op.
	if r := reaperFrom(ctx); r != nil {
		r.add(d)
	}
	return d, nil
}

// SampleRate returns the decode sample rate.
func (d *Decoder) SampleRate() int { return d.sampleRate }

// Channels returns the decode channel count.
func (d *Decoder) Channels() int { return d.channels }

// ReadInto fills dst with count interleaved sample frames starting at sample
// index start. Indices before 0 and at/after the end of the stream are
// zero-filled (out-of-range reads return silence, matching MoviePy), so a
// caller that knows the timeline never needs to special-case EOF. dst must hold
// at least count*Channels float32 samples.
func (d *Decoder) ReadInto(start, count int, dst []float32) error {
	if d.closed {
		return clip.ErrClosed
	}
	if count <= 0 {
		return nil
	}
	need := count * d.channels
	if len(dst) < need {
		return clip.Wrap("decode "+d.path, clip.ErrVideoCorrupted)
	}
	for i := 0; i < need; i++ {
		dst[i] = 0
	}

	// A start before sample 0 leaves leading silence, then reads from 0.
	dstFrame := 0
	if start < 0 {
		lead := -start
		if lead >= count {
			return nil // the whole window is before the stream start
		}
		dstFrame = lead
		count -= lead
		start = 0
	}

	if err := d.seek(start); err != nil {
		return err
	}
	return d.readFrames(count, dst[dstFrame*d.channels:])
}

// Close stops the subprocess and releases resources. It is idempotent.
func (d *Decoder) Close() error {
	if d.closed {
		return nil
	}
	d.closed = true
	d.stopProc()
	return nil
}

// seek positions the decoder so the next read returns sample frame idx, using
// the audio state table: a no-op at the current position, a restart for a
// backward or far-forward jump, and a skip for a nearby forward move.
func (d *Decoder) seek(idx int) error {
	switch {
	case idx == d.pos:
		return nil
	case idx < d.pos || idx > d.pos+restartThreshold:
		return d.restart(idx)
	default:
		return d.skip(idx - d.pos)
	}
}

// readFrames reads up to n sample frames into dst, stopping early (leaving the
// remainder as the zero already written) at end of stream.
func (d *Decoder) readFrames(n int, dst []float32) error {
	if d.proc == nil {
		if d.eof {
			return nil // past the end: silence already written
		}
		if err := d.launch(d.pos); err != nil {
			return err
		}
	}
	bytesNeeded := n * d.frameBytes
	if cap(d.scratch) < bytesNeeded {
		d.scratch = make([]byte, bytesNeeded)
	}
	buf := d.scratch[:bytesNeeded]
	got, err := d.readRaw(buf)
	frames := got / d.frameBytes
	d.pos += frames
	decodePCM(buf[:frames*d.frameBytes], dst)
	if err != nil {
		return err
	}
	return nil
}

// readRaw reads as much of buf as the stream provides, returning the byte count
// actually read. A clean or truncated end of stream is reported as eof (not an
// error); a context cancellation or a real failure is returned.
func (d *Decoder) readRaw(buf []byte) (int, error) {
	n, err := io.ReadFull(d.proc.Stdout, buf)
	switch {
	case err == nil:
		return n, nil
	case d.ctx.Err() != nil:
		d.stopProc()
		return n, clip.Wrap("decode "+d.path, d.ctx.Err())
	case err == io.EOF || err == io.ErrUnexpectedEOF:
		// End of stream (possibly mid-frame): keep the whole frames we have and
		// treat the rest as silence. A partial frame's trailing bytes are
		// dropped by the caller's frames = got/frameBytes truncation.
		d.eof = true
		d.stopProc()
		return n, nil
	default:
		d.stopProc()
		return n, clip.Wrap("decode "+d.path, err)
	}
}

func (d *Decoder) skip(n int) error {
	if d.proc == nil {
		if d.eof {
			d.pos += n
			return nil
		}
		if err := d.launch(d.pos); err != nil {
			return err
		}
	}
	bytesNeeded := n * d.frameBytes
	if cap(d.scratch) < bytesNeeded {
		d.scratch = make([]byte, bytesNeeded)
	}
	got, err := d.readRaw(d.scratch[:bytesNeeded])
	d.pos += got / d.frameBytes
	return err
}

func (d *Decoder) restart(idx int) error {
	d.stopProc()
	d.restarts++
	return d.launch(idx)
}

// launch starts FFmpeg decoding from sample frame idx and sets pos to idx.
func (d *Decoder) launch(idx int) error {
	bin, err := ffmpeg.FFmpegPath()
	if err != nil {
		return err
	}
	proc, err := ffmpeg.Start(d.ctx, ffmpeg.Spec{
		Path:   bin,
		Args:   d.decodeArgs(idx),
		Stdout: true,
	})
	if err != nil {
		return err
	}
	d.proc = proc
	d.pos = idx
	d.eof = false
	return nil
}

// decodeArgs builds the decode command for sample frame idx: a two-pass -ss
// (coarse before -i, precise after) then s16le PCM at the target rate/channels.
func (d *Decoder) decodeArgs(idx int) []string {
	var pre, post []string
	// clip.SampleTime is the canonical (overflow-safe) timestamp of sample idx;
	// the ~1ns floor/ceil difference is immaterial to the µs-resolution -ss seek.
	start := time.Duration(clip.SampleTime(idx, d.sampleRate))
	if start > 0 {
		offset := start
		if offset > time.Second {
			offset = time.Second
		}
		pre = []string{"-ss", secs(start - offset)}
		post = []string{"-ss", secs(offset)}
	}
	args := []string{"-nostdin", "-loglevel", "error"}
	args = append(args, pre...)
	args = append(args, "-i", ffmpeg.SafeInputPath(d.path))
	args = append(args, post...)
	args = append(args,
		"-vn",
		"-f", "s16le",
		"-acodec", "pcm_s16le",
		"-ar", strconv.Itoa(d.sampleRate),
		"-ac", strconv.Itoa(d.channels),
		"-",
	)
	return args
}

// stopProc kills and reaps the current subprocess, if any.
func (d *Decoder) stopProc() {
	if d.proc == nil {
		return
	}
	_ = d.proc.Kill()
	_ = d.proc.Wait()
	d.proc = nil
}

// decodePCM converts little-endian s16 bytes into normalized float32 samples.
func decodePCM(src []byte, dst []float32) {
	for i := 0; i+1 < len(src); i += 2 {
		s := int16(binary.LittleEndian.Uint16(src[i : i+2]))
		dst[i/2] = float32(s) / pcmFullScale
	}
}

func secs(d time.Duration) string {
	return strconv.FormatFloat(d.Seconds(), 'f', 6, 64)
}
