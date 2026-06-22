package text

import (
	"image/color"
	"strings"
	"testing"

	"github.com/mowshon/moviego/v2/clip"
)

func TestParseJSONArrayForm(t *testing.T) {
	const src = `[
	  {"start": "00:00:01,500", "end": "00:00:04,200", "text": "Welcome to the tutorial."},
	  {"start": "00:00:04,500", "end": "00:00:07,800", "text": "Let's explore the JSON format."}
	]`
	cues, err := ParseJSON(strings.NewReader(src))
	if err != nil {
		t.Fatal(err)
	}
	if len(cues) != 2 {
		t.Fatalf("want 2 cues, got %d", len(cues))
	}
	if cues[0].Start != sec(1.5) || cues[0].End != sec(4.2) || cues[0].Text != "Welcome to the tutorial." {
		t.Fatalf("cue 0 mismatch: %+v", cues[0])
	}
	if cues[1].Start != sec(4.5) || cues[1].End != sec(7.8) {
		t.Fatalf("cue 1 timing mismatch: %+v", cues[1])
	}
}

func TestParseJSONObjectFormWithWords(t *testing.T) {
	const src = `{
	  "styles": {"font": "Arial", "color": "#FFFFFF"},
	  "cues": [
	    {
	      "start": 1500, "end": 4200, "text": "Welcome to the tutorial.",
	      "words": [
	        {"word": "Welcome", "start": 1500, "end": 1800},
	        {"word": "to", "start": 1850, "end": 1950},
	        {"word": "the", "start": 2000, "end": 2100},
	        {"word": "tutorial.", "start": 2200, "end": 2600}
	      ]
	    }
	  ]
	}`
	styles, cues, err := ParseJSONStyled(strings.NewReader(src))
	if err != nil {
		t.Fatal(err)
	}
	if styles.Font != "Arial" || styles.Color != "#FFFFFF" {
		t.Fatalf("styles mismatch: %+v", styles)
	}
	if len(cues) != 1 {
		t.Fatalf("want 1 cue, got %d", len(cues))
	}
	c := cues[0]
	if c.Start != sec(1.5) || c.End != sec(4.2) {
		t.Fatalf("cue timing (ms) mismatch: %+v", c)
	}
	if len(c.Words) != 4 {
		t.Fatalf("want 4 words, got %d", len(c.Words))
	}
	if c.Words[0].Text != "Welcome" || c.Words[0].Start != sec(1.5) || c.Words[0].End != sec(1.8) {
		t.Fatalf("word 0 mismatch: %+v", c.Words[0])
	}
	if c.Words[3].Text != "tutorial." || c.Words[3].End != sec(2.6) {
		t.Fatalf("word 3 mismatch: %+v", c.Words[3])
	}
}

func TestParseJSONMixedTimestampForms(t *testing.T) {
	// String timestamps in the object form must also work.
	const src = `{"cues":[{"start":"00:01.000","end":"00:02.500","text":"hi"}]}`
	cues, err := ParseJSON(strings.NewReader(src))
	if err != nil {
		t.Fatal(err)
	}
	if len(cues) != 1 || cues[0].Start != sec(1) || cues[0].End != sec(2.5) {
		t.Fatalf("string timestamps in object form failed: %+v", cues)
	}
}

func TestParseJSONInvalid(t *testing.T) {
	if _, err := ParseJSON(strings.NewReader("not json")); err == nil {
		t.Fatal("garbage input must error")
	}
	if _, err := ParseJSON(strings.NewReader(`[{"start":"nope","end":"00:00:01,000","text":"x"}]`)); err == nil {
		t.Fatal("bad timestamp must error")
	}
}

func TestParseHexColor(t *testing.T) {
	cases := []struct {
		in   string
		want color.NRGBA
	}{
		{"#FFFFFF", color.NRGBA{0xff, 0xff, 0xff, 0xff}},
		{"#000000", color.NRGBA{0x00, 0x00, 0x00, 0xff}},
		{"ff0000", color.NRGBA{0xff, 0x00, 0x00, 0xff}},
		{"#0f0", color.NRGBA{0x00, 0xff, 0x00, 0xff}},
		{"#11223380", color.NRGBA{0x11, 0x22, 0x33, 0x80}},
	}
	for _, c := range cases {
		got, ok := parseHexColor(c.in)
		if !ok || got != c.want {
			t.Errorf("parseHexColor(%q) = %v, %v; want %v", c.in, got, ok, c.want)
		}
	}
	if _, ok := parseHexColor("#xyz"); ok {
		t.Error("invalid hex must fail")
	}
}

func TestApplyStylesIgnoresUnresolvableFont(t *testing.T) {
	out := applyStyles(SubtitleOptions{Size: clip.Size{W: 100, H: 100}}, Styles{Font: "Arial", Color: "#FF0000"})
	if out.FontPath != "" {
		t.Fatalf("non-file font name must be ignored, got %q", out.FontPath)
	}
	if out.Color == nil {
		t.Fatal("hex color from styles should be applied")
	}
}

func TestApplyStylesDoesNotOverrideCaller(t *testing.T) {
	caller := SubtitleOptions{Color: color.White}
	out := applyStyles(caller, Styles{Color: "#FF0000"})
	if out.Color != color.Color(color.White) {
		t.Fatal("caller-set color must win over styles")
	}
}
