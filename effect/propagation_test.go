package effect_test

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/mowshon/moviego/v2/clip"
	"github.com/mowshon/moviego/v2/ease"
	"github.com/mowshon/moviego/v2/effect"
	"github.com/mowshon/moviego/v2/keyframe"
	"github.com/mowshon/moviego/v2/video"
)

// TestEffectTargetsMatchMatrix pins every effect's declared sidecar targets to
// the sidecar-propagation matrix. An effect that silently changes which sidecars
// it touches fails here.
func TestEffectTargetsMatchMatrix(t *testing.T) {
	cases := []struct {
		name string
		eff  effect.VideoEffect
		want effect.EffectTargets
	}{
		{"Resize", effect.Resize{Scale: 0.5}, effect.EffectTargets{Video: true, Mask: true}},
		{"Crop", effect.Crop{W: 2, H: 2}, effect.EffectTargets{Video: true, Mask: true}},
		{"Rotate", effect.Rotate{Degrees: 90}, effect.EffectTargets{Video: true, Mask: true}},
		{"BlackAndWhite", effect.BlackAndWhite{}, effect.EffectTargets{Video: true}},
		{"FadeIn", effect.FadeIn{Dur: time.Second}, effect.EffectTargets{Video: true}},
		{"FadeOut", effect.FadeOut{Dur: time.Second}, effect.EffectTargets{Video: true}},
		{"WaterDrop", effect.WaterDrop{Dur: time.Second}, effect.EffectTargets{Video: true, Mask: true}},
		{"ChromaKey", effect.ChromaKey{}, effect.EffectTargets{Video: true, Mask: true}},
		{"MultiplySpeed", effect.MultiplySpeed{Factor: 2}, effect.EffectTargets{Video: true, Mask: true, Audio: true}},
		{"Loop", effect.Loop{N: 2}, effect.EffectTargets{Video: true, Mask: true, Audio: true}},
		// Keyframe-animated effects.
		{"Opacity", effect.Opacity{Track: keyframe.New([]keyframe.Key{{At: 0, Val: 1}}, nil)}, effect.EffectTargets{Video: true, Mask: true}},
		{"Scale", effect.Scale{Track: keyframe.New([]keyframe.Key{{At: 0, Val: 1}}, nil)}, effect.EffectTargets{Video: true, Mask: true}},
		// Color-grading and blur effects.
		{"Brightness", effect.Brightness{Delta: 0.1}, effect.EffectTargets{Video: true}},
		{"Contrast", effect.Contrast{Amount: 1.2}, effect.EffectTargets{Video: true}},
		{"Saturation", effect.Saturation{Amount: 1.2}, effect.EffectTargets{Video: true}},
		{"Gamma", effect.Gamma{Value: 1.5}, effect.EffectTargets{Video: true}},
		{"Invert", effect.Invert{}, effect.EffectTargets{Video: true}},
		{"ColorBalance", effect.ColorBalance{Mids: [3]float64{0.1, 0, 0}}, effect.EffectTargets{Video: true}},
		{"HSL", effect.HSL{Hue: 30}, effect.EffectTargets{Video: true}},
		{"Grayscale", effect.Grayscale{}, effect.EffectTargets{Video: true}},
		{"Vignette", effect.Vignette{Amount: 0.5}, effect.EffectTargets{Video: true}},
		{"GaussianBlur", effect.GaussianBlur{Radius: 2}, effect.EffectTargets{Video: true, Mask: true}},
		{"MotionBlur", effect.MotionBlur{Distance: 4}, effect.EffectTargets{Video: true, Mask: true}},
	}
	for _, c := range cases {
		if got := c.eff.Targets(); got != c.want {
			t.Errorf("%s targets = %+v, want %+v", c.name, got, c.want)
		}
	}
}

// maskedImage builds a static image with a Gray8 mask whose pixels carry value
// v, so a mask transform's effect is observable.
func maskedImage(w, h int, rgbVal, maskVal byte) *video.ImageNode {
	rgb := clip.NewFrame(w, h, clip.RGB24)
	for i := range rgb.Pix {
		rgb.Pix[i] = rgbVal
	}
	alpha := clip.NewFrame(w, h, clip.Gray8)
	for i := range alpha.Pix {
		alpha.Pix[i] = maskVal
	}
	return video.NewImage(rgb, alpha)
}

