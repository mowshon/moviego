package effect_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/mowshon/moviego/v2/audio"
	"github.com/mowshon/moviego/v2/clip"
	"github.com/mowshon/moviego/v2/effect"
	"github.com/mowshon/moviego/v2/video"
)

// fakeVideo is a lazy (non-static) VideoClip producing a solid fill, used to
// exercise the per-frame TransformNode path. It is deliberately NOT a
// StaticMapper so geometry effects do not take the eager branch.
type fakeVideo struct {
	size   clip.Size
	dur    clip.Time
	hasDur bool
	rate   clip.Rate
	mask   bool
	fill   byte
	access video.AccessClass
}

func (f *fakeVideo) Start() clip.Time    { return 0 }
func (f *fakeVideo) Duration() clip.Time { return clip.DurationOr(f.dur, f.hasDur) }
func (f *fakeVideo) End() clip.Time      { return clip.EndOr(0, f.dur, f.hasDur) }
func (f *fakeVideo) WithStart(clip.Time) clip.Clip {
	c := *f
	return &c
}

func (f *fakeVideo) WithDuration(d clip.Time) clip.Clip {
	c := *f
	c.dur, c.hasDur = d, true
	return &c
}
func (f *fakeVideo) WithEnd(clip.Time, bool) clip.Clip { c := *f; return &c }
func (f *fakeVideo) Size() clip.Size                   { return f.size }
func (f *fakeVideo) Rate() (clip.Rate, bool)           { return f.rate, f.rate.Num != 0 }
func (f *fakeVideo) HasMask() bool                     { return f.mask }
func (f *fakeVideo) Audio() audio.AudioClip            { return nil }
func (f *fakeVideo) ParallelSafe() bool                { return true }
func (f *fakeVideo) SourceAccess() video.AccessClass   { return f.access }
func (f *fakeVideo) Close() error                      { return nil }

func (f *fakeVideo) RenderInto(_ context.Context, _ clip.Time, rgb, alpha *clip.Frame) (bool, error) {
	for i := range rgb.Pix {
		rgb.Pix[i] = f.fill
	}
	if f.mask && alpha != nil {
		for i := range alpha.Pix {
			alpha.Pix[i] = 200
		}
		return true, nil
	}
	return false, nil
}

func (f *fakeVideo) FrameInto(ctx context.Context, t clip.Time, dst *clip.Frame) error {
	_, err := f.RenderInto(ctx, t, dst, nil)
	return err
}

func (f *fakeVideo) MaskInto(_ context.Context, _ clip.Time, dst *clip.Frame) (bool, error) {
	if !f.mask {
		return false, nil
	}
	for i := range dst.Pix {
		dst.Pix[i] = 200
	}
	return true, nil
}

func solidImage(w, h int, v byte) *video.ImageNode {
	rgb := clip.NewFrame(w, h, clip.RGB24)
	for i := range rgb.Pix {
		rgb.Pix[i] = v
	}
	return video.NewImage(rgb, nil)
}

func TestResizeStaticIsEager(t *testing.T) {
	out, err := effect.Resize{Scale: 0.5}.ApplyVideo(solidImage(8, 8, 100))
	if err != nil {
		t.Fatalf("resize: %v", err)
	}
	if out.Size() != (clip.Size{W: 4, H: 4}) {
		t.Errorf("size = %v, want 4x4", out.Size())
	}
	// An eagerly mapped static image stays static.
	if out.SourceAccess() != video.AccessStatic {
		t.Errorf("access = %v, want static (eager)", out.SourceAccess())
	}
}

