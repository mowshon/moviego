package imagex

import "github.com/mowshon/moviego/v2/clip"

// SplitAlpha separates an RGBA frame into a new RGB24 frame and a Gray8 mask.
// The mask carries the source alpha channel (0 transparent, 255 opaque).
func SplitAlpha(rgba *clip.Frame) (rgb, alpha *clip.Frame, err error) {
	if rgba.Format != clip.RGBA {
		return nil, nil, clip.Wrap("split alpha", clip.ErrVideoCorrupted)
	}
	rgb = clip.NewFrame(rgba.W, rgba.H, clip.RGB24)
	alpha = clip.NewFrame(rgba.W, rgba.H, clip.Gray8)
	if err := SplitAlphaInto(rgb, alpha, rgba); err != nil {
		return nil, nil, err
	}
	return rgb, alpha, nil
}

// SplitAlphaInto writes the color channels of rgba into rgbDst (RGB24) and its
// alpha channel into alphaDst (Gray8). All three frames must share dimensions.
func SplitAlphaInto(rgbDst, alphaDst, rgba *clip.Frame) error {
	if rgba.Format != clip.RGBA || rgbDst.Format != clip.RGB24 || alphaDst.Format != clip.Gray8 {
		return clip.Wrap("split alpha", clip.ErrVideoCorrupted)
	}
	if rgbDst.W != rgba.W || rgbDst.H != rgba.H || alphaDst.W != rgba.W || alphaDst.H != rgba.H {
		return clip.Wrap("split alpha", clip.ErrVideoCorrupted)
	}
	for y := 0; y < rgba.H; y++ {
		s := rgba.Pix[y*rgba.Stride:]
		dRGB := rgbDst.Pix[y*rgbDst.Stride:]
		dA := alphaDst.Pix[y*alphaDst.Stride:]
		for x := 0; x < rgba.W; x++ {
			dRGB[x*3], dRGB[x*3+1], dRGB[x*3+2] = s[x*4], s[x*4+1], s[x*4+2]
			dA[x] = s[x*4+3]
		}
	}
	return nil
}

// StackAlphaInto interleaves an RGB24 color frame and a Gray8 mask into an RGBA
// destination, the inverse of SplitAlphaInto. The export feed reuses one dst
// across frames so transparent encoding never allocates per frame.
func StackAlphaInto(rgbaDst, rgb, alpha *clip.Frame) error {
	if rgbaDst.Format != clip.RGBA || rgb.Format != clip.RGB24 || alpha.Format != clip.Gray8 {
		return clip.Wrap("stack alpha", clip.ErrVideoCorrupted)
	}
	if rgbaDst.W != rgb.W || rgbaDst.H != rgb.H || alpha.W != rgb.W || alpha.H != rgb.H {
		return clip.Wrap("stack alpha", clip.ErrVideoCorrupted)
	}
	for y := 0; y < rgb.H; y++ {
		dRGBA := rgbaDst.Pix[y*rgbaDst.Stride:]
		sRGB := rgb.Pix[y*rgb.Stride:]
		sA := alpha.Pix[y*alpha.Stride:]
		for x := 0; x < rgb.W; x++ {
			dRGBA[x*4], dRGBA[x*4+1], dRGBA[x*4+2], dRGBA[x*4+3] = sRGB[x*3], sRGB[x*3+1], sRGB[x*3+2], sA[x]
		}
	}
	return nil
}
