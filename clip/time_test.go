package clip

import (
	"testing"
	"time"
)

func TestParseTime(t *testing.T) {
	cases := []struct {
		in   string
		want Time
	}{
		{"33.5", 33500 * time.Millisecond},
		{"1:33,5", 93500 * time.Millisecond},
		{"01:01:33.045", 3693045 * time.Millisecond},
		{"01:01:33,5", 3693500 * time.Millisecond},
		{"0", 0},
	}
	for _, c := range cases {
		got, err := ParseTime(c.in)
		if err != nil {
			t.Errorf("ParseTime(%q) error: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("ParseTime(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestParseTimeError(t *testing.T) {
	if _, err := ParseTime("12:ab"); err == nil {
		t.Error("ParseTime(\"12:ab\") expected error, got nil")
	}
}

func TestFiniteAndSentinels(t *testing.T) {
	if Finite(Infinite) {
		t.Error("Finite(Infinite) = true, want false")
	}
	if !Finite(0) || !Finite(time.Second) {
		t.Error("Finite(finite) = false, want true")
	}
	// DurationOr/EndOr collapse the (value, ok) bookkeeping to the sentinel.
	if got := DurationOr(time.Second, true); got != time.Second {
		t.Errorf("DurationOr(1s, true) = %v, want 1s", got)
	}
	if got := DurationOr(time.Second, false); got != Infinite {
		t.Errorf("DurationOr(_, false) = %v, want Infinite", got)
	}
	if got := EndOr(time.Second, 2*time.Second, true); got != 3*time.Second {
		t.Errorf("EndOr(1s, 2s, true) = %v, want 3s", got)
	}
	// An unbounded clip's End must be Infinite, never start+Infinite (overflow).
	if got := EndOr(time.Second, 0, false); got != Infinite {
		t.Errorf("EndOr(1s, _, false) = %v, want Infinite", got)
	}
}