func TestResizeLazyForNonStatic(t *testing.T) {
	in := &fakeVideo{size: clip.Size{W: 8, H: 8}, fill: 100, access: video.AccessLinear}
	out, err := effect.Resize{Width: 4}.ApplyVideo(in)
	if err != nil {
		t.Fatalf("resize: %v", err)
	}
	if out.Size() != (clip.Size{W: 4, H: 4}) { // aspect-preserved from 8x8
		t.Errorf("size = %v, want 4x4", out.Size())
	}
	// The lazy path preserves the inner source-access class.
	if out.SourceAccess() != video.AccessLinear {
		t.Errorf("access = %v, want linear (lazy)", out.SourceAccess())
	}
	dst := clip.NewFrame(4, 4, clip.RGB24)
	if err := out.FrameInto(context.Background(), 0, dst); err != nil {
		t.Fatalf("frame: %v", err)
	}
	for _, b := range dst.Pix {
		if b != 100 {
			t.Fatalf("resized solid fill = %d, want 100", b)
		}
	}
}

func TestCropClampsToBounds(t *testing.T) {
	out, err := effect.Crop{X: 2, Y: 2, W: 100, H: 100}.ApplyVideo(solidImage(8, 8, 50))
	if err != nil {
		t.Fatalf("crop: %v", err)
	}
	if out.Size() != (clip.Size{W: 6, H: 6}) { // 8-2 in each axis
		t.Errorf("size = %v, want 6x6", out.Size())
	}
}

// TestCropOriginBeyondBounds ensures an X/Y past the source edge clamps to a 1px
// window instead of producing a negative dimension (bounds.W - x).
func TestCropOriginBeyondBounds(t *testing.T) {
	out, err := effect.Crop{X: 100, Y: 50}.ApplyVideo(solidImage(8, 8, 50))
	if err != nil {
		t.Fatalf("crop: %v", err)
	}
	if out.Size() != (clip.Size{W: 1, H: 1}) {
		t.Errorf("size = %v, want 1x1 (last in-bounds pixel)", out.Size())
	}
	// The clamped window must actually render without a stride/length panic.
	dst := clip.NewFrame(1, 1, clip.RGB24)
	if err := out.FrameInto(context.Background(), 0, dst); err != nil {
		t.Fatalf("frame: %v", err)
	}
}

func TestRotateSwapsSizeForQuarterTurn(t *testing.T) {
	out, err := effect.Rotate{Degrees: 90}.ApplyVideo(solidImage(4, 2, 0))
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}
	if out.Size() != (clip.Size{W: 2, H: 4}) {
		t.Errorf("size = %v, want 2x4", out.Size())
	}
}

func TestRotateArbitraryAngleSizes(t *testing.T) {
	// Expand grows the canvas to the rotated bounding box; without it the size is
	// preserved and the corners rotate out of frame.
	expanded, err := effect.Rotate{Degrees: 45, Expand: true}.ApplyVideo(solidImage(10, 10, 0))
	if err != nil {
		t.Fatalf("rotate expand: %v", err)
	}
	want := clip.Size{W: 16, H: 16} // 10*(cos45+sin45) ≈ 14.14 -> ceil 15 -> even 16
	if expanded.Size() != want {
		t.Errorf("expanded size = %v, want %v", expanded.Size(), want)
	}
	// A non-square clip rounds each axis up to even independently, and every
	// expanded dimension is even so chroma-subsampled export never trips ErrOddSize.
	rect, err := effect.Rotate{Degrees: 30, Expand: true}.ApplyVideo(solidImage(100, 50, 0))
	if err != nil {
		t.Fatalf("rotate rect: %v", err)
	}
	if got := rect.Size(); got != (clip.Size{W: 112, H: 94}) || got.W%2 != 0 || got.H%2 != 0 {
		t.Errorf("100x50 @30 = %v, want {112,94} (even)", got)
	}
	inplace, err := effect.Rotate{Degrees: 45}.ApplyVideo(solidImage(10, 10, 0))
	if err != nil {
		t.Fatalf("rotate in-place: %v", err)
	}
	if inplace.Size() != (clip.Size{W: 10, H: 10}) {
		t.Errorf("in-place size = %v, want 10x10", inplace.Size())
	}
}

