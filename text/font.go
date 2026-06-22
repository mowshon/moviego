package text

import (
	"os"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"

	"github.com/mowshon/moviego/v2/clip"
)

// dpi fixes the rasterization resolution so a font size in points maps 1:1 to
// pixels (1pt == 1px at 72 DPI). Callers think in pixels; the layout math does
// too.
const dpi = 72

// Font is a parsed font, the unit the rasterizer keeps and reuses. The
// underlying sfnt font is safe for concurrent use, but each font.Face it
// produces is not, so faceAt builds a fresh face per call.
type Font struct {
	sfnt *opentype.Font
}

// ParseFont parses TrueType/OpenType font bytes.
func ParseFont(data []byte) (*Font, error) {
	f, err := opentype.Parse(data)
	if err != nil {
		return nil, clip.Wrap("parse font", err)
	}
	return &Font{sfnt: f}, nil
}

// LoadFont reads and parses a .ttf/.otf file.
func LoadFont(path string) (*Font, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, clip.Wrap("load font "+path, err)
	}
	f, err := opentype.Parse(data)
	if err != nil {
		return nil, clip.Wrap("load font "+path, err)
	}
	return &Font{sfnt: f}, nil
}

var (
	defaultOnce sync.Once
	defaultFont *Font
	defaultErr  error
)

// DefaultFont returns the embedded Go Regular face, parsed once and cached. It
// is the fallback when no font path is given, keeping rendering deterministic
// without a system font dependency.
func DefaultFont() (*Font, error) {
	defaultOnce.Do(func() {
		defaultFont, defaultErr = ParseFont(goregular.TTF)
	})
	return defaultFont, defaultErr
}

// faceAt builds a font.Face at the given pixel size. The face is not
// concurrency-safe, so callers create one per rasterization and discard it.
func (f *Font) faceAt(sizePx float64) (font.Face, error) {
	face, err := opentype.NewFace(f.sfnt, &opentype.FaceOptions{
		Size:    sizePx,
		DPI:     dpi,
		Hinting: font.HintingFull,
	})
	if err != nil {
		return nil, clip.Wrap("new face", err)
	}
	return face, nil
}

// resolveFont returns the font for the given path, falling back to the embedded
// default when path is empty.
func resolveFont(path string) (*Font, error) {
	if path == "" {
		return DefaultFont()
	}
	return LoadFont(path)
}
