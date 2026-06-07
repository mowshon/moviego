package render

import "testing"

func TestHardwareCodec(t *testing.T) {
	cases := map[string]struct {
		want string
		ok   bool
	}{
		"nvenc":        {"h264_nvenc", true},
		"CUDA":         {"h264_nvenc", true},
		"qsv":          {"h264_qsv", true},
		"videotoolbox": {"h264_videotoolbox", true},
		" vt ":         {"h264_videotoolbox", true},
		"":             {"", false},
		"bogus":        {"", false},
	}
	for in, want := range cases {
		got, ok := hardwareCodec(in)
		if got != want.want || ok != want.ok {
			t.Errorf("hardwareCodec(%q) = (%q, %v), want (%q, %v)", in, got, ok, want.want, want.ok)
		}
	}
}