func TestFadeInBlends(t *testing.T) {
	white := solidImage(2, 2, 255).WithDuration(2 * time.Second).(video.VideoClip)
	out, err := effect.FadeIn{Dur: time.Second, Color: [3]byte{0, 0, 0}}.ApplyVideo(white)
	if err != nil {
		t.Fatalf("fade in: %v", err)
	}
	dst := clip.NewFrame(2, 2, clip.RGB24)

	// t=0 is fully the fade color (black).
	if err := out.FrameInto(context.Background(), 0, dst); err != nil {
		t.Fatalf("frame: %v", err)
	}
	if dst.Pix[0] != 0 {
		t.Errorf("t=0 value = %d, want 0 (black)", dst.Pix[0])
	}
	// t>=Dur is the full clip (white).
	if err := out.FrameInto(context.Background(), time.Second, dst); err != nil {
		t.Fatalf("frame: %v", err)
	}
	if dst.Pix[0] != 255 {
		t.Errorf("t=Dur value = %d, want 255 (white)", dst.Pix[0])
	}
}

func TestBlackAndWhiteConvertsRGB(t *testing.T) {
	src := clip.NewFrame(2, 1, clip.RGB24)
	src.Pix[0], src.Pix[1], src.Pix[2] = 255, 0, 0
	src.Pix[3], src.Pix[4], src.Pix[5] = 0, 255, 0

	out, err := effect.BlackAndWhite{}.ApplyVideo(video.NewImage(src, nil))
	if err != nil {
		t.Fatalf("black and white: %v", err)
	}
	dst := clip.NewFrame(2, 1, clip.RGB24)
	if err := out.FrameInto(context.Background(), 0, dst); err != nil {
		t.Fatalf("frame: %v", err)
	}

	if dst.Pix[0] != 77 || dst.Pix[1] != 77 || dst.Pix[2] != 77 {
		t.Errorf("red grayscale = %v, want [77 77 77]", dst.Pix[:3])
	}
	if dst.Pix[3] != 149 || dst.Pix[4] != 149 || dst.Pix[5] != 149 {
		t.Errorf("green grayscale = %v, want [149 149 149]", dst.Pix[3:6])
	}
}

func TestBlackAndWhiteStart(t *testing.T) {
	src := clip.NewFrame(1, 1, clip.RGB24)
	src.Pix[0], src.Pix[1], src.Pix[2] = 255, 0, 0

	out, err := effect.BlackAndWhite{Start: time.Second}.ApplyVideo(video.NewImage(src, nil))
	if err != nil {
		t.Fatalf("black and white: %v", err)
	}
	dst := clip.NewFrame(1, 1, clip.RGB24)

	if err := out.FrameInto(context.Background(), 500*time.Millisecond, dst); err != nil {
		t.Fatalf("frame before start: %v", err)
	}
	if dst.Pix[0] != 255 || dst.Pix[1] != 0 || dst.Pix[2] != 0 {
		t.Errorf("before start = %v, want original red", dst.Pix[:3])
	}

	if err := out.FrameInto(context.Background(), time.Second, dst); err != nil {
		t.Fatalf("frame at start: %v", err)
	}
	if dst.Pix[0] != 77 || dst.Pix[1] != 77 || dst.Pix[2] != 77 {
		t.Errorf("at start = %v, want grayscale red", dst.Pix[:3])
	}
}

func TestWaterDropUsesVideoBackground(t *testing.T) {
	src := clip.NewFrame(9, 9, clip.RGB24)
	for y := 0; y < src.H; y++ {
		for x := 0; x < src.W; x++ {
			off := y*src.Stride + x*3
			src.Pix[off], src.Pix[off+1], src.Pix[off+2] = byte(x*20), byte(y*20), 100
		}
	}
	out, err := (effect.WaterDrop{
		Dur:        time.Second,
		Amplitude:  2,
		Wavelength: 4,
		Speed:      8,
		Damping:    16,
	}).ApplyVideo(video.NewImage(src, nil))
	if err != nil {
		t.Fatalf("water drop: %v", err)
	}

	dst := clip.NewFrame(9, 9, clip.RGB24)
	if err := out.FrameInto(context.Background(), 250*time.Millisecond, dst); err != nil {
		t.Fatalf("frame: %v", err)
	}
	off := 4*dst.Stride + 3*3 // one pixel left of center, inside the first wave.
	if dst.Pix[off] == src.Pix[off] && dst.Pix[off+1] == src.Pix[off+1] && dst.Pix[off+2] == src.Pix[off+2] {
		t.Errorf("active water drop left background pixel unchanged: got %v", dst.Pix[off:off+3])
	}
}

