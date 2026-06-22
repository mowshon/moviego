package text

import (
	"strings"
	"testing"
	"time"

	"github.com/mowshon/moviego/v2/clip"
)

func sec(s float64) clip.Time { return clip.Time(s * float64(time.Second)) }

func TestParseSRT(t *testing.T) {
	const src = `1
00:00:01,000 --> 00:00:04,000
Hello world

2
00:00:05,500 --> 00:00:07,250
Second line
wraps two rows
`
	cues, err := ParseSRT(strings.NewReader(src))
	if err != nil {
		t.Fatal(err)
	}
	if len(cues) != 2 {
		t.Fatalf("want 2 cues, got %d", len(cues))
	}
	if cues[0].Start != sec(1) || cues[0].End != sec(4) || cues[0].Text != "Hello world" {
		t.Fatalf("cue 0 mismatch: %+v", cues[0])
	}
	if cues[1].Start != sec(5.5) || cues[1].End != sec(7.25) {
		t.Fatalf("cue 1 timing mismatch: %+v", cues[1])
	}
	if cues[1].Text != "Second line\nwraps two rows" {
		t.Fatalf("multi-line cue text not preserved: %q", cues[1].Text)
	}
}

func TestParseVTT(t *testing.T) {
	const src = `WEBVTT - Some title

NOTE this is a comment block
that spans lines

00:01.000 --> 00:03.000 line:90% align:center
Short form mm:ss

cue-id-1
00:00:04.000 --> 00:00:06.000
Full form
`
	cues, err := ParseVTT(strings.NewReader(src))
	if err != nil {
		t.Fatal(err)
	}
	if len(cues) != 2 {
		t.Fatalf("want 2 cues, got %d: %+v", len(cues), cues)
	}
	if cues[0].Start != sec(1) || cues[0].End != sec(3) || cues[0].Text != "Short form mm:ss" {
		t.Fatalf("vtt cue 0 mismatch: %+v", cues[0])
	}
	if cues[1].Start != sec(4) || cues[1].End != sec(6) || cues[1].Text != "Full form" {
		t.Fatalf("vtt cue 1 mismatch: %+v", cues[1])
	}
}

func TestParseTrailingBlockNoNewline(t *testing.T) {
	// A final cue with no trailing blank line must still flush.
	cues, err := ParseSRT(strings.NewReader("1\n00:00:00,000 --> 00:00:01,000\nlast"))
	if err != nil {
		t.Fatal(err)
	}
	if len(cues) != 1 || cues[0].Text != "last" {
		t.Fatalf("trailing block not flushed: %+v", cues)
	}
}

func TestParseBadTimestamp(t *testing.T) {
	_, err := ParseSRT(strings.NewReader("1\nnot a timestamp --> also bad\ntext\n"))
	if err == nil {
		t.Fatal("malformed timing line must error")
	}
}

func TestParseEmptyInput(t *testing.T) {
	cues, err := ParseSRT(strings.NewReader("\n\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(cues) != 0 {
		t.Fatalf("empty input must yield no cues, got %d", len(cues))
	}
}
