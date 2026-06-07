package ffmpeg

import "testing"

func TestRingWriter(t *testing.T) {
	r := newRingWriter(8)
	mustWrite(t, r, "abc")
	if got := string(r.Bytes()); got != "abc" {
		t.Fatalf("after abc: %q", got)
	}
	// Fill exactly to capacity.
	mustWrite(t, r, "defgh")
	if got := string(r.Bytes()); got != "abcdefgh" {
		t.Fatalf("at capacity: %q", got)
	}
	// Wrap: oldest bytes drop, tail order preserved.
	mustWrite(t, r, "ij")
	if got := string(r.Bytes()); got != "cdefghij" {
		t.Fatalf("after wrap: %q", got)
	}
}

func TestRingWriterOverlongWrite(t *testing.T) {
	r := newRingWriter(4)
	mustWrite(t, r, "abcdefg") // longer than capacity: keep last 4
	if got := string(r.Bytes()); got != "defg" {
		t.Fatalf("overlong: %q", got)
	}
}

func mustWrite(t *testing.T, r *ringWriter, s string) {
	t.Helper()
	n, err := r.Write([]byte(s))
	if err != nil || n != len(s) {
		t.Fatalf("Write(%q) = %d, %v", s, n, err)
	}
}