func TestWaterDropCustomOrigin(t *testing.T) {
	cases := []struct {
		name string
		drop effect.WaterDrop
		x, y int
	}{
		{
			name: "non-zero coordinates",
			drop: effect.WaterDrop{
				Dur:        time.Second,
				Amplitude:  2,
				Wavelength: 2,
				Speed:      4,
				Damping:    8,
				X:          1,
				Y:          1,
			},
			x: 1,
			y: 1,
		},
		{
			name: "explicit zero coordinates",
			drop: effect.WaterDrop{
				Dur:        time.Second,
				Amplitude:  2,
				Wavelength: 2,
				Speed:      4,
				Damping:    8,
				UseOrigin:  true,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := clip.NewFrame(9, 9, clip.RGB24)
			for i := range src.Pix {
				src.Pix[i] = 100
			}
			out, err := tc.drop.ApplyVideo(video.NewImage(src, nil))
			if err != nil {
				t.Fatalf("water drop: %v", err)
			}

			dst := clip.NewFrame(9, 9, clip.RGB24)
			if err := out.FrameInto(context.Background(), 125*time.Millisecond, dst); err != nil {
				t.Fatalf("frame: %v", err)
			}
			off := tc.y*dst.Stride + tc.x*3
			if dst.Pix[off] <= src.Pix[off] {
				t.Errorf("origin pixel = %d, want visible wave from custom origin", dst.Pix[off])
			}
		})
	}
}

func TestWaterDropBeforeStartCopiesFrame(t *testing.T) {
	src := clip.NewFrame(4, 4, clip.RGB24)
	for i := range src.Pix {
		src.Pix[i] = byte(i)
	}
	out, err := (effect.WaterDrop{Start: time.Second, Dur: time.Second}).ApplyVideo(video.NewImage(src, nil))
	if err != nil {
		t.Fatalf("water drop: %v", err)
	}

	dst := clip.NewFrame(4, 4, clip.RGB24)
	if err := out.FrameInto(context.Background(), 500*time.Millisecond, dst); err != nil {
		t.Fatalf("frame: %v", err)
	}
	for i := range src.Pix {
		if dst.Pix[i] != src.Pix[i] {
			t.Fatalf("pre-start pixel %d = %d, want %d", i, dst.Pix[i], src.Pix[i])
		}
	}
}

func TestChromaKeyCreatesMaskFromGreen(t *testing.T) {
	src := clip.NewFrame(2, 1, clip.RGB24)
	src.Pix[0], src.Pix[1], src.Pix[2] = 0, 255, 0
	src.Pix[3], src.Pix[4], src.Pix[5] = 255, 0, 0

	out, err := effect.ChromaKey{}.ApplyVideo(video.NewImage(src, nil))
	if err != nil {
		t.Fatalf("chroma key: %v", err)
	}
	if !out.HasMask() {
		t.Fatal("chroma key did not create a mask")
	}
	mask := clip.NewFrame(2, 1, clip.Gray8)
	ok, err := out.MaskInto(context.Background(), 0, mask)
	if err != nil || !ok {
		t.Fatalf("mask into: ok=%v err=%v", ok, err)
	}
	if mask.Pix[0] != 0 {
		t.Errorf("green alpha = %d, want transparent", mask.Pix[0])
	}
	if mask.Pix[1] != 255 {
		t.Errorf("red alpha = %d, want opaque", mask.Pix[1])
	}
}

