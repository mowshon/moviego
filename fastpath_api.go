package mgo

import (
	"context"

	"github.com/mowshon/moviego/v2/ffmpeg"
	"github.com/mowshon/moviego/v2/video"
)

// This file exposes the "avoid Go pixels" file-to-file fast paths.
// They are deliberately separate from the lazy editing graph (OpenVideo/...):
// stream copy is only keyframe-accurate and the concat demuxer only joins files
// that already share codec/parameters, so routing them through WriteVideo
// implicitly would surprise callers. Reach for them explicitly when the speed
// is worth the constraints.

// StreamCopyTrim cuts [start, start+dur) out of the file in into out without
// re-encoding any stream (a fast keyframe seek plus -c copy). dur <= 0 trims to
// the end. The cut snaps to the keyframe at or before start, so the result can
// begin slightly earlier than requested; when frame accuracy matters, trim
// through the editing graph (OpenVideo(...).Subclip(...)) and WriteVideo
// instead, which re-encodes from exact frames.
func StreamCopyTrim(ctx context.Context, in, out string, start, dur Time) error {
	return ffmpeg.StreamCopyTrim(ctx, in, out, start, dur)
}

// ConcatFiles joins files end to end into out, preferring the no-re-encode
// concat demuxer and falling back to a graph re-encode when that is not possible.
//
// It first tries the concat demuxer, which stream-copies every segment when the
// inputs share a codec and stream parameters. If that fails (mismatched codecs,
// sizes, or pixel formats) — and the context is still live — it falls back to
// the editing graph: opening each file, building a Concat(...) clip, and
// re-encoding through WriteVideo (the Go render path, which handles arbitrary
// inputs by centering/padding to a common canvas). This is a full re-encode, not
// an FFmpeg concat-filter pass; it is correct but slower and changes the codec.
// Reach for ffmpeg.ConcatDemux directly when only the copy fast path is
// acceptable.
func ConcatFiles(ctx context.Context, out string, files ...string) error {
	if len(files) == 0 {
		// Surfaces the documented empty-input error.
		return ffmpeg.ConcatDemux(ctx, files, out)
	}
	if err := ffmpeg.ConcatDemux(ctx, files, out); err == nil {
		return nil
	}
	// Don't burn work on the re-encode fallback if the demux failure was the
	// caller canceling: report the cancellation instead.
	if err := ctx.Err(); err != nil {
		return err
	}
	// Fallback: re-encode through the editing graph. Sources are opened with the
	// caller's ctx so probing is cancelable too.
	clips := make([]*Video, 0, len(files))
	defer func() {
		for _, v := range clips {
			_ = v.Close()
		}
	}()
	for _, f := range files {
		n, err := video.OpenFile(ctx, f)
		if err != nil {
			return err
		}
		clips = append(clips, &Video{inner: n})
	}
	return WriteVideo(ctx, Concat(clips...), out, ExportOptions{})
}
