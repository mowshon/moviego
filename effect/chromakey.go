package effect

import (
	"context"
	"errors"
	"math"
	"strconv"
	"strings"
	"sync"

	"github.com/mowshon/moviego/v2/audio"
	"github.com/mowshon/moviego/v2/clip"
	"github.com/mowshon/moviego/v2/video"
)

const (
	defaultChromaKeySimilarity = 0.10
	defaultChromaKeyBlend      = 0.08
	defaultChromaKeySpill      = 1.0
	chromaScale                = 256
	chromaRange                = 255 * chromaScale
)

// ErrInvalidChromaKey reports a ChromaKey parameter outside its allowed range.
var ErrInvalidChromaKey = errors.New("chroma key: Similarity, Blend, Spill, ClipBlack, ClipWhite must be in [0, 1] with ClipBlack < ClipWhite, and ShrinkEdge, SoftenEdge must be >= 0")

// ErrInvalidChromaKeyColor reports an invalid ChromaKey hex color.
var ErrInvalidChromaKeyColor = errors.New("chroma key: Hex must be RRGGBB or #RRGGBB")

// ChromaKey removes a solid-color background by turning matching pixels into a
// mask. It defaults to green screen removal; set Hex or Color for blue screen or
// any other background color. Hex takes precedence over Color.
//
// The matte is built in floating point through an After Effects-style pipeline —
// chroma key, ClipBlack/ClipWhite levels, ShrinkEdge, SoftenEdge — and quantized
// to the 8-bit mask only once at the end, so the spatial passes never compound
// banding. Despill runs over the whole foreground to strip the key color's cast
// from edges (hair) and light wrap.
//
// Similarity controls how close a pixel must be to the key color before it
// becomes fully transparent. Blend softens that edge, producing partial alpha.
// Both are normalized chroma distances; zero values choose useful defaults.
// ClipBlack/ClipWhite remap the raw matte so faint background haze clamps to
// transparent and near-solid foreground clamps to opaque. ShrinkEdge erodes the
// matte inward by N pixels to trim the key-color fringe ring; SoftenEdge blurs
// it to feather the result — both default off (zero). Spill suppresses the key
// color channel across the foreground and defaults on. The effect is exposed
// only through Fx(effect.ChromaKey{...}).
type ChromaKey struct {
	Color      [3]byte
	Hex        string
	Similarity float64
	Blend      float64
	Spill      float64
	ClipBlack  float64
	ClipWhite  float64
	ShrinkEdge float64
	SoftenEdge float64
	Start      clip.Time
	// UseColor makes Color explicit even when it is [0, 0, 0]. Without it,
	// ChromaKey{} defaults to green screen removal.
	UseColor bool
}

func (k ChromaKey) Targets() EffectTargets { return EffectTargets{Video: true, Mask: true} }

func (k ChromaKey) ApplyVideo(c video.VideoClip) (video.VideoClip, error) {
	cfg, err := k.config()
	if err != nil {
		return nil, err
	}
	return &chromaKeyNode{
		inner:     c,
		cfg:       cfg,
		scratch:   clip.NewFramePool(),
		floatPool: &sync.Pool{},
	}, nil
}

type chromaKeyConfig struct {
	start              clip.Time
	transparentDist    float64
	transparentDistSq  int64
	opaqueDistSq       int64
	edgeWidth          float64
	spill              float64
	clipBlack          float64
	clipWhite          float64
	shrinkRadius       int
	softenRadius       int
	keyCb, keyCr       int
	dominantKeyChannel int
}

func (k ChromaKey) config() (chromaKeyConfig, error) {
	if k.Similarity < 0 || k.Similarity > 1 || k.Blend < 0 || k.Blend > 1 ||
		k.Spill < 0 || k.Spill > 1 || k.ClipBlack < 0 || k.ClipBlack > 1 ||
		k.ClipWhite < 0 || k.ClipWhite > 1 || k.ShrinkEdge < 0 || k.SoftenEdge < 0 {
		return chromaKeyConfig{}, ErrInvalidChromaKey
	}
	color := k.Color
	if k.Hex != "" {
		var err error
		color, err = parseHexColor(k.Hex)
		if err != nil {
			return chromaKeyConfig{}, err
		}
	}
	if k.Hex == "" && !k.UseColor && color == [3]byte{} {
		color = [3]byte{0, 255, 0}
	}
	similarity := k.Similarity
	if similarity == 0 {
		similarity = defaultChromaKeySimilarity
	}
	blend := k.Blend
	if blend == 0 {
		blend = defaultChromaKeyBlend
	}
	spill := k.Spill
	if spill == 0 {
		spill = defaultChromaKeySpill
	}
	clipWhite := k.ClipWhite
	if clipWhite == 0 {
		clipWhite = 1
	}
	if k.ClipBlack >= clipWhite {
		return chromaKeyConfig{}, ErrInvalidChromaKey
	}
	transparentDist := similarity * chromaRange
	edgeWidth := blend * chromaRange
	opaqueDist := transparentDist + edgeWidth
	keyCb, keyCr := chromaCoords(color[0], color[1], color[2])
	return chromaKeyConfig{
		start:              k.Start,
		transparentDist:    transparentDist,
		transparentDistSq:  squareDist(transparentDist),
		opaqueDistSq:       squareDist(opaqueDist),
		edgeWidth:          edgeWidth,
		spill:              spill,
		clipBlack:          k.ClipBlack,
		clipWhite:          clipWhite,
		shrinkRadius:       int(k.ShrinkEdge + 0.5),
		softenRadius:       int(k.SoftenEdge + 0.5),
		keyCb:              keyCb,
		keyCr:              keyCr,
		dominantKeyChannel: dominantChannel(color),
	}, nil
}