func TestChromaKeyCombinesExistingMask(t *testing.T) {
	src := clip.NewFrame(1, 1, clip.RGB24)
	src.Pix[0], src.Pix[1], src.Pix[2] = 255, 0, 0
	alpha := clip.NewFrame(1, 1, clip.Gray8)
	alpha.Pix[0] = 128

	out, err := effect.ChromaKey{}.ApplyVideo(video.NewImage(src, alpha))
	if err != nil {
		t.Fatalf("chroma key: %v", err)
	}
	mask := clip.NewFrame(1, 1, clip.Gray8)
	ok, err := out.MaskInto(context.Background(), 0, mask)
	if err != nil || !ok {
		t.Fatalf("mask into: ok=%v err=%v", ok, err)
	}
	if mask.Pix[0] != 128 {
		t.Errorf("combined alpha = %d, want existing mask value", mask.Pix[0])
	}
}

func TestChromaKeyCanUseExplicitBlack(t *testing.T) {
	src := clip.NewFrame(1, 1, clip.RGB24)

	out, err := (effect.ChromaKey{Color: [3]byte{0, 0, 0}, UseColor: true}).ApplyVideo(video.NewImage(src, nil))
	if err != nil {
		t.Fatalf("chroma key: %v", err)
	}
	mask := clip.NewFrame(1, 1, clip.Gray8)
	ok, err := out.MaskInto(context.Background(), 0, mask)
	if err != nil || !ok {
		t.Fatalf("mask into: ok=%v err=%v", ok, err)
	}
	if mask.Pix[0] != 0 {
		t.Errorf("black alpha = %d, want transparent", mask.Pix[0])
	}
}

func TestChromaKeyCanUseHexColor(t *testing.T) {
	src := clip.NewFrame(2, 1, clip.RGB24)
	src.Pix[0], src.Pix[1], src.Pix[2] = 0, 0, 255
	src.Pix[3], src.Pix[4], src.Pix[5] = 255, 0, 0

	out, err := (effect.ChromaKey{Hex: "#0000ff"}).ApplyVideo(video.NewImage(src, nil))
	if err != nil {
		t.Fatalf("chroma key: %v", err)
	}
	mask := clip.NewFrame(2, 1, clip.Gray8)
	ok, err := out.MaskInto(context.Background(), 0, mask)
	if err != nil || !ok {
		t.Fatalf("mask into: ok=%v err=%v", ok, err)
	}
	if mask.Pix[0] != 0 {
		t.Errorf("blue alpha = %d, want transparent", mask.Pix[0])
	}
	if mask.Pix[1] != 255 {
		t.Errorf("red alpha = %d, want opaque", mask.Pix[1])
	}
}

func TestChromaKeyCanUseBlackHexColor(t *testing.T) {
	src := clip.NewFrame(1, 1, clip.RGB24)

	out, err := (effect.ChromaKey{Hex: "000000"}).ApplyVideo(video.NewImage(src, nil))
	if err != nil {
		t.Fatalf("chroma key: %v", err)
	}
	mask := clip.NewFrame(1, 1, clip.Gray8)
	ok, err := out.MaskInto(context.Background(), 0, mask)
	if err != nil || !ok {
		t.Fatalf("mask into: ok=%v err=%v", ok, err)
	}
	if mask.Pix[0] != 0 {
		t.Errorf("black alpha = %d, want transparent", mask.Pix[0])
	}
}

func TestChromaKeyDespillsGreenCast(t *testing.T) {
	// A foreground pixel with a green cast: green dominates the mean of red/blue.
	src := clip.NewFrame(1, 1, clip.RGB24)
	src.Pix[0], src.Pix[1], src.Pix[2] = 100, 220, 140

	out, err := effect.ChromaKey{}.ApplyVideo(video.NewImage(src, nil))
	if err != nil {
		t.Fatalf("chroma key: %v", err)
	}
	dst := clip.NewFrame(1, 1, clip.RGB24)
	if err := out.FrameInto(context.Background(), 0, dst); err != nil {
		t.Fatalf("frame into: %v", err)
	}
	// Default Spill is full, so green is pulled to the mean of red and blue.
	if want := byte((100 + 140 + 1) / 2); dst.Pix[1] != want {
		t.Errorf("despilled green = %d, want %d", dst.Pix[1], want)
	}
	if dst.Pix[0] != 100 || dst.Pix[2] != 140 {
		t.Errorf("despill touched red/blue: got (%d, %d), want (100, 140)", dst.Pix[0], dst.Pix[2])
	}
}

