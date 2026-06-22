package mgo

import (
	"context"
	"fmt"
	"image"
	"os"
	"path/filepath"

	"github.com/mowshon/moviego/v2/clip"
	"github.com/mowshon/moviego/v2/imagex"
	"github.com/mowshon/moviego/v2/video"
)

// ExtractFrame decodes a single frame of the file at path at time t and returns
// it as an image.Image. It opens the file, reads the one frame, and closes it,
// so it is the cheap way to grab a still (thumbnail, poster) without building a
// clip graph. Canceling ctx aborts the decode.
func ExtractFrame(ctx context.Context, path string, t Time) (image.Image, error) {
	n, err := video.OpenFile(ctx, path)
	if err != nil {
		return nil, err
	}
	defer n.Close()
	size := n.Size()
	dst := clip.NewFrame(size.W, size.H, clip.RGB24)
	if err := n.FrameInto(ctx, t, dst); err != nil {
		return nil, err
	}
	return imagex.ToImage(dst), nil
}

// ImageFormat selects the encoding for a written frame sequence.
type ImageFormat int

const (
	// FormatPNG writes lossless PNG (keeps alpha for transparent clips).
	FormatPNG ImageFormat = iota
	// FormatJPEG writes JPEG at FrameSequenceOptions.Quality (alpha discarded).
	FormatJPEG
)

// FrameSequenceOptions configures WriteFrameSequence. The zero value writes PNGs
// named "frame_00000.png" at the clip's own rate.
type FrameSequenceOptions struct {
	// Format selects PNG (default) or JPEG.
	Format ImageFormat
	// Quality is the JPEG quality 1..100 (default 90); ignored for PNG.
	Quality int
	// Rate overrides the sampling rate; the zero value uses the clip's rate.
	Rate Rate
	// Prefix is the filename stem before the zero-padded index (default "frame_").
	Prefix string
}

// WriteFrameSequence renders v and writes each frame to dir as a numbered image
// (PNG or JPEG). The directory is created if missing. It returns the number of
// frames written. A build error carried on v is returned before any file is
// created. Canceling ctx stops the render.
func WriteFrameSequence(ctx context.Context, v *Video, dir string, opts FrameSequenceOptions) (int, error) {
	if v.err != nil {
		return 0, v.err
	}
	// Validate an override rate before touching the filesystem: a malformed rate
	// (non-positive Num or Den) makes FrameTime constant, so the index-driven
	// loop would never reach the duration and spin forever. Reject it here so an
	// invalid call leaves no directory behind.
	if opts.Rate.Num != 0 && (opts.Rate.Num <= 0 || opts.Rate.Den <= 0) {
		return 0, clip.Wrap("write frame sequence", clip.ErrNoRate)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return 0, clip.Wrap("write frame sequence", err)
	}
	prefix := opts.Prefix
	if prefix == "" {
		prefix = "frame_"
	}
	quality := opts.Quality
	if quality <= 0 {
		quality = 90
	}
	ext := "png"
	if opts.Format == FormatJPEG {
		ext = "jpg"
	}

	encode := func(rf clip.RenderedFrame) error {
		path := filepath.Join(dir, fmt.Sprintf("%s%05d.%s", prefix, rf.Index, ext))
		f, err := os.Create(path)
		if err != nil {
			return clip.Wrap("write frame sequence", err)
		}
		if err := encodeFrame(f, rf, opts.Format, quality); err != nil {
			f.Close()
			return err
		}
		return f.Close()
	}

	if opts.Rate.Num != 0 {
		return video.IterFramesAt(ctx, v.inner, opts.Rate, encode)
	}
	return video.IterFrames(ctx, v.inner, encode)
}

// encodeFrame writes one rendered frame, stacking alpha into RGBA for a
// transparent PNG and otherwise encoding the RGB frame directly.
func encodeFrame(f *os.File, rf clip.RenderedFrame, format ImageFormat, quality int) error {
	out := rf.RGB
	if rf.Alpha != nil && format == FormatPNG {
		rgba := clip.NewFrame(rf.RGB.W, rf.RGB.H, clip.RGBA)
		if err := imagex.StackAlphaInto(rgba, rf.RGB, rf.Alpha); err != nil {
			return err
		}
		out = rgba
	}
	if format == FormatJPEG {
		return imagex.EncodeJPEG(f, out, quality)
	}
	return imagex.EncodePNG(f, out)
}
