package ffmpeg

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/mowshon/moviego/v2/clip"
)

// This file holds the "avoid Go pixels" fast paths that do not even need a
// filtergraph: a pure cut via stream copy and a concatenation of compatible
// files via the concat demuxer. Both are explicit
// utilities rather than automatic export rerouting, because stream copy is only
// keyframe-accurate and the concat demuxer only works for files that already
// share a codec and parameters — surprises a silent fast path must not impose.

// StreamCopyTrimArgs builds the argument list for a stream-copy trim: a fast
// keyframe seek (-ss before -i), a duration limit (-t), and -c copy so no
// stream is re-encoded. The cut snaps to the keyframe at or before start, so
// the real start can be a little earlier than requested; that keyframe caveat
// is the whole reason this is an opt-in path, not the default export.
func StreamCopyTrimArgs(in, out string, start, dur clip.Time) []string {
	args := []string{"-nostdin", "-loglevel", "error", "-y"}
	if start > 0 {
		args = append(args, "-ss", secs(start))
	}
	args = append(args, "-i", SafeInputPath(in))
	if dur > 0 {
		args = append(args, "-t", secs(dur))
	}
	// Map only the video and audio streams (optional, so a video-only or
	// audio-only source still works), copy them, and reset edge timestamps so
	// the cut starts at zero. "-map 0" would also copy data/subtitle/attachment
	// streams, which some output containers reject and which would fail the copy;
	// this mirrors MoviePy-style cuts that keep only A/V.
	args = append(args,
		"-map", "0:v?", "-map", "0:a?",
		"-c", "copy", "-avoid_negative_ts", "make_zero", SafeInputPath(out))
	return args
}

// StreamCopyTrim cuts [start, start+dur) of in into out without re-encoding.
// dur <= 0 means "to the end". See StreamCopyTrimArgs for the keyframe caveat.
func StreamCopyTrim(ctx context.Context, in, out string, start, dur clip.Time) error {
	bin, err := FFmpegPath()
	if err != nil {
		return err
	}
	proc, err := Start(ctx, Spec{Path: bin, Args: StreamCopyTrimArgs(in, out, start, dur)})
	if err != nil {
		return err
	}
	if err := proc.Wait(); err != nil {
		_ = os.Remove(out)
		return clip.Wrap("stream-copy trim "+out, err)
	}
	return nil
}

// ConcatListContent renders the concat-demuxer list file for files, one
// "file '<path>'" line per input with single quotes escaped, in order.
func ConcatListContent(files []string) string {
	var b strings.Builder
	for _, f := range files {
		b.WriteString("file '")
		b.WriteString(strings.ReplaceAll(f, "'", `'\''`))
		b.WriteString("'\n")
	}
	return b.String()
}

// ConcatDemuxArgs builds the argument list for the concat demuxer reading list
// and stream-copying every segment into out. -safe 0 permits arbitrary paths.
// Only audio and video streams are copied (-map 0:v? -map 0:a?); data, subtitle,
// and attachment streams are dropped, matching StreamCopyTrimArgs behavior.
func ConcatDemuxArgs(list, out string) []string {
	return []string{
		"-nostdin", "-loglevel", "error", "-y",
		"-f", "concat", "-safe", "0",
		"-i", SafeInputPath(list),
		"-map", "0:v?", "-map", "0:a?",
		"-c", "copy", SafeInputPath(out),
	}
}

// ConcatDemux joins files end to end into out without re-encoding. It only
// works when the inputs share a codec and stream parameters (the concat
// demuxer's requirement); incompatible inputs make FFmpeg fail, and the caller
// should fall back to the concat filter (re-encode). It writes a temporary list
// file in the system temp dir and removes it afterward.
//
// Each input is resolved to an absolute path before being written to the list.
// The concat demuxer resolves a relative "file '...'" entry against the list
// file's own directory (not the process CWD), so a relative input would
// otherwise be looked up next to the temp file and fail; absolute paths sidestep
// that rule entirely.
func ConcatDemux(ctx context.Context, files []string, out string) error {
	if len(files) == 0 {
		return clip.Wrap("concat demux", clip.ErrVideoCorrupted)
	}
	bin, err := FFmpegPath()
	if err != nil {
		return err
	}
	abs := make([]string, len(files))
	for i, f := range files {
		a, err := filepath.Abs(f)
		if err != nil {
			return clip.Wrap("concat demux", err)
		}
		abs[i] = a
	}
	list, err := os.CreateTemp("", "moviego-concat-*.txt")
	if err != nil {
		return clip.Wrap("concat demux", err)
	}
	listPath := list.Name()
	defer os.Remove(listPath)
	if _, err := list.WriteString(ConcatListContent(abs)); err != nil {
		list.Close()
		return clip.Wrap("concat demux", err)
	}
	if err := list.Close(); err != nil {
		return clip.Wrap("concat demux", err)
	}

	proc, err := Start(ctx, Spec{Path: bin, Args: ConcatDemuxArgs(listPath, out)})
	if err != nil {
		return err
	}
	if err := proc.Wait(); err != nil {
		_ = os.Remove(out)
		return clip.Wrap("concat demux "+out, err)
	}
	return nil
}