func TestChromaKeyShrinkAndSoftenEdge(t *testing.T) {
	// Left half green (keyed out), right half red (kept). Without spatial passes
	// the boundary is hard; ShrinkEdge pulls the red edge in, SoftenEdge feathers.
	const w = 8
	src := clip.NewFrame(w, 1, clip.RGB24)
	for x := 0; x < w; x++ {
		o := x * 3
		if x < w/2 {
			src.Pix[o+1] = 255 // green
		} else {
			src.Pix[o] = 255 // red
		}
	}

	mask := func(k effect.ChromaKey) []byte {
		out, err := k.ApplyVideo(video.NewImage(src, nil))
		if err != nil {
			t.Fatalf("chroma key: %v", err)
		}
		m := clip.NewFrame(w, 1, clip.Gray8)
		if _, err := out.MaskInto(context.Background(), 0, m); err != nil {
			t.Fatalf("mask into: %v", err)
		}
		return append([]byte(nil), m.Pix[:w]...)
	}

	hard := mask(effect.ChromaKey{})
	if hard[w/2-1] != 0 || hard[w/2] != 255 {
		t.Fatalf("hard matte not a clean step: %v", hard)
	}

	shrunk := mask(effect.ChromaKey{ShrinkEdge: 1})
	if shrunk[w/2] != 0 {
		t.Errorf("ShrinkEdge did not trim the foreground edge: %v", shrunk)
	}
	if shrunk[w-1] != 255 {
		t.Errorf("ShrinkEdge eroded the solid interior: %v", shrunk)
	}

	soft := mask(effect.ChromaKey{SoftenEdge: 1})
	if soft[w/2] == 0 || soft[w/2] == 255 {
		t.Errorf("SoftenEdge did not feather the boundary: %v", soft)
	}
}

func TestChromaKeyClipWhiteHardensMatte(t *testing.T) {
	// A pixel sitting in the blend band yields partial alpha by default; a high
	// ClipWhite remaps that partial value up toward opaque.
	src := clip.NewFrame(1, 1, clip.RGB24)
	src.Pix[0], src.Pix[1], src.Pix[2] = 90, 150, 90

	maskAt := func(k effect.ChromaKey) byte {
		out, err := k.ApplyVideo(video.NewImage(src, nil))
		if err != nil {
			t.Fatalf("chroma key: %v", err)
		}
		m := clip.NewFrame(1, 1, clip.Gray8)
		if _, err := out.MaskInto(context.Background(), 0, m); err != nil {
			t.Fatalf("mask into: %v", err)
		}
		return m.Pix[0]
	}

	base := maskAt(effect.ChromaKey{Blend: 0.5})
	if base == 0 || base == 255 {
		t.Fatalf("test pixel not in the blend band: alpha = %d", base)
	}
	hardened := maskAt(effect.ChromaKey{Blend: 0.5, ClipWhite: 0.5})
	if hardened <= base {
		t.Errorf("ClipWhite did not push the matte toward opaque: base=%d hardened=%d", base, hardened)
	}
}

func TestFadeOutRequiresDuration(t *testing.T) {
	noDur := solidImage(2, 2, 255) // ImageNode without a duration
	if _, err := (effect.FadeOut{Dur: time.Second}).ApplyVideo(noDur); !errors.Is(err, clip.ErrNoDuration) {
		t.Errorf("err = %v, want ErrNoDuration", err)
	}
}

