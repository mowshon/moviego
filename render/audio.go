package render

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/mowshon/moviego/v2/audio"
	audioio "github.com/mowshon/moviego/v2/audio/io"
	"github.com/mowshon/moviego/v2/clip"
	"github.com/mowshon/moviego/v2/video"
)

// renderAudio renders v's audio sidecar (if any) to a temp file whose codec and
// container are compatible with out, so the video encode can stream-copy it. It
// returns the temp path (empty when there is no audio to render), a cleanup
// function that removes the temp file, and any render error.
func renderAudio(ctx context.Context, v video.VideoClip, out string, opts ExportOptions) (string, func(), error) {
	noop := func() {}
	if opts.DisableAudio {
		return "", noop, nil
	}
	a := v.Audio()
	if a == nil || a.SampleRate() <= 0 || a.Channels() <= 0 {
		return "", noop, nil
	}
	dur := v.Duration()
	if !clip.Finite(dur) || dur <= 0 {
		// The video timeline drives audio length; without it there is nothing to
		// sync against, so skip audio rather than guess.
		return "", noop, nil
	}

	ext, codec := audioTempSpec(out)
	if opts.AudioCodec != "" {
		codec = opts.AudioCodec
	}
	tmp, err := os.CreateTemp("", "mgo-audio-*"+ext)
	if err != nil {
		return "", noop, clip.Wrap("render audio", err)
	}
	path := tmp.Name()
	_ = tmp.Close()
	cleanup := func() { _ = os.Remove(path) }

	// Reap every decoder opened during this render. The mixed sidecar of a
	// composite/concat is a transient graph whose leaf decoders are held by no
	// node the caller can Close, so a child read to exactly its end (no EOF) would
	// otherwise leave a stray FFmpeg process behind. Reaping here is also correct
	// for a plain file's own audio decoder: nothing reads audio after this (the
	// video render never touches the sidecar), and the owning node's later Close
	// is an idempotent no-op.
	reaper := &audioio.Reaper{}
	rctx := audioio.WithReaper(ctx, reaper)
	if err := audio.RenderToTemp(rctx, a, path, codec, opts.AudioBitrate, dur); err != nil {
		_ = reaper.CloseAll()
		cleanup()
		return "", noop, err
	}
	if err := reaper.CloseAll(); err != nil {
		cleanup()
		return "", noop, clip.Wrap("render audio", err)
	}
	return path, cleanup, nil
}

// audioTempSpec maps an output container extension to a temp audio container and
// codec that the container can stream-copy. The temp file is muxed with
// `-c:a copy`, so the codec must be valid for both the temp and final container.
func audioTempSpec(out string) (ext, codec string) {
	switch strings.ToLower(filepath.Ext(out)) {
	case ".webm":
		return ".webm", "libopus"
	case ".ogg", ".oga":
		return ".ogg", "libvorbis"
	case ".mkv":
		return ".mka", "aac"
	default: // .mp4, .mov, .m4v, … : AAC in an .m4a elementary container
		return ".m4a", "aac"
	}
}
