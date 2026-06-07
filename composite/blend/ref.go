package blend

// This file is the float64 reference for the integer kernels in blend.go. It is
// the source of truth the tolerance tests compare against; it is intentionally
// simple and never used on the hot path.

// RefOverOpaque returns fg*a + bg*(255-a) for one channel in float64
// (path c). a is the source opacity in 0..255.
func RefOverOpaque(fg, bg, a byte) float64 {
	af := float64(a) / 255
	return float64(fg)*af + float64(bg)*(1-af)
}

// RefOver returns the float64 "over" result for one pixel (path d): the final
// alpha (0..255) and the three recovered RGB channels (0..255). When the final
// alpha is zero the RGB is undefined and returned as zero.
func RefOver(fg [3]byte, bg [3]byte, aSrc, aDst byte) (finalA float64, rgb [3]float64) {
	a := float64(aSrc) / 255
	abg := float64(aDst) / 255
	fa := a + abg*(1-a)
	finalA = fa * 255
	if fa == 0 {
		return 0, [3]float64{}
	}
	for c := 0; c < 3; c++ {
		num := float64(fg[c])*a + float64(bg[c])*abg*(1-a)
		rgb[c] = num / fa
	}
	return finalA, rgb
}