func TestSpeedAndLoopDuration(t *testing.T) {
	base := solidImage(2, 2, 0).WithDuration(4 * time.Second).(video.VideoClip)

	fast, err := effect.MultiplySpeed{Factor: 2}.ApplyVideo(base)
	if err != nil {
		t.Fatalf("speed: %v", err)
	}
	if d := fast.Duration(); d != 2*time.Second {
		t.Errorf("speed duration = %v, want 2s", d)
	}

	looped, err := effect.Loop{N: 3}.ApplyVideo(base)
	if err != nil {
		t.Fatalf("loop: %v", err)
	}
	if d := looped.Duration(); d != 12*time.Second {
		t.Errorf("loop duration = %v, want 12s", d)
	}
	if looped.SourceAccess() != video.AccessRandom {
		t.Errorf("loop access = %v, want random", looped.SourceAccess())
	}
}

func TestEffectValidationErrors(t *testing.T) {
	base := solidImage(8, 8, 0).WithDuration(time.Second).(video.VideoClip)
	cases := []struct {
		name string
		eff  effect.VideoEffect
		want error
	}{
		{"resize zero scale", effect.Resize{Scale: 0}, effect.ErrInvalidResize},
		{"resize negative scale", effect.Resize{Scale: -1}, effect.ErrInvalidResize},
		{"speed zero", effect.MultiplySpeed{Factor: 0}, effect.ErrInvalidFactor},
		{"speed negative", effect.MultiplySpeed{Factor: -2}, effect.ErrInvalidFactor},
		{"loop zero", effect.Loop{N: 0}, effect.ErrInvalidLoopCount},
		{"loop negative", effect.Loop{N: -3}, effect.ErrInvalidLoopCount},
		{"chroma key similarity", effect.ChromaKey{Similarity: 2}, effect.ErrInvalidChromaKey},
		{"chroma key spill", effect.ChromaKey{Spill: 2}, effect.ErrInvalidChromaKey},
		{"chroma key shrink negative", effect.ChromaKey{ShrinkEdge: -1}, effect.ErrInvalidChromaKey},
		{"chroma key clip inverted", effect.ChromaKey{ClipBlack: 0.8, ClipWhite: 0.2}, effect.ErrInvalidChromaKey},
		{"chroma key hex length", effect.ChromaKey{Hex: "#0f0"}, effect.ErrInvalidChromaKeyColor},
		{"chroma key hex digit", effect.ChromaKey{Hex: "#00xx00"}, effect.ErrInvalidChromaKeyColor},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := c.eff.ApplyVideo(base); !errors.Is(err, c.want) {
				t.Errorf("err = %v, want %v", err, c.want)
			}
		})
	}
}

// TestCropGolden pins the exact pixel mapping of a crop (a deterministic byte
// move, no resampling).
func TestCropGolden(t *testing.T) {
	src := clip.NewFrame(4, 4, clip.RGB24)
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			off := y*src.Stride + x*3
			src.Pix[off], src.Pix[off+1] = byte(x), byte(y)
		}
	}
	out, err := effect.Crop{X: 1, Y: 2, W: 2, H: 1}.ApplyVideo(video.NewImage(src, nil))
	if err != nil {
		t.Fatalf("crop: %v", err)
	}
	dst := clip.NewFrame(2, 1, clip.RGB24)
	if err := out.FrameInto(context.Background(), 0, dst); err != nil {
		t.Fatalf("frame: %v", err)
	}
	// dst(0,0) = src(1,2); dst(1,0) = src(2,2).
	if dst.Pix[0] != 1 || dst.Pix[1] != 2 || dst.Pix[3] != 2 || dst.Pix[4] != 2 {
		t.Errorf("crop golden = %v, want x,y of (1,2),(2,2)", dst.Pix[:6])
	}
}

