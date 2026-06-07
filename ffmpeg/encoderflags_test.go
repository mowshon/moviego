package ffmpeg

import "testing"

func TestEncoderUsesPreset(t *testing.T) {
	cases := map[string]bool{
		"libx264":           true,
		"libx265":           true,
		"h264_nvenc":        true,
		"h264_qsv":          true,
		"h264_videotoolbox": false,
		"hevc_videotoolbox": false,
		"":                  true,
	}
	for codec, want := range cases {
		if got := EncoderUsesPreset(codec); got != want {
			t.Errorf("EncoderUsesPreset(%q) = %v, want %v", codec, got, want)
		}
	}
}
