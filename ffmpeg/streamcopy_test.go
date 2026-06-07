package ffmpeg

import (
	"strings"
	"testing"
	"time"
)

func TestStreamCopyTrimArgs(t *testing.T) {
	got := strings.Join(StreamCopyTrimArgs("in.mp4", "out.mp4", time.Second, 2*time.Second), " ")
	want := "-nostdin -loglevel error -y -ss 1 -i in.mp4 -t 2 -map 0:v? -map 0:a? -c copy -avoid_negative_ts make_zero out.mp4"
	if got != want {
		t.Errorf("args:\n got %q\nwant %q", got, want)
	}
}

// TestStreamCopyTrimArgsToEnd: a zero duration omits -t (trim to end); a zero
// start omits -ss (copy from the beginning).
func TestStreamCopyTrimArgsToEnd(t *testing.T) {
	got := strings.Join(StreamCopyTrimArgs("in.mp4", "out.mp4", 0, 0), " ")
	want := "-nostdin -loglevel error -y -i in.mp4 -map 0:v? -map 0:a? -c copy -avoid_negative_ts make_zero out.mp4"
	if got != want {
		t.Errorf("args:\n got %q\nwant %q", got, want)
	}
}

func TestConcatListContent(t *testing.T) {
	got := ConcatListContent([]string{"a.mp4", "dir/b's.mp4"})
	want := "file 'a.mp4'\nfile 'dir/b'\\''s.mp4'\n"
	if got != want {
		t.Errorf("list:\n got %q\nwant %q", got, want)
	}
}

func TestConcatDemuxArgs(t *testing.T) {
	got := strings.Join(ConcatDemuxArgs("list.txt", "out.mp4"), " ")
	want := "-nostdin -loglevel error -y -f concat -safe 0 -i list.txt -map 0:v? -map 0:a? -c copy out.mp4"
	if got != want {
		t.Errorf("args:\n got %q\nwant %q", got, want)
	}
}

// TestStreamCopyTrimArgsLeadingDash exercises the filename-safety rule for both
// the input and the output path.
func TestStreamCopyTrimArgsLeadingDash(t *testing.T) {
	args := StreamCopyTrimArgs("-in.mp4", "-out.mp4", 0, time.Second)
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "-i ./-in.mp4") {
		t.Errorf("expected ./-in.mp4 input, got %q", joined)
	}
	if !strings.HasSuffix(joined, " ./-out.mp4") {
		t.Errorf("expected ./-out.mp4 output, got %q", joined)
	}
}

// TestConcatDemuxArgsLeadingDash confirms the output path is dash-escaped too.
func TestConcatDemuxArgsLeadingDash(t *testing.T) {
	joined := strings.Join(ConcatDemuxArgs("list.txt", "-out.mp4"), " ")
	if !strings.HasSuffix(joined, " ./-out.mp4") {
		t.Errorf("expected ./-out.mp4 output, got %q", joined)
	}
}
