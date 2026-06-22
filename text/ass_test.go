package text

import (
	"strings"
	"testing"
	"time"

	"github.com/mowshon/moviego/v2/clip"
)

const sampleASS = `[Script Info]
Title: Demo
ScriptType: v4.00+

[V4+ Styles]
Format: Name, Fontname, Fontsize
Style: Default,Arial,20

[Events]
Format: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text
Dialogue: 0,0:00:01.00,0:00:03.50,Default,,0,0,0,,Hello {\i1}world{\i0}
Comment: 0,0:00:03.50,0:00:04.00,Default,,0,0,0,,ignored
Dialogue: 0,0:00:04.00,0:00:06.00,Default,,0,0,0,,Line one\NLine two, with a comma
`

func TestParseASS(t *testing.T) {
	cues, err := ParseASS(strings.NewReader(sampleASS))
	if err != nil {
		t.Fatal(err)
	}
	if len(cues) != 2 {
		t.Fatalf("got %d cues, want 2 (the Comment line is skipped)", len(cues))
	}

	if cues[0].Start != clip.Time(time.Second) || cues[0].End != clip.Time(3500*time.Millisecond) {
		t.Errorf("cue0 interval = [%v,%v], want [1s,3.5s]", cues[0].Start, cues[0].End)
	}
	if cues[0].Text != "Hello world" {
		t.Errorf("cue0 text = %q, want override tags stripped to %q", cues[0].Text, "Hello world")
	}

	// \N becomes a newline; the comma inside the text survives the column split.
	if cues[1].Text != "Line one\nLine two, with a comma" {
		t.Errorf("cue1 text = %q, want the \\N newline and trailing comma kept", cues[1].Text)
	}
}

func TestParseASSTime(t *testing.T) {
	got, ok := parseASSTime("1:02:03.04")
	if !ok {
		t.Fatal("parseASSTime failed")
	}
	want := clip.Time(time.Hour + 2*time.Minute + 3*time.Second + 40*time.Millisecond)
	if got != want {
		t.Errorf("parseASSTime = %v, want %v", got, want)
	}
	if _, ok := parseASSTime("not:a:time"); ok {
		t.Error("expected malformed timestamp to fail")
	}
}
