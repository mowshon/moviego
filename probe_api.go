package mgo

import (
	"context"

	"github.com/mowshon/moviego/v2/ffmpeg"
	videoio "github.com/mowshon/moviego/v2/video/io"
)

// ProbeOptions tunes Probe. See ffmpeg.ProbeOptions; the one knob is the frame
// rate source (FPSDefault uses r_frame_rate, FPSAvg uses avg_frame_rate).
type ProbeOptions = ffmpeg.ProbeOptions

// FPSSource selects which ffprobe field supplies the frame rate.
type FPSSource = ffmpeg.FPSSource

// Frame rate sources for ProbeOptions.
const (
	FPSDefault = ffmpeg.FPSDefault // r_frame_rate (the default)
	FPSTbr     = ffmpeg.FPSTbr
	FPSAvg     = ffmpeg.FPSAvg // avg_frame_rate
)

// Info is the metadata Probe surfaces for a media file. Audio is nil when the
// file has no audio track.
type Info struct {
	Duration    Time
	Size        Size // display size, after applying Rotation
	RawSize     Size // stored size, before rotation
	Rate        Rate
	VariableFPS bool   // avg_frame_rate diverges meaningfully from r_frame_rate
	Rotation    int    // normalized display rotation: 0/90/180/270
	Codec       string // video codec name
	PixFmt      string
	HasAlpha    bool  // derived from PixFmt (ffprobe reports no hasAlpha flag)
	FrameCount  int64 // 0 when ffprobe does not report it
	Format      string
	Audio       *AudioInfo
}

// AudioInfo is the first audio stream's metadata.
type AudioInfo struct {
	Codec      string
	SampleRate int
	Channels   int
	Layout     string
}

// Probe runs ffprobe and returns structured metadata for path's first video and
// audio stream. Pass a ProbeOptions to choose the frame rate source. It is a
// thin wrapper over ffmpeg.Probe; bitrate is not in the probe output today and
// is intentionally omitted.
func Probe(path string, opts ...ProbeOptions) (Info, error) {
	var opt ProbeOptions
	if len(opts) > 0 {
		opt = opts[0]
	}
	mi, err := ffmpeg.Probe(context.Background(), path, opt)
	if err != nil {
		return Info{}, err
	}
	info := Info{
		Duration: mi.Duration,
		Format:   mi.Format,
	}
	if v := mi.Video; v != nil {
		info.Size = v.Size
		info.RawSize = v.RawSize
		info.Rate = v.Rate
		info.VariableFPS = mi.VariableFPS
		info.Rotation = v.Rotation
		info.Codec = v.Codec
		info.PixFmt = v.PixFmt
		info.HasAlpha = videoio.IsAlphaPixFmt(v.PixFmt)
		info.FrameCount = v.NBFrames
	}
	if a := mi.Audio; a != nil {
		info.Audio = &AudioInfo{
			Codec:      a.Codec,
			SampleRate: a.SampleRate,
			Channels:   a.Channels,
			Layout:     a.Layout,
		}
	}
	return info, nil
}
