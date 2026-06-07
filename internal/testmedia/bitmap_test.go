package testmedia

import "testing"

func TestBitmapFrame(t *testing.T) {
	f, err := BitmapFrame([]string{"RG", "BW"}, nil)
	if err != nil {
		t.Fatalf("BitmapFrame error: %v", err)
	}
	if f.W != 2 || f.H != 2 {
		t.Fatalf("size = %dx%d, want 2x2", f.W, f.H)
	}
	// Top-left red, top-right green.
	if got := f.Pix[0:3]; got[0] != 255 || got[1] != 0 || got[2] != 0 {
		t.Errorf("pixel(0,0) = %v, want red", got)
	}
	if got := f.Pix[3:6]; got[0] != 0 || got[1] != 255 || got[2] != 0 {
		t.Errorf("pixel(1,0) = %v, want green", got)
	}
	// Bottom-right white.
	off := 1*f.Stride + 1*3
	if got := f.Pix[off : off+3]; got[0] != 255 || got[1] != 255 || got[2] != 255 {
		t.Errorf("pixel(1,1) = %v, want white", got)
	}
}

func TestBitmapFrameErrors(t *testing.T) {
	if _, err := BitmapFrame(nil, nil); err == nil {
		t.Error("empty rows expected error")
	}
	if _, err := BitmapFrame([]string{"RG", "B"}, nil); err == nil {
		t.Error("ragged rows expected error")
	}
	if _, err := BitmapFrame([]string{"RZ"}, nil); err == nil {
		t.Error("unknown code expected error")
	}
}
