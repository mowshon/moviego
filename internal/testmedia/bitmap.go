package testmedia

import "github.com/mowshon/moviego/v2/clip"

// DefaultColors maps single-character codes to RGB triples, mirroring MoviePy's
// BitmapClip palette.
var DefaultColors = map[rune][3]byte{
	'R': {255, 0, 0},
	'G': {0, 255, 0},
	'B': {0, 0, 255},
	'W': {255, 255, 255},
	'K': {0, 0, 0},
	'O': {0, 0, 0}, // alias for black
}

// BitmapFrame builds an RGB24 clip.Frame from a grid of color codes: one
// character per pixel, one string per row. colors falls back to DefaultColors
// when nil. All rows must share the first row's width; shorter rows are an
// error returned to the caller.
func BitmapFrame(rows []string, colors map[rune][3]byte) (*clip.Frame, error) {
	if colors == nil {
		colors = DefaultColors
	}
	h := len(rows)
	if h == 0 {
		return nil, clip.Wrap("bitmap frame", errEmpty)
	}
	w := len([]rune(rows[0]))
	f := clip.NewFrame(w, h, clip.RGB24)
	for y, row := range rows {
		runes := []rune(row)
		if len(runes) != w {
			return nil, clip.Wrap("bitmap frame", errRagged)
		}
		for x, r := range runes {
			rgb, ok := colors[r]
			if !ok {
				return nil, clip.Wrap("bitmap frame", errUnknownCode(r))
			}
			off := y*f.Stride + x*3
			f.Pix[off], f.Pix[off+1], f.Pix[off+2] = rgb[0], rgb[1], rgb[2]
		}
	}
	return f, nil
}