// TestResizeTransformsMask checks the matrix's Mask:true claim behaviorally: a
// resize must carry the mask through and resize it to the new dimensions.
func TestResizeTransformsMask(t *testing.T) {
	in := maskedImage(8, 8, 100, 180)
	out, err := effect.Resize{Scale: 0.5}.ApplyVideo(in)
	if err != nil {
		t.Fatalf("resize: %v", err)
	}
	if !out.HasMask() {
		t.Fatal("resized clip lost its mask")
	}
	maskDst := clip.NewFrame(4, 4, clip.Gray8)
	ok, err := out.MaskInto(context.Background(), 0, maskDst)
	if err != nil || !ok {
		t.Fatalf("mask into: ok=%v err=%v", ok, err)
	}
	if maskDst.W != 4 || maskDst.H != 4 {
		t.Errorf("mask size = %dx%d, want 4x4", maskDst.W, maskDst.H)
	}
	// A solid mask resizes to the same solid value.
	for _, b := range maskDst.Pix {
		if b != 180 {
			t.Fatalf("resized mask value = %d, want 180", b)
		}
	}
}

// TestFadeLeavesMaskUnchanged checks the matrix's Mask:false claim: a fade
// transforms only RGB and passes the mask through verbatim.
func TestFadeLeavesMaskUnchanged(t *testing.T) {
	in := maskedImage(2, 2, 255, 123).WithDuration(2 * time.Second).(video.VideoClip)
	out, err := effect.FadeIn{Dur: time.Second, Color: [3]byte{0, 0, 0}}.ApplyVideo(in)
	if err != nil {
		t.Fatalf("fade: %v", err)
	}
	maskDst := clip.NewFrame(2, 2, clip.Gray8)
	ok, err := out.MaskInto(context.Background(), 0, maskDst)
	if err != nil || !ok {
		t.Fatalf("mask into: ok=%v err=%v", ok, err)
	}
	want := bytes.Repeat([]byte{123}, 4)
	if !bytes.Equal(maskDst.Pix, want) {
		t.Errorf("fade changed the mask: %v, want all 123", maskDst.Pix)
	}
}

// TestBlackAndWhiteLeavesMaskUnchanged checks the matrix's Mask:false claim: the
// grayscale filter transforms only RGB and passes the mask through verbatim.
func TestBlackAndWhiteLeavesMaskUnchanged(t *testing.T) {
	in := maskedImage(2, 2, 255, 123)
	out, err := effect.BlackAndWhite{}.ApplyVideo(in)
	if err != nil {
		t.Fatalf("black and white: %v", err)
	}
	maskDst := clip.NewFrame(2, 2, clip.Gray8)
	ok, err := out.MaskInto(context.Background(), 0, maskDst)
	if err != nil || !ok {
		t.Fatalf("mask into: ok=%v err=%v", ok, err)
	}
	want := bytes.Repeat([]byte{123}, 4)
	if !bytes.Equal(maskDst.Pix, want) {
		t.Errorf("black and white changed the mask: %v, want all 123", maskDst.Pix)
	}
}

// TestSpeedPropagatesMaskThroughTimeMap verifies a time transform keeps the
// mask sidecar (the matrix's Mask:true for MultiplySpeed).
func TestSpeedPropagatesMaskThroughTimeMap(t *testing.T) {
	in := maskedImage(2, 2, 0, 200).WithDuration(4 * time.Second).(video.VideoClip)
	out, err := effect.MultiplySpeed{Factor: 2}.ApplyVideo(in)
	if err != nil {
		t.Fatalf("speed: %v", err)
	}
	if !out.HasMask() {
		t.Error("speed transform dropped the mask")
	}
}

// TestLoopPropagatesMask verifies Loop keeps the mask sidecar (matrix Mask:true).
func TestLoopPropagatesMask(t *testing.T) {
	in := maskedImage(2, 2, 0, 200).WithDuration(time.Second).(video.VideoClip)
	out, err := effect.Loop{N: 2}.ApplyVideo(in)
	if err != nil {
		t.Fatalf("loop: %v", err)
	}
	if !out.HasMask() {
		t.Error("loop dropped the mask")
	}
}

