package blend

// Blend1 blends one channel: fg*a + bg*(255-a), divided by 255 with rounding.
// a is the source opacity in 0..255. The (t + (t>>8))>>8 trick rounds the
// divide-by-255 without a real division.
func Blend1(fg, bg, a byte) byte {
	t := int(fg)*int(a) + int(bg)*(255-int(a)) + 128
	return byte((t + (t >> 8)) >> 8)
}

// CopyRGBRow copies n packed RGB24 pixels from src into dst (path a, and the
// pixel half of path b).
func CopyRGBRow(dst, src []byte, n int) {
	copy(dst[:n*3], src[:n*3])
}

// LerpRow cross-fades two packed rows into dst: dst = b*t + a*(255-t) per byte,
// with t the 0..255 weight toward b. It is the time-domain analogue of the
// spatial "over" kernels — the per-row blend a crossfade transition is built on
// — and works for both RGB24 and Gray8 rows since it operates per byte. n is the
// byte count (pixels*bpp).
func LerpRow(dst, a, b []byte, n int, t byte) {
	switch t {
	case 0:
		copy(dst[:n], a[:n])
	case 255:
		copy(dst[:n], b[:n])
	default:
		for i := 0; i < n; i++ {
			dst[i] = Blend1(b[i], a[i], t)
		}
	}
}

// FillAlphaRow sets n Gray8 alpha bytes to 255, marking the region the source
// covered as fully opaque (path b).
func FillAlphaRow(dst []byte, n int) {
	for i := 0; i < n; i++ {
		dst[i] = 255
	}
}

// OverOpaqueRow composites a masked source over an opaque destination (path c):
// dst = fg*a + bg*(255-a), per channel. dst and fg are RGB24 rows of n pixels;
// a is the source's Gray8 alpha row. The destination stays opaque (no alpha).
func OverOpaqueRow(dst, fg, a []byte, n int) {
	for x := 0; x < n; x++ {
		o := x * 3
		av := a[x]
		dst[o] = Blend1(fg[o], dst[o], av)
		dst[o+1] = Blend1(fg[o+1], dst[o+1], av)
		dst[o+2] = Blend1(fg[o+2], dst[o+2], av)
	}
}

// OverRow composites a masked source over a masked destination (path d), the
// "over" operator with premultiplied recovery:
//
//	final_a = a + a_bg*(1-a)
//	out     = (fg*a + bg*a_bg*(1-a)) / final_a      (final_a==0 -> transparent)
//
// All alpha is integer 0..255. dstRGB/fgRGB are RGB24 rows; aDst is the
// destination Gray8 alpha (updated in place to final_a); aSrc is the source
// Gray8 alpha. n is the pixel count. The division is rounded so the result
// stays within 1 of the float64 reference.
func OverRow(dstRGB, fgRGB, aSrc, aDst []byte, n int) {
	for x := 0; x < n; x++ {
		a := int(aSrc[x])
		abg := int(aDst[x])
		// a_bg*(1-a) scaled to 0..255, rounded.
		inner := (abg*(255-a) + 127) / 255
		finalA := a + inner
		if finalA <= 0 {
			// Fully transparent pixel: leave RGB as-is, clear alpha.
			aDst[x] = 0
			continue
		}
		o := x * 3
		half := finalA / 2
		for c := 0; c < 3; c++ {
			num := int(fgRGB[o+c])*a + int(dstRGB[o+c])*inner
			dstRGB[o+c] = byte((num + half) / finalA)
		}
		if finalA > 255 {
			finalA = 255
		}
		aDst[x] = byte(finalA)
	}
}