type chromaKeyNode struct {
	inner     video.VideoClip
	cfg       chromaKeyConfig
	scratch   *clip.FramePool
	floatPool *sync.Pool // holds *[]float32 matte buffers, shared across With* copies
}

func (n *chromaKeyNode) Start() clip.Time    { return n.inner.Start() }
func (n *chromaKeyNode) Duration() clip.Time { return n.inner.Duration() }
func (n *chromaKeyNode) End() clip.Time      { return n.inner.End() }

func (n *chromaKeyNode) WithStart(t clip.Time) clip.Clip {
	c := *n
	c.inner = n.inner.WithStart(t).(video.VideoClip)
	return &c
}

func (n *chromaKeyNode) WithDuration(d clip.Time) clip.Clip {
	c := *n
	c.inner = n.inner.WithDuration(d).(video.VideoClip)
	return &c
}

func (n *chromaKeyNode) WithEnd(end clip.Time, changeDuration bool) clip.Clip {
	c := *n
	c.inner = n.inner.WithEnd(end, changeDuration).(video.VideoClip)
	return &c
}

func (n *chromaKeyNode) Size() clip.Size         { return n.inner.Size() }
func (n *chromaKeyNode) Rate() (clip.Rate, bool) { return n.inner.Rate() }
func (n *chromaKeyNode) HasMask() bool           { return true }
func (n *chromaKeyNode) Audio() audio.AudioClip  { return n.inner.Audio() }
func (n *chromaKeyNode) ParallelSafe() bool      { return n.inner.ParallelSafe() }
func (n *chromaKeyNode) SourceAccess() video.AccessClass {
	return n.inner.SourceAccess()
}
func (n *chromaKeyNode) Children() []video.VideoClip { return []video.VideoClip{n.inner} }

func (n *chromaKeyNode) RenderInto(ctx context.Context, t clip.Time, rgbDst, alphaDst *clip.Frame) (bool, error) {
	size := n.inner.Size()
	srcRGB := n.scratch.Get(size.W, size.H, clip.RGB24)
	defer srcRGB.Release()
	var srcAlpha *clip.Frame
	if n.inner.HasMask() {
		srcAlpha = n.scratch.Get(size.W, size.H, clip.Gray8)
		defer srcAlpha.Release()
	}
	hasAlpha, err := n.inner.RenderInto(ctx, t, srcRGB, srcAlpha)
	if err != nil {
		return false, err
	}
	copyFrameInto(rgbDst, srcRGB)
	if t < n.cfg.start {
		if alphaDst != nil {
			if hasAlpha {
				copyFrameInto(alphaDst, srcAlpha)
			} else {
				fillMask(alphaDst, 255)
			}
			return true, nil
		}
		return false, nil
	}
	if alphaDst != nil {
		n.writeMatte(alphaDst, srcRGB, srcAlpha, hasAlpha)
	}
	chromaKeyDespillInto(rgbDst, n.cfg)
	return alphaDst != nil, nil
}

func (n *chromaKeyNode) FrameInto(ctx context.Context, t clip.Time, dst *clip.Frame) error {
	size := n.inner.Size()
	srcRGB := n.scratch.Get(size.W, size.H, clip.RGB24)
	defer srcRGB.Release()
	if err := n.inner.FrameInto(ctx, t, srcRGB); err != nil {
		return err
	}
	copyFrameInto(dst, srcRGB)
	if t >= n.cfg.start {
		chromaKeyDespillInto(dst, n.cfg)
	}
	return nil
}

