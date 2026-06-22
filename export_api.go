package mgo

import (
	"context"

	"github.com/mowshon/moviego/v2/render"
)

// ExportOptions controls a video export. A zero Rate uses the clip's own rate.
type ExportOptions = render.ExportOptions

// CodecHEVCAlpha is the opt-in codec for a web-friendly transparent .mp4/.mov:
// HEVC with an alpha channel (tagged hvc1), played by Safari and QuickTime. Set
// it as ExportOptions.Codec when exporting a clip with a mask, e.g.
//
//	mgo.WriteVideo(ctx, clip, "out.mp4", mgo.ExportOptions{Codec: mgo.CodecHEVCAlpha})
//
// It uses macOS VideoToolbox and is unavailable on builds without it (the
// export then fails with a clear error rather than dropping alpha). For a
// portable transparent export, write to a .mov (lossless qtrle) instead.
const CodecHEVCAlpha = "hevc_videotoolbox"

// WriteVideo renders v and encodes it to out. Canceling ctx stops the export
// and kills FFmpeg. A build error recorded while assembling v (see Video.Err)
// is returned before any file is created.
func WriteVideo(ctx context.Context, v *Video, out string, opts ExportOptions) error {
	if v.err != nil {
		return v.err
	}
	return render.WriteVideo(ctx, v.inner, out, opts)
}
