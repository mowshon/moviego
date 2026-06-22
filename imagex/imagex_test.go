package imagex_test

import (
	"bytes"
	"testing"

	"github.com/mowshon/moviego/v2/clip"
	"github.com/mowshon/moviego/v2/imagex"
)

// solidRGBA builds a w x h RGBA frame filled with r,g,b,a.
func solidRGBA(w, h int, r, g, b, a byte) *clip.Frame {
	f := clip.NewFrame(w, h, clip.RGBA)
	for i := 0; i < len(f.Pix); i += 4 {
		f.Pix[i], f.Pix[i+1], f.Pix[i+2], f.Pix[i+3] = r, g, b, a
	}
	return f
}

func TestDecodeChoosesFormatByAlpha(t *testing.T) {
	// An opaque PNG round-trips to RGB24; a transparent one to RGBA.
	opaque := clip.NewFrame(4, 3, clip.RGB24)
	for i := range opaque.Pix {
		opaque.Pix[i] = byte(i)
	}
	var buf bytes.Buffer
	if err := imagex.EncodePNG(&buf, opaque); err != nil {
		t.Fatalf("encode opaque: %v", err)
	}
	got, err := imagex.DecodeBytes(buf.Bytes())
	if err != nil {
		t.Fatalf("decode opaque: %v", err)
	}
	if got.Format != clip.RGB24 || got.W != 4 || got.H != 3 {
		t.Fatalf("opaque decode = %v %dx%d, want rgb24 4x3", got.Format, got.W, got.H)
	}
	if !bytes.Equal(got.Pix, opaque.Pix) {
		t.Error("opaque round-trip changed pixels")
	}

	transp := solidRGBA(4, 3, 10, 20, 30, 128)
	buf.Reset()
	if err := imagex.EncodePNG(&buf, transp); err != nil {
		t.Fatalf("encode transparent: %v", err)
	}
	got, err = imagex.DecodeBytes(buf.Bytes())
	if err != nil {
		t.Fatalf("decode transparent: %v", err)
	}
	if got.Format != clip.RGBA {
		t.Fatalf("transparent decode format = %v, want rgba", got.Format)
	}
}

func TestSplitAndStackAlphaRoundTrip(t *testing.T) {
	src := solidRGBA(3, 2, 5, 6, 7, 200)
	src.Pix[0], src.Pix[1], src.Pix[2], src.Pix[3] = 1, 2, 3, 4 // vary one pixel

	rgb, alpha, err := imagex.SplitAlpha(src)
	if err != nil {
		t.Fatalf("split: %v", err)
	}
	if rgb.Format != clip.RGB24 || alpha.Format != clip.Gray8 {
		t.Fatalf("split formats = %v/%v", rgb.Format, alpha.Format)
	}
	if alpha.Pix[0] != 4 || alpha.Pix[1] != 200 {
		t.Errorf("alpha = %v, want first pixels 4,200", alpha.Pix[:2])
	}

	back := clip.NewFrame(3, 2, clip.RGBA)
	if err := imagex.StackAlphaInto(back, rgb, alpha); err != nil {
		t.Fatalf("stack: %v", err)
	}
	if !bytes.Equal(back.Pix, src.Pix) {
		t.Error("split then stack did not reproduce the source")
	}
}

func TestResizeSameSizeIsCopy(t *testing.T) {
	src := clip.NewFrame(4, 4, clip.RGB24)
	for i := range src.Pix {
		src.Pix[i] = byte(i * 3)
	}
	dst, err := imagex.Resize(src, clip.Size{W: 4, H: 4})
	if err != nil {
		t.Fatalf("resize: %v", err)
	}
	if !bytes.Equal(dst.Pix, src.Pix) {
		t.Error("same-size resize altered pixels")
	}
}

func TestResizeChangesDimensions(t *testing.T) {
	src := clip.NewFrame(8, 8, clip.RGB24)
	for i := range src.Pix {
		src.Pix[i] = 128
	}
	dst, err := imagex.Resize(src, clip.Size{W: 4, H: 2})
	if err != nil {
		t.Fatalf("resize: %v", err)
	}
	if dst.W != 4 || dst.H != 2 {
		t.Fatalf("size = %dx%d, want 4x2", dst.W, dst.H)
	}
	// A solid source stays solid after resampling.
	for _, b := range dst.Pix {
		if b != 128 {
			t.Fatalf("resized solid frame has value %d, want 128", b)
		}
	}
}