func (n *chromaKeyNode) MaskInto(ctx context.Context, t clip.Time, dst *clip.Frame) (bool, error) {
	size := n.inner.Size()
	if t < n.cfg.start {
		if n.inner.HasMask() {
			return n.inner.MaskInto(ctx, t, dst)
		}
		fillMask(dst, 255)
		return true, nil
	}
	srcRGB := n.scratch.Get(size.W, size.H, clip.RGB24)
	defer srcRGB.Release()
	if err := n.inner.FrameInto(ctx, t, srcRGB); err != nil {
		return false, err
	}
	var srcAlpha *clip.Frame
	hasAlpha := false
	if n.inner.HasMask() {
		srcAlpha = n.scratch.Get(size.W, size.H, clip.Gray8)
		defer srcAlpha.Release()
		var err error
		hasAlpha, err = n.inner.MaskInto(ctx, t, srcAlpha)
		if err != nil {
			return false, err
		}
	}
	n.writeMatte(dst, srcRGB, srcAlpha, hasAlpha)
	return true, nil
}

func (n *chromaKeyNode) Close() error { return nil }

// writeMatte builds the float matte, runs the optional spatial passes, and
// quantizes the result into the 8-bit destination mask.
func (n *chromaKeyNode) writeMatte(dst, rgb, alpha *clip.Frame, hasAlpha bool) {
	w, h := rgb.W, rgb.H
	mBuf := n.getFloats(w * h)
	mf := &clip.MaskF32{Val: mBuf, W: w, H: h, Stride: w}
	chromaKeyMatteF32(mf, rgb, alpha, hasAlpha, n.cfg)
	if n.cfg.shrinkRadius > 0 || n.cfg.softenRadius > 0 {
		tBuf := n.getFloats(w * h)
		tmp := &clip.MaskF32{Val: tBuf, W: w, H: h, Stride: w}
		erodeMatteF32(mf, tmp, n.cfg.shrinkRadius)
		softenMatteF32(mf, tmp, n.cfg.softenRadius)
		n.putFloats(tBuf)
	}
	quantizeMatteInto(dst, mf)
	n.putFloats(mBuf)
}

func (n *chromaKeyNode) getFloats(size int) []float32 {
	if v := n.floatPool.Get(); v != nil {
		if b := *v.(*[]float32); cap(b) >= size {
			return b[:size]
		}
	}
	return make([]float32, size)
}

func (n *chromaKeyNode) putFloats(b []float32) { n.floatPool.Put(&b) }

// chromaKeyMatteF32 fills mf with the raw key matte after ClipBlack/ClipWhite
// remapping and any incoming alpha, all in [0, 1].
func chromaKeyMatteF32(mf *clip.MaskF32, rgb, alpha *clip.Frame, hasAlpha bool, cfg chromaKeyConfig) {
	span := cfg.clipWhite - cfg.clipBlack
	for y := 0; y < rgb.H; y++ {
		rgbRow := rgb.Pix[y*rgb.Stride:]
		mRow := mf.Val[y*mf.Stride:]
		var alphaRow []byte
		if hasAlpha {
			alphaRow = alpha.Pix[y*alpha.Stride:]
		}
		for x := 0; x < rgb.W; x++ {
			o := x * 3
			a := (chromaKeyAlpha(rgbRow[o], rgbRow[o+1], rgbRow[o+2], cfg) - cfg.clipBlack) / span
			if a < 0 {
				a = 0
			} else if a > 1 {
				a = 1
			}
			if hasAlpha {
				a *= float64(alphaRow[x]) / 255
			}
			mRow[x] = float32(a)
		}
	}
}

// chromaKeyAlpha maps a pixel's chroma distance from the key color to [0, 1]:
// inside the similarity radius is transparent, beyond the blend band is opaque.
func chromaKeyAlpha(r, g, b byte, cfg chromaKeyConfig) float64 {
	cb, cr := chromaCoords(r, g, b)
	dCb, dCr := int64(cb-cfg.keyCb), int64(cr-cfg.keyCr)
	d2 := dCb*dCb + dCr*dCr
	if d2 <= cfg.transparentDistSq {
		return 0
	}
	if d2 >= cfg.opaqueDistSq || cfg.edgeWidth <= 0 {
		return 1
	}
	return (math.Sqrt(float64(d2)) - cfg.transparentDist) / cfg.edgeWidth
}

