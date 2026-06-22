package effect

import (
	"bufio"
	"errors"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/mowshon/moviego/v2/clip"
	"github.com/mowshon/moviego/v2/ffmpeg"
	"github.com/mowshon/moviego/v2/video"
)

// maxCubeSize bounds LUT_3D_SIZE so a malformed file cannot request a huge
// allocation from its declared size alone: the data buffer is size³·3 float64s,
// so 144 caps it near 72 MiB. Real-world LUTs top out around 64 per axis, with
// 144 covering even the largest film cubes.
const maxCubeSize = 144

// ErrInvalidLUT reports a .cube file that could not be parsed into a usable 3D
// table (missing or inconsistent LUT_3D_SIZE, a malformed entry, or the wrong
// number of entries).
var ErrInvalidLUT = errors.New("lut: invalid .cube file")

// LUT applies a 3D color lookup table from a Resolve/Adobe .cube file,
// trilinearly sampling it per pixel. It is the standard way to ship a film
// "look"; it advertises FFmpeg's lut3d for fusion. Only RGB is affected.
type LUT struct {
	Path  string
	Start clip.Time
}

func (l LUT) Targets() EffectTargets { return EffectTargets{Video: true} }

func (l LUT) ApplyVideo(c video.VideoClip) (video.VideoClip, error) {
	cube, err := loadCube(l.Path)
	if err != nil {
		return nil, err
	}
	fn := func(dst, src *clip.Frame) error { cube.applyInto(dst, src); return nil }
	filter := unaryFilter(func(in, out string) string { return ffmpeg.Lut3D(in, out, l.Path) })
	return videoPixelFn("lut3d", fn, l.Start, filter).ApplyVideo(c)
}

// cubeLUT is a parsed 3D LUT: a size×size×size grid of RGB output triplets with
// the input domain it maps. Entries are stored red-fastest (the .cube order).
type cubeLUT struct {
	size      int
	domainMin [3]float64
	domainMax [3]float64
	data      []float64 // size^3 * 3 floats, index ((b*size+g)*size+r)*3 + c
}

func loadCube(path string) (*cubeLUT, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return parseCube(f)
}

// parseCube reads a .cube file. It tolerates comments (#), blank lines, TITLE,
// DOMAIN_MIN/MAX and LUT_3D_SIZE in any order before the data, and rejects a
// LUT_1D_SIZE declaration with ErrInvalidLUT (only 3D tables are supported).
func parseCube(r io.Reader) (*cubeLUT, error) {
	cube := &cubeLUT{domainMax: [3]float64{1, 1, 1}}
	var data []float64
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		switch strings.ToUpper(fields[0]) {
		case "TITLE":
			continue
		case "LUT_3D_SIZE":
			if len(fields) != 2 {
				return nil, ErrInvalidLUT
			}
			n, err := strconv.Atoi(fields[1])
			if err != nil || n < 2 || n > maxCubeSize {
				return nil, ErrInvalidLUT
			}
			cube.size = n
			data = make([]float64, 0, n*n*n*3)
		case "LUT_1D_SIZE":
			return nil, ErrInvalidLUT // 1D LUTs are out of scope
		case "DOMAIN_MIN":
			if err := triple(fields[1:], &cube.domainMin); err != nil {
				return nil, err
			}
		case "DOMAIN_MAX":
			if err := triple(fields[1:], &cube.domainMax); err != nil {
				return nil, err
			}
		default:
			if len(fields) != 3 {
				return nil, ErrInvalidLUT
			}
			for _, s := range fields {
				v, err := strconv.ParseFloat(s, 64)
				if err != nil {
					return nil, ErrInvalidLUT
				}
				data = append(data, v)
			}
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if cube.size == 0 || len(data) != cube.size*cube.size*cube.size*3 {
		return nil, ErrInvalidLUT
	}
	for ch := 0; ch < 3; ch++ {
		if cube.domainMax[ch] <= cube.domainMin[ch] {
			return nil, ErrInvalidLUT
		}
	}
	cube.data = data
	return cube, nil
}

// triple parses three floats into dst.
func triple(fields []string, dst *[3]float64) error {
	if len(fields) != 3 {
		return ErrInvalidLUT
	}
	for i, s := range fields {
		v, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return ErrInvalidLUT
		}
		dst[i] = v
	}
	return nil
}

// applyInto maps every RGB pixel of src through the LUT into dst.
func (l *cubeLUT) applyInto(dst, src *clip.Frame) {
	for y := 0; y < src.H; y++ {
		s := src.Pix[y*src.Stride:]
		d := dst.Pix[y*dst.Stride:]
		for x := 0; x < src.W; x++ {
			i := x * 3
			r, g, b := l.sample(float64(s[i])/255, float64(s[i+1])/255, float64(s[i+2])/255)
			d[i] = clampByte(int(r*255 + 0.5))
			d[i+1] = clampByte(int(g*255 + 0.5))
			d[i+2] = clampByte(int(b*255 + 0.5))
		}
	}
}

// sample trilinearly interpolates the output color for a normalized input.
func (l *cubeLUT) sample(r, g, b float64) (float64, float64, float64) {
	fr := l.gridPos(r, 0)
	fg := l.gridPos(g, 1)
	fb := l.gridPos(b, 2)
	r0, g0, b0 := int(fr), int(fg), int(fb)
	r1, g1, b1 := min(r0+1, l.size-1), min(g0+1, l.size-1), min(b0+1, l.size-1)
	dr, dg, db := fr-float64(r0), fg-float64(g0), fb-float64(b0)
	var out [3]float64
	for c := 0; c < 3; c++ {
		c00 := lerp(l.at(r0, g0, b0, c), l.at(r1, g0, b0, c), dr)
		c10 := lerp(l.at(r0, g1, b0, c), l.at(r1, g1, b0, c), dr)
		c01 := lerp(l.at(r0, g0, b1, c), l.at(r1, g0, b1, c), dr)
		c11 := lerp(l.at(r0, g1, b1, c), l.at(r1, g1, b1, c), dr)
		c0 := lerp(c00, c10, dg)
		c1 := lerp(c01, c11, dg)
		out[c] = lerp(c0, c1, db)
	}
	return out[0], out[1], out[2]
}

// gridPos maps a normalized input through the domain onto a [0, size-1] grid
// coordinate, clamped to the grid.
func (l *cubeLUT) gridPos(v float64, ch int) float64 {
	t := (v - l.domainMin[ch]) / (l.domainMax[ch] - l.domainMin[ch])
	t = clamp01(t)
	return t * float64(l.size-1)
}

// at returns channel c of the grid entry at (r, g, b).
func (l *cubeLUT) at(r, g, b, c int) float64 {
	return l.data[((b*l.size+g)*l.size+r)*3+c]
}

func lerp(a, b, t float64) float64 { return a + (b-a)*t }