// gradientMaskImage builds a w x h image whose mask byte at (x,y) is y*w+x, so a
// geometric mask transform's pixel mapping is observable.
func gradientMaskImage(w, h int) *video.ImageNode {
	rgb := clip.NewFrame(w, h, clip.RGB24)
	alpha := clip.NewFrame(w, h, clip.Gray8)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			alpha.Pix[y*alpha.Stride+x] = byte(y*w + x)
		}
	}
	return video.NewImage(rgb, alpha)
}

// TestScaleTransformsMask checks the matrix's Mask:true claim for Scale: a
// zoom-out must shrink the mask and letterbox it with transparent (zero) pixels,
// matching the geometric transform applied to the RGB content.
func TestScaleTransformsMask(t *testing.T) {
	// 8x8 solid mask (all 255). At factor 0.5 the mask shrinks to 4x4 centered.
	in := maskedImage(8, 8, 0, 255)
	track := keyframe.New([]keyframe.Key{{At: 0, Val: 0.5}}, ease.Linear)
	out, err := effect.Scale{Track: track}.ApplyVideo(in)
	if err != nil {
		t.Fatalf("scale: %v", err)
	}
	if !out.HasMask() {
		t.Fatal("scaled clip lost its mask")
	}
	maskDst := clip.NewFrame(8, 8, clip.Gray8)
	ok, err := out.MaskInto(context.Background(), 0, maskDst)
	if err != nil || !ok {
		t.Fatalf("mask into: ok=%v err=%v", ok, err)
	}
	// Corner (0,0) is in the letterbox border — must be transparent.
	if maskDst.Pix[0] != 0 {
		t.Errorf("corner mask = %d, want 0 (letterbox)", maskDst.Pix[0])
	}
	// Center (4,4) is within the 4x4 content block — must be opaque.
	center := maskDst.Pix[4*maskDst.Stride+4]
	if center != 255 {
		t.Errorf("center mask = %d, want 255 (content)", center)
	}
}

// TestCropTransformsMask checks the matrix's Mask:true for Crop: the mask is
// cropped to the same window as the RGB.
func TestCropTransformsMask(t *testing.T) {
	in := gradientMaskImage(4, 4)
	out, err := effect.Crop{X: 1, Y: 1, W: 2, H: 2}.ApplyVideo(in)
	if err != nil {
		t.Fatalf("crop: %v", err)
	}
	maskDst := clip.NewFrame(2, 2, clip.Gray8)
	ok, err := out.MaskInto(context.Background(), 0, maskDst)
	if err != nil || !ok {
		t.Fatalf("mask into: ok=%v err=%v", ok, err)
	}
	if maskDst.W != 2 || maskDst.H != 2 {
		t.Fatalf("mask size = %dx%d, want 2x2", maskDst.W, maskDst.H)
	}
	// dst(0,0) maps to source (1,1) => 1*4+1 = 5.
	if maskDst.Pix[0] != 5 {
		t.Errorf("cropped mask origin = %d, want 5 (source 1,1)", maskDst.Pix[0])
	}
}

// TestRotateTransformsMask checks the matrix's Mask:true for Rotate: the mask is
// rotated identically to the RGB and its size swaps.
func TestRotateTransformsMask(t *testing.T) {
	// 2x1 mask: (0,0)=10, (1,0)=20.
	rgb := clip.NewFrame(2, 1, clip.RGB24)
	alpha := clip.NewFrame(2, 1, clip.Gray8)
	alpha.Pix[0], alpha.Pix[1] = 10, 20
	in := video.NewImage(rgb, alpha)

	out, err := effect.Rotate{Degrees: 90}.ApplyVideo(in)
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}
	if out.Size() != (clip.Size{W: 1, H: 2}) {
		t.Fatalf("rotated size = %v, want 1x2", out.Size())
	}
	maskDst := clip.NewFrame(1, 2, clip.Gray8)
	ok, err := out.MaskInto(context.Background(), 0, maskDst)
	if err != nil || !ok {
		t.Fatalf("mask into: ok=%v err=%v", ok, err)
	}
	// 90 CW: source (0,0)->dst(0,0)=10; source (1,0)->dst(0,1)=20.
	if maskDst.Pix[0] != 10 || maskDst.Pix[1*maskDst.Stride] != 20 {
		t.Errorf("rotated mask = %d,%d; want 10,20", maskDst.Pix[0], maskDst.Pix[1*maskDst.Stride])
	}
}