// erodeMatteF32 shrinks the foreground by a separable min filter of the given
// radius, trimming the key-color fringe ring left at hard matte edges.
func erodeMatteF32(mf, tmp *clip.MaskF32, r int) {
	if r <= 0 {
		return
	}
	w, h := mf.W, mf.H
	for y := 0; y < h; y++ {
		src := mf.Val[y*mf.Stride:]
		dst := tmp.Val[y*tmp.Stride:]
		for x := 0; x < w; x++ {
			lo, hi := clampIndex(x-r, w), clampIndex(x+r, w)
			m := src[lo]
			for i := lo + 1; i <= hi; i++ {
				if src[i] < m {
					m = src[i]
				}
			}
			dst[x] = m
		}
	}
	for x := 0; x < w; x++ {
		for y := 0; y < h; y++ {
			lo, hi := clampIndex(y-r, h), clampIndex(y+r, h)
			m := tmp.Val[lo*tmp.Stride+x]
			for i := lo + 1; i <= hi; i++ {
				if v := tmp.Val[i*tmp.Stride+x]; v < m {
					m = v
				}
			}
			mf.Val[y*mf.Stride+x] = m
		}
	}
}

// softenMatteF32 feathers the matte with a separable box blur of the given
// radius using a running sum, so cost is independent of radius.
func softenMatteF32(mf, tmp *clip.MaskF32, r int) {
	if r <= 0 {
		return
	}
	w, h := mf.W, mf.H
	win := float32(2*r + 1)
	for y := 0; y < h; y++ {
		src := mf.Val[y*mf.Stride:]
		dst := tmp.Val[y*tmp.Stride:]
		var sum float32
		for k := -r; k <= r; k++ {
			sum += src[clampIndex(k, w)]
		}
		dst[0] = sum / win
		for x := 1; x < w; x++ {
			sum += src[clampIndex(x+r, w)] - src[clampIndex(x-r-1, w)]
			dst[x] = sum / win
		}
	}
	for x := 0; x < w; x++ {
		var sum float32
		for k := -r; k <= r; k++ {
			sum += tmp.Val[clampIndex(k, h)*tmp.Stride+x]
		}
		mf.Val[x] = sum / win
		for y := 1; y < h; y++ {
			sum += tmp.Val[clampIndex(y+r, h)*tmp.Stride+x] - tmp.Val[clampIndex(y-r-1, h)*tmp.Stride+x]
			mf.Val[y*mf.Stride+x] = sum / win
		}
	}
}

func quantizeMatteInto(dst *clip.Frame, mf *clip.MaskF32) {
	for y := 0; y < dst.H; y++ {
		dRow := dst.Pix[y*dst.Stride:]
		mRow := mf.Val[y*mf.Stride:]
		for x := 0; x < dst.W; x++ {
			switch v := mRow[x]; {
			case v <= 0:
				dRow[x] = 0
			case v >= 1:
				dRow[x] = 255
			default:
				dRow[x] = byte(v*255 + 0.5)
			}
		}
	}
}

// chromaKeyDespillInto removes the key color's cast from the whole foreground by
// pulling its dominant channel down toward the mean of the other two.
func chromaKeyDespillInto(rgb *clip.Frame, cfg chromaKeyConfig) {
	if cfg.spill <= 0 || cfg.dominantKeyChannel < 0 {
		return
	}
	c := cfg.dominantKeyChannel
	o1, o2 := (c+1)%3, (c+2)%3
	for y := 0; y < rgb.H; y++ {
		row := rgb.Pix[y*rgb.Stride:]
		for x := 0; x < rgb.W; x++ {
			o := x * 3
			limit := (int(row[o+o1]) + int(row[o+o2]) + 1) / 2
			if int(row[o+c]) > limit {
				row[o+c] = byte(float64(row[o+c])*(1-cfg.spill) + float64(limit)*cfg.spill + 0.5)
			}
		}
	}
}

func clampIndex(i, n int) int {
	if i < 0 {
		return 0
	}
	if i >= n {
		return n - 1
	}
	return i
}

func chromaCoords(r, g, b byte) (cb, cr int) {
	ri, gi, bi := int(r), int(g), int(b)
	return -43*ri - 85*gi + 128*bi, 128*ri - 107*gi - 21*bi
}

func parseHexColor(s string) ([3]byte, error) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "#")
	if len(s) != 6 {
		return [3]byte{}, ErrInvalidChromaKeyColor
	}
	v, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		return [3]byte{}, ErrInvalidChromaKeyColor
	}
	return [3]byte{byte(v >> 16), byte(v >> 8), byte(v)}, nil
}

func dominantChannel(color [3]byte) int {
	ch := 0
	if color[1] > color[ch] {
		ch = 1
	}
	if color[2] > color[ch] {
		ch = 2
	}
	if color[ch] == 0 {
		return -1
	}
	return ch
}

func squareDist(v float64) int64 {
	return int64(v*v + 0.5)
}

func fillMask(dst *clip.Frame, v byte) {
	for y := 0; y < dst.H; y++ {
		row := dst.Pix[y*dst.Stride : y*dst.Stride+dst.W]
		for x := range row {
			row[x] = v
		}
	}
}
