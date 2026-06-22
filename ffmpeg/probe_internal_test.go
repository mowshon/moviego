package ffmpeg

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/mowshon/moviego/v2/clip"
)

// A realistic ffprobe JSON blob: NTSC video with a display-matrix rotation plus
// an audio stream. Exercises tag mapping, rotation swap, NTSC snap, and stream
// pairing in one unmarshal.
const sampleJSON = `{
  "streams": [
    {
      "index": 0, "codec_type": "video", "codec_name": "h264",
      "width": 160, "height": 120, "pix_fmt": "yuv420p",
      "r_frame_rate": "30000/1001", "avg_frame_rate": "30000/1001",
      "nb_frames": "60",
      "side_data_list": [{"side_data_type": "Display Matrix", "rotation": -90}]
    },
    {
      "index": 1, "codec_type": "audio", "codec_name": "aac",
      "sample_rate": "44100", "channels": 2, "channel_layout": "stereo"
    }
  ],
  "format": {"format_name": "mov,mp4,m4a,3gp,3g2,mj2", "duration": "2.000000"}
}`

func TestProbeJSONUnmarshal(t *testing.T) {
	var out probeOutput
	if err := json.Unmarshal([]byte(sampleJSON), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	info, err := out.toMediaInfo(ProbeOptions{})
	if err != nil {
		t.Fatalf("toMediaInfo: %v", err)
	}
	if info.Video == nil || info.Audio == nil {
		t.Fatalf("expected video+audio, got %+v", info)
	}
	if info.Video.Rate != (clip.Rate{Num: 30000, Den: 1001}) {
		t.Errorf("rate = %v, want 30000/1001", info.Video.Rate)
	}
	// rotation -90 normalizes to 270 and swaps stored 160x120 to 120x160.
	if info.Video.Rotation != 270 {
		t.Errorf("rotation = %d, want 270", info.Video.Rotation)
	}
	if info.Video.Size != (clip.Size{W: 120, H: 160}) {
		t.Errorf("display size = %v, want 120x160", info.Video.Size)
	}
	if info.Video.RawSize != (clip.Size{W: 160, H: 120}) {
		t.Errorf("raw size = %v, want 160x120", info.Video.RawSize)
	}
	if info.Video.NBFrames != 60 {
		t.Errorf("nb_frames = %d, want 60", info.Video.NBFrames)
	}
	if info.Audio.SampleRate != 44100 || info.Audio.Channels != 2 {
		t.Errorf("audio = %+v, want 44100/2", info.Audio)
	}
	if info.Duration != 2*time.Second {
		t.Errorf("duration = %v, want 2s", info.Duration)
	}
}

func TestProbeFPSSourceSelection(t *testing.T) {
	const j = `{"streams":[{"codec_type":"video","width":64,"height":48,
		"r_frame_rate":"24/1","avg_frame_rate":"30/1"}],"format":{"duration":"1.0"}}`
	var out probeOutput
	if err := json.Unmarshal([]byte(j), &out); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		src  FPSSource
		want clip.Rate
	}{
		{FPSDefault, clip.Rate{Num: 24, Den: 1}},
		{FPSTbr, clip.Rate{Num: 24, Den: 1}}, // no JSON tbr -> r_frame_rate
		{FPSAvg, clip.Rate{Num: 30, Den: 1}},
	}
	for _, c := range cases {
		info, err := out.toMediaInfo(ProbeOptions{FPSSource: c.src})
		if err != nil {
			t.Fatalf("%v: %v", c.src, err)
		}
		if info.Video.Rate != c.want {
			t.Errorf("FPSSource %v: rate = %v, want %v", c.src, info.Video.Rate, c.want)
		}
	}
}

func TestProbeVariableFPS(t *testing.T) {
	cases := []struct {
		r, a string
		want bool
	}{
		{"30/1", "30/1", false},
		{"30000/1001", "30000/1001", false},
		{"30/1", "15/1", true},
		{"30/1", "0/0", false}, // unknown avg -> not flagged
	}
	for _, c := range cases {
		j := `{"streams":[{"codec_type":"video","width":2,"height":2,
			"r_frame_rate":"` + c.r + `","avg_frame_rate":"` + c.a + `"}],"format":{"duration":"1"}}`
		var out probeOutput
		if err := json.Unmarshal([]byte(j), &out); err != nil {
			t.Fatal(err)
		}
		info, err := out.toMediaInfo(ProbeOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if info.VariableFPS != c.want {
			t.Errorf("VFR(r=%s,a=%s) = %v, want %v", c.r, c.a, info.VariableFPS, c.want)
		}
	}
}

func TestRotationFromTag(t *testing.T) {
	// Legacy tags.rotate path, used when no display-matrix side data exists.
	const j = `{"streams":[{"codec_type":"video","width":1920,"height":1080,
		"r_frame_rate":"30/1","tags":{"rotate":"90"}}],"format":{"duration":"1"}}`
	var out probeOutput
	if err := json.Unmarshal([]byte(j), &out); err != nil {
		t.Fatal(err)
	}
	info, err := out.toMediaInfo(ProbeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if info.Video.Rotation != 90 || info.Video.Size != (clip.Size{W: 1080, H: 1920}) {
		t.Errorf("rotation=%d size=%v, want 90 / 1080x1920", info.Video.Rotation, info.Video.Size)
	}
}

func TestProbeNoStreams(t *testing.T) {
	const j = `{"streams":[],"format":{"duration":"1"}}`
	var out probeOutput
	if err := json.Unmarshal([]byte(j), &out); err != nil {
		t.Fatal(err)
	}
	if _, err := out.toMediaInfo(ProbeOptions{}); err == nil {
		t.Error("expected error for stream-less input")
	}
}

func TestParseRate(t *testing.T) {
	cases := []struct {
		in   string
		want clip.Rate
		ok   bool
	}{
		{"30/1", clip.Rate{Num: 30, Den: 1}, true},
		{"30000/1001", clip.Rate{Num: 30000, Den: 1001}, true},
		{"2997/100", clip.Rate{Num: 30000, Den: 1001}, true}, // snaps to canonical NTSC
		{"60/2", clip.Rate{Num: 30, Den: 1}, true},           // reduced
		{"0/0", clip.Rate{}, false},
		{"24", clip.Rate{}, false}, // no slash
	}
	for _, c := range cases {
		got, ok := parseRate(c.in)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("parseRate(%q) = %v,%v want %v,%v", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestNormalizeRotation(t *testing.T) {
	cases := map[int]int{0: 0, 90: 90, 180: 180, 270: 270, -90: 270, 360: 0, 450: 90, -270: 90}
	for in, want := range cases {
		if got := normalizeRotation(in); got != want {
			t.Errorf("normalizeRotation(%d) = %d, want %d", in, got, want)
		}
	}
}
