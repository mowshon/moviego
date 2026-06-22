package videoio

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/mowshon/moviego/v2/clip"
	"github.com/mowshon/moviego/v2/ffmpeg"
)

// restartThreshold is the forward seek distance (in frames) beyond which the
// decoder restarts FFmpeg rather than skipping frames. MoviePy uses 100.
const restartThreshold = 100

// seekEpsilon backs off the seek target by MoviePy's 1e-5s frame-index bias so a
// precise -ss lands on the intended frame rather than the next one.
const seekEpsilon = 10 * time.Microsecond

// DecoderOptions configures OpenDecoder. Size and Rate are normally supplied
// from a prior Probe so the decoder does not probe a second time; when Size is
// zero the decoder probes the file itself.
type DecoderOptions struct {
	Size clip.Size // decoded frame dimensions, display orientation (post-rotation)
	Rate clip.Rate // frame rate used to compute seek timestamps
}

// Decoder reads packed rgb24 frames from a media file in index order. It owns a
// single FFmpeg subprocess and is not safe for concurrent use; a render gives
// each decoder to exactly one goroutine.
type Decoder struct {
	ctx        context.Context
	path       string
	size       clip.Size
	rate       clip.Rate
	frameBytes int

	proc    *ffmpeg.Proc
	pos     int    // index of the next frame ReadInto will return
	scratch []byte // reusable skip buffer
	eof     bool
	closed  bool

	restarts int // test hook: number of FFmpeg restarts
}

// OpenDecoder starts decoding path from frame 0. It probes the file for the
// frame size when opts.Size is zero.
func OpenDecoder(ctx context.Context, path string, opts DecoderOptions) (*Decoder, error) {
	size, rate := opts.Size, opts.Rate
	if size.W == 0 || size.H == 0 {
		info, err := ffmpeg.Probe(ctx, path, ffmpeg.ProbeOptions{})
		if err != nil {
			return nil, err
		}
		if info.Video == nil {
			return nil, clip.Wrap("open decoder "+path, clip.ErrVideoCorrupted)
		}
		size = info.Video.Size
		rate = info.Video.Rate
	}
	d := &Decoder{
		ctx:        ctx,
		path:       path,
		size:       size,
		rate:       rate,
		frameBytes: size.W * size.H * clip.RGB24.BytesPerPixel(),
	}
	if err := d.launch(0); err != nil {
		return nil, err
	}
	return d, nil
}

// Size returns the decoded frame size.
func (d *Decoder) Size() clip.Size { return d.size }

// Rate returns the frame rate used for seeking.
func (d *Decoder) Rate() clip.Rate { return d.rate }

// Pos returns the index of the next frame ReadInto will return.
func (d *Decoder) Pos() int { return d.pos }

// ReadInto fills dst with the frame at the current position and advances. It
// returns ErrEOF at end of stream.
func (d *Decoder) ReadInto(dst *clip.Frame) error {
	if d.closed {
		return clip.ErrClosed
	}
	if err := d.validateDst(dst); err != nil {
		return err
	}
	if d.proc == nil {
		if d.eof {
			return clip.ErrEOF
		}
		if err := d.launch(d.pos); err != nil {
			return err
		}
	}
	if err := d.readRaw(dst.Pix[:d.frameBytes]); err != nil {
		return err
	}
	d.pos++
	return nil
}

// SeekToFrame positions the decoder so the next ReadInto returns frame idx,
// following the documented state table: a seek to the current position is a
// no-op, a backward or far-forward seek restarts FFmpeg, and a nearby forward
// seek skips the intervening frames into scratch.
func (d *Decoder) SeekToFrame(idx int) error {
	if d.closed {
		return clip.ErrClosed
	}
	if idx < 0 {
		idx = 0
	}
	switch {
	case idx == d.pos:
		return nil
	case idx < d.pos || idx > d.pos+restartThreshold:
		return d.restart(idx)
	default:
		return d.skip(idx - d.pos)
	}
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

func (d *Decoder) skip(n int) error {
	if d.proc == nil {
		if err := d.launch(d.pos); err != nil {
			return err
		}
	}
	if d.scratch == nil {
		d.scratch = make([]byte, d.frameBytes)
	}
	for i := 0; i < n; i++ {
		if err := d.readRaw(d.scratch); err != nil {
			return err
		}
		d.pos++
	}
	return nil
}

func (d *Decoder) restart(idx int) error {
	d.stopProc()
	d.restarts++
	return d.launch(idx)
}

// launch starts FFmpeg decoding from frame idx and sets pos to idx.
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

// decodeArgs builds the two-pass -ss decode command for frame idx: a coarse
// -ss before -i and a precise -ss after, with offset = min(1s, start).
func (d *Decoder) decodeArgs(idx int) []string {
	var pre, post []string
	start := d.rate.FrameTime(idx) - seekEpsilon
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
	args = append(args, "-an", "-f", "rawvideo", "-pix_fmt", "rgb24", "-")
	return args
}

func (d *Decoder) readRaw(buf []byte) error {
	_, err := io.ReadFull(d.proc.Stdout, buf)
	if err == nil {
		return nil
	}
	// Clear the dead process on every error path so a retry relaunches cleanly
	// rather than reading a closed pipe.
	if d.ctx.Err() != nil {
		d.stopProc()
		return clip.Wrap("decode "+d.path, d.ctx.Err())
	}
	if err == io.EOF {
		// A whole-frame boundary with no more data is a clean end of stream.
		d.eof = true
		d.stopProc()
		return clip.ErrEOF
	}
	if err == io.ErrUnexpectedEOF {
		// A partial frame means the stream was truncated or FFmpeg failed, not a
		// clean end—surface it as corruption with the stderr tail.
		d.eof = true
		waitErr := d.reap()
		return clip.Wrap("decode "+d.path, corrupted(waitErr))
	}
	d.stopProc()
	return clip.Wrap("decode "+d.path, err)
}

// corrupted wraps the corruption sentinel with an optional FFmpeg exit detail.
func corrupted(detail error) error {
	if detail == nil {
		return clip.ErrVideoCorrupted
	}
	return fmt.Errorf("%w: %v", clip.ErrVideoCorrupted, detail)
}

func (d *Decoder) validateDst(dst *clip.Frame) error {
	if dst.Format != clip.RGB24 || dst.W != d.size.W || dst.H != d.size.H {
		return clip.Wrap("decode "+d.path, clip.ErrVideoCorrupted)
	}
	if len(dst.Pix) < d.frameBytes {
		return clip.Wrap("decode "+d.path, clip.ErrVideoCorrupted)
	}
	return nil
}

// stopProc kills and reaps the current subprocess, if any.
func (d *Decoder) stopProc() { _ = d.reap() }

// reap kills and waits for the current subprocess, clears it, and returns its
// exit error (the stderr tail rides along on the *ffmpeg.ExitError).
func (d *Decoder) reap() error {
	if d.proc == nil {
		return nil
	}
	_ = d.proc.Kill()
	err := d.proc.Wait()
	d.proc = nil
	return err
}

// secs formats a duration as fractional seconds for an FFmpeg -ss argument.
func secs(d time.Duration) string {
	return strconv.FormatFloat(d.Seconds(), 'f', 6, 64)
}