func TestCropInto(t *testing.T) {
	src := clip.NewFrame(4, 4, clip.RGB24)
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			off := y*src.Stride + x*3
			src.Pix[off] = byte(x)
			src.Pix[off+1] = byte(y)
		}
	}
	dst := clip.NewFrame(2, 2, clip.RGB24)
	if err := imagex.CropInto(dst, src, 1, 2); err != nil {
		t.Fatalf("crop: %v", err)
	}
	// Top-left of the crop is source pixel (1,2).
	if dst.Pix[0] != 1 || dst.Pix[1] != 2 {
		t.Errorf("crop origin = (%d,%d), want (1,2)", dst.Pix[0], dst.Pix[1])
	}
	// Out-of-bounds window is rejected.
	if err := imagex.CropInto(dst, src, 3, 3); err == nil {
		t.Error("expected out-of-bounds crop to error")
	}
}

func TestRotate90SwapsAndMaps(t *testing.T) {
	// 2x1 image: pixel (0,0)=red, (1,0)=green.
	src := clip.NewFrame(2, 1, clip.RGB24)
	src.Pix[0], src.Pix[1], src.Pix[2] = 255, 0, 0
	src.Pix[3], src.Pix[4], src.Pix[5] = 0, 255, 0

	dst := clip.NewFrame(1, 2, clip.RGB24) // 90 CW swaps to 1x2
	if err := imagex.RotateInto(dst, src, 1); err != nil {
		t.Fatalf("rotate: %v", err)
	}
	// 90 CW: source (0,0) -> dst (H-1-0, 0) = (0,0); source (1,0) -> dst (0,1).
	if dst.Pix[0] != 255 { // dst (0,0) red
		t.Errorf("dst(0,0) R = %d, want 255", dst.Pix[0])
	}
	off := 1*dst.Stride + 0 // dst (0,1)
	if dst.Pix[off+1] != 255 {
		t.Errorf("dst(0,1) G = %d, want 255", dst.Pix[off+1])
	}
}

func TestRotate180Reverses(t *testing.T) {
	src := clip.NewFrame(2, 2, clip.RGB24)
	for i := range src.Pix {
		src.Pix[i] = byte(i)
	}
	dst := clip.NewFrame(2, 2, clip.RGB24)
	if err := imagex.RotateInto(dst, src, 2); err != nil {
		t.Fatalf("rotate: %v", err)
	}
	// 180 maps the last source pixel to the first dst pixel.
	last := len(src.Pix) - 3
	if dst.Pix[0] != src.Pix[last] {
		t.Errorf("dst first pixel = %d, want source last %d", dst.Pix[0], src.Pix[last])
	}
}

// TestRotateAngleIntoExactCases pins RotateAngleInto on angles that have exact
// integer-grid answers (so bilinear is lossless) plus the expanded-canvas fill.
func TestRotateAngleIntoExactCases(t *testing.T) {
	src := clip.NewFrame(4, 4, clip.RGB24)
	for i := range src.Pix {
		src.Pix[i] = byte(i)
	}

	// angle 0 into a same-size canvas is an exact copy.
	id := clip.NewFrame(4, 4, clip.RGB24)
	if err := imagex.RotateAngleInto(id, src, 0, [3]byte{}); err != nil {
		t.Fatalf("rotate 0: %v", err)
	}
	if !bytes.Equal(id.Pix, src.Pix) {
		t.Error("angle 0 is not an identity copy")
	}

	// An expanded canvas keeps the fill color where dst maps outside the source.
	big := clip.NewFrame(6, 6, clip.RGB24)
	if err := imagex.RotateAngleInto(big, src, 0, [3]byte{9, 9, 9}); err != nil {
		t.Fatalf("rotate expand: %v", err)
	}
	if got := [3]byte{big.Pix[0], big.Pix[1], big.Pix[2]}; got != [3]byte{9, 9, 9} {
		t.Errorf("expanded corner = %v, want fill {9,9,9}", got)
	}
}
