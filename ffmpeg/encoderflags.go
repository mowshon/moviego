package ffmpeg

import "strings"

// EncoderUsesPreset reports whether codec accepts the libx264-style "-preset"
// flag. The VideoToolbox encoders (h264_videotoolbox, hevc_videotoolbox) reject
// "-preset" and error out; libx264/libx265 and the nvenc/qsv hardware encoders
// all accept it. Both the fused (filter_complex) and Go (rawvideo) encode paths
// consult this so a hardware export only ever emits encoder-appropriate flags.
func EncoderUsesPreset(codec string) bool {
	return !strings.Contains(strings.ToLower(codec), "videotoolbox")
}
