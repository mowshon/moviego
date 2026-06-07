// Package blend holds the pixel-level alpha compositing kernels used by the
// compositor. It reproduces MoviePy's four compose_on blend paths plus the
// "fully opaque" shortcut, implemented as integer math over packed byte rows so
// the common path never touches float64.
//
// The four paths, selected by the compositor from the (destination-mask,
// source-mask) combination:
//
//	(a) no dst mask, no src mask  -> CopyRGBRow            (direct paste)
//	(b) src opaque, dst masked    -> CopyRGBRow + FillAlphaRow (paste, cover alpha)
//	(c) src masked, dst opaque    -> OverOpaqueRow         (fg*a + bg*(255-a))
//	(d) both masked               -> OverRow               (premultiplied "over")
//
// A pure-Go reference (ref.go) computes the same paths in float64; blend_test.go
// asserts the integer kernels stay within a per-channel error of 1 of it.
package blend