// TestRotateGolden pins the 180-degree pixel mapping (last source pixel becomes
// the first destination pixel).
func TestRotateGolden(t *testing.T) {
	src := clip.NewFrame(2, 2, clip.RGB24)
	for i := range src.Pix {
		src.Pix[i] = byte(i + 1)
	}
	out, err := effect.Rotate{Degrees: 180}.ApplyVideo(video.NewImage(src, nil))
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}
	dst := clip.NewFrame(2, 2, clip.RGB24)
	if err := out.FrameInto(context.Background(), 0, dst); err != nil {
		t.Fatalf("frame: %v", err)
	}
	last := len(src.Pix) - 3
	if dst.Pix[0] != src.Pix[last] {
		t.Errorf("rotate-180 golden first pixel = %d, want source last %d", dst.Pix[0], src.Pix[last])
	}
}

// TestFadeInGolden pins the integer blend at the midpoint of the fade.
func TestFadeInGolden(t *testing.T) {
	white := solidImage(1, 1, 200).WithDuration(2 * time.Second).(video.VideoClip)
	out, err := effect.FadeIn{Dur: time.Second, Color: [3]byte{0, 0, 0}}.ApplyVideo(white)
	if err != nil {
		t.Fatalf("fade: %v", err)
	}
	dst := clip.NewFrame(1, 1, clip.RGB24)
	// At t=Dur/2 the factor is 0.5: out = round(200*128/255) = 100.
	if err := out.FrameInto(context.Background(), 500*time.Millisecond, dst); err != nil {
		t.Fatalf("frame: %v", err)
	}
	if dst.Pix[0] != 100 {
		t.Errorf("fade midpoint = %d, want 100", dst.Pix[0])
	}
}

// TestResizeGolden pins an upscale of a uniform source: every output pixel keeps
// the source value regardless of the CatmullRom kernel.
func TestResizeGolden(t *testing.T) {
	out, err := effect.Resize{Width: 4, Height: 4}.ApplyVideo(solidImage(2, 2, 77))
	if err != nil {
		t.Fatalf("resize: %v", err)
	}
	dst := clip.NewFrame(4, 4, clip.RGB24)
	if err := out.FrameInto(context.Background(), 0, dst); err != nil {
		t.Fatalf("frame: %v", err)
	}
	for i, b := range dst.Pix {
		if b != 77 {
			t.Fatalf("resized uniform pixel %d = %d, want 77", i, b)
		}
	}
}

// TestTransformNodeConcurrentRenderInto exercises the parallel-safety claim: a
// lazy TransformNode (from resizing a non-static clip) must render concurrently
// for different t with no shared mutable scratch. Run under -race.
func TestTransformNodeConcurrentRenderInto(t *testing.T) {
	in := &fakeVideo{size: clip.Size{W: 16, H: 16}, fill: 100, access: video.AccessLinear}
	out, err := effect.Resize{Width: 8, Height: 8}.ApplyVideo(in)
	if err != nil {
		t.Fatalf("resize: %v", err)
	}

	const workers = 8
	var wg sync.WaitGroup
	errs := make([]error, workers)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			dst := clip.NewFrame(8, 8, clip.RGB24)
			for i := 0; i < 50; i++ {
				if _, err := out.RenderInto(context.Background(), time.Duration(w*i)*time.Millisecond, dst, nil); err != nil {
					errs[w] = err
					return
				}
				for _, b := range dst.Pix {
					if b != 100 {
						errs[w] = errInvalidPixel
						return
					}
				}
			}
		}(w)
	}
	wg.Wait()
	for w, err := range errs {
		if err != nil {
			t.Fatalf("worker %d: %v", w, err)
		}
	}
}

var errInvalidPixel = errors.New("unexpected pixel value")

func TestChainAppliesInOrder(t *testing.T) {
	base := solidImage(8, 8, 100).WithDuration(time.Second).(video.VideoClip)
	out, err := effect.Chain(base,
		effect.Resize{Scale: 0.5},
		effect.Crop{X: 0, Y: 0, W: 2, H: 2},
	)
	if err != nil {
		t.Fatalf("chain: %v", err)
	}
	if out.Size() != (clip.Size{W: 2, H: 2}) {
		t.Errorf("size = %v, want 2x2 after resize+crop", out.Size())
	}
}
