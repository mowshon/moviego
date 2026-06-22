package effect_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/mowshon/moviego/v2/clip"
	"github.com/mowshon/moviego/v2/ease"
	"github.com/mowshon/moviego/v2/effect"
	"github.com/mowshon/moviego/v2/keyframe"
	"github.com/mowshon/moviego/v2/video"
)

func fadeTrack() keyframe.Track {
	return keyframe.New([]keyframe.Key{{At: 0, Val: 0}, {At: time.Second, Val: 1}}, ease.Linear)
}

// renderAlpha renders the clip at t and returns its RGB and (optional) alpha.
func renderAlpha(t *testing.T, c video.VideoClip, at clip.Time) (rgb, alpha *clip.Frame) {
	t.Helper()
	size := c.Size()
	rgb = clip.NewFrame(size.W, size.H, clip.RGB24)
	if c.HasMask() {
		alpha = clip.NewFrame(size.W, size.H, clip.Gray8)
	}
	if _, err := c.RenderInto(context.Background(), at, rgb, alpha); err != nil {
		t.Fatalf("render at %v: %v", at, err)
	}
	return rgb, alpha
}

func TestOpacityEmptyTrackErrors(t *testing.T) {
	src := &fakeVideo{size: clip.Size{W: 2, H: 2}, dur: time.Second, hasDur: true, fill: 100}
	if _, err := (effect.Opacity{}).ApplyVideo(src); !errors.Is(err, effect.ErrNoKeyframes) {
		t.Errorf("err = %v, want ErrNoKeyframes", err)
	}
}

func TestOpacitySynthesizesMask(t *testing.T) {
	src := &fakeVideo{size: clip.Size{W: 4, H: 4}, dur: time.Second, hasDur: true, fill: 100}
	out, err := (effect.Opacity{Track: fadeTrack()}).ApplyVideo(src)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if !out.HasMask() {
		t.Fatal("opacity clip must report a mask")
	}
	cases := []struct {
		at   clip.Time
		want byte
	}{
		{0, 0},
		{500 * time.Millisecond, 128},
		{time.Second, 255},
	}
	for _, c := range cases {
		rgb, alpha := renderAlpha(t, out, c.at)
		if rgb.Pix[0] != 100 {
			t.Errorf("at %v: RGB altered = %d, want 100", c.at, rgb.Pix[0])
		}
		if got := alpha.Pix[0]; got != c.want {
			t.Errorf("at %v: alpha = %d, want %d", c.at, got, c.want)
		}
	}
}

func TestOpacityScalesExistingMask(t *testing.T) {
	// An inner clip with alpha 200 at half opacity → 200*128/255 ≈ 100.
	src := &fakeVideo{size: clip.Size{W: 4, H: 4}, dur: time.Second, hasDur: true, fill: 50, mask: true}
	out, _ := (effect.Opacity{Track: fadeTrack()}).ApplyVideo(src)
	_, alpha := renderAlpha(t, out, 500*time.Millisecond)
	if got := alpha.Pix[0]; got < 99 || got > 101 {
		t.Errorf("scaled alpha = %d, want ~100", got)
	}
}

func TestScalePreservesSize(t *testing.T) {
	src := &fakeVideo{size: clip.Size{W: 8, H: 8}, dur: time.Second, hasDur: true, fill: 255}
	out, err := (effect.Scale{Track: keyframe.New([]keyframe.Key{{At: 0, Val: 0.5}}, nil)}).ApplyVideo(src)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if got := out.Size(); got.W != 8 || got.H != 8 {
		t.Errorf("size = %v, want 8x8", got)
	}
}

func TestScaleZoomOutLetterboxes(t *testing.T) {
	// A white clip at factor 0.5 shrinks to a centered 4x4 white block on an
	// 8x8 frame: corners are black, the center stays white.
	src := &fakeVideo{size: clip.Size{W: 8, H: 8}, dur: time.Second, hasDur: true, fill: 255}
	track := keyframe.New([]keyframe.Key{{At: 0, Val: 0.5}}, nil)
	out, _ := (effect.Scale{Track: track}).ApplyVideo(src)
	rgb, _ := renderAlpha(t, out, 0)
	corner := rgb.Pix[0] // pixel (0,0)
	center := rgb.Pix[4*rgb.Stride+4*3]
	if corner != 0 {
		t.Errorf("corner = %d, want 0 (letterbox)", corner)
	}
	if center != 255 {
		t.Errorf("center = %d, want 255 (content)", center)
	}
}

func TestScaleFactorOneIsIdentity(t *testing.T) {
	src := &fakeVideo{size: clip.Size{W: 4, H: 4}, dur: time.Second, hasDur: true, fill: 123}
	out, _ := (effect.Scale{Track: keyframe.New([]keyframe.Key{{At: 0, Val: 1}}, nil)}).ApplyVideo(src)
	rgb, _ := renderAlpha(t, out, 0)
	for i, v := range rgb.Pix {
		if v != 123 {
			t.Fatalf("identity pix[%d] = %d, want 123", i, v)
		}
	}
}
