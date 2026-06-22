# Effects

An effect is a value type that transforms a clip into a new clip. Video effects
implement `effect.VideoEffect` (`ApplyVideo` + `Targets`) and are listed below.
Effects never mutate a clip — they wrap it in a new graph node and return a new
handle.

Audio processing uses the `(*Audio)` methods (`Volume`, `FadeIn`, `Speed`, …);
the `effect.AudioEffect` interface and `(*Audio).Fx` let you plug in a custom
audio effect the same way.

## Two ways to apply an effect

**1. A named facade method**, for the common one-liners:

```go
v := clip.Resize(0.5).FadeIn(time.Second).FlipH()
```

**2. `.Fx(effect.X{...})`**, which works for every effect and lets you set its
tunable fields. Use it when there is no facade method, or when you need a field
the method does not expose:

```go
import "github.com/mowshon/moviego/v2/effect"

v := clip.
    Fx(effect.Blur{Radius: 8}).
    Fx(effect.ChromaKey{Hex: "#00FF00", Similarity: 0.3})
```

Many fields have useful zero-value defaults, so `effect.ChromaKey{}` and
`effect.WaterDrop{}` are valid. Some zero values are invalid because they would
produce an undefined graph (`Resize{Scale: 0}`, `MultiplySpeed{Factor: 0}`,
`Loop{N: 0}`); those errors are carried on the handle and surfaced by `Err()`
and `WriteVideo`.

## Sidecar targets

`Targets()` reports which sidecars an effect transforms:

- **Video** — the RGB frames (every effect).
- **Mask** — the transparency sidecar, kept in sync (geometric and reframe ops).
- **Audio** — the audio sidecar (time effects like speed and loop).

This is the contract verified by the sidecar-propagation matrix
(`docs/sidecar-propagation.md`).

## When an effect starts

Same-size effects that have a `Start clip.Time` field apply only from that time
onward; before it, each frame (and its mask) passes through unchanged. The zero
value applies from the beginning. `BlackAndWhite`, `Blur`, `Sharpen`, `FlipH`,
`FlipV`, `ChromaKey`, and `WaterDrop` all support `Start`.

Size-changing effects (`Resize`, `Crop`, `Rotate`, `Pad`, `FitTo`) have **no**
`Start`: a clip node reports a single `Size()` for its whole timeline, so it
cannot be one size before a time and another after. To change size for only part
of a clip, split it (`Subclip`) and `Concat` the pieces.

---

## Video effects

### Geometry (size-changing)

| Effect | Facade | Fields | Targets | Notes |
|---|---|---|---|---|
| `Resize` | `Resize(scale)`, `ResizeTo(w,h)` | `Scale float64`, `Width int`, `Height int` | Video, Mask | Set `Scale` for a uniform factor, or `Width`/`Height` for a target size; one of W/H preserves aspect. At least one target must be positive, otherwise `ErrInvalidResize`. |
| `Crop` | `Crop(x,y,w,h)` | `X, Y, W, H int` | Video, Mask | Extracts a rectangle, clamped to the clip bounds. Non-positive `W`/`H` means "to the source edge"; an origin past the edge clamps to a 1px window. |
| `Rotate` | `Rotate(degrees)` | `Degrees float64`, `Expand bool` | Video, Mask | Clockwise by any angle. Exact multiples of 90° take the lossless transpose (90/270 swap W and H). Other angles sample bilinearly; `Expand` grows the canvas to the rotated bounding box (the facade sets it), otherwise the size is kept and corners are clipped. Exposed margins are black (RGB) / transparent (mask). Advertises FFmpeg `rotate` for non-quarter angles. |
| `Pad` | `Pad(w,h,col)` | `W, H int`, `Color [3]byte` | Video, Mask | Centers the clip on a `W×H` canvas filled with `Color` (letterbox/pillarbox). Target must be ≥ source on each axis, otherwise `ErrInvalidPad`. Mask margins are transparent. |
| `FitTo` | `FitTo(w,h)` | `W, H int`, `Color [3]byte` | Video, Mask | Scales to fit within `W×H` preserving aspect, then letterboxes to exactly `W×H`. The facade uses black bars; use `.Fx(effect.FitTo{..., Color: ...})` for another color. `W` and `H` must be positive. |

```go
clip.Crop(0, 0, 1280, 720)
clip.Fx(effect.Pad{W: 1080, H: 1080, Color: [3]byte{0, 0, 0}})
clip.FitTo(1080, 1920)   // landscape -> vertical with bars
```

### Reframe (same-size)

| Effect | Facade | Fields | Targets | Notes |
|---|---|---|---|---|
| `FlipH` | `FlipH(start...)` | `Start clip.Time` | Video, Mask | Mirror left-to-right. Advertises FFmpeg `hflip` for fusion (when `Start` is zero). |
| `FlipV` | `FlipV(start...)` | `Start clip.Time` | Video, Mask | Mirror top-to-bottom. Advertises FFmpeg `vflip` (when `Start` is zero). |

```go
clip.FlipH()
clip.FlipH(2 * time.Second)        // mirror only from 2s onward
clip.Fx(effect.FlipV{Start: time.Second})
```

### Color

All color ops transform only RGB (mask and audio untouched) and accept a
`Start` time. The per-channel ops (Brightness/Contrast/Gamma/Invert/ColorBalance)
precompute a lookup table once, so each frame is table reads with no float math
in the hot loop; `Saturation` and `HSL` mix channels per pixel. A zero value is
a passthrough wherever a no-op is meaningful, so these compose freely.

| Effect | Facade | Fields | Targets | Notes |
|---|---|---|---|---|
| `BlackAndWhite` | `BlackAndWhite(start...)` | `Start clip.Time` | Video | Grayscale from `Start` onward (default from the beginning). |
| `Grayscale` | `Grayscale(start...)` | `Start clip.Time` | Video | Canonical name for `BlackAndWhite`; identical conversion. |
| `Brightness` | `Brightness(delta, start...)` | `Delta float64`, `Start` | Video | Adds `Delta` (normalized `[-1,1]`) to every channel. `Delta == 0` is a passthrough. Advertises FFmpeg `eq`. |
| `Contrast` | `Contrast(amount, start...)` | `Amount float64`, `Start` | Video | Scales around mid-gray; `1.0` = unchanged, `<= 0` is a passthrough. Advertises `eq`. |
| `Saturation` | `Saturation(amount, start...)` | `Amount float64`, `Start` | Video | Scales distance from luma; `1.0` = unchanged, `<1` desaturates, `>1` more vivid. `<= 0` is a passthrough (not gray — use `Grayscale`). Advertises `eq`. |
| `Gamma` | `Gamma(value, start...)` | `Value float64`, `Start` | Video | Power curve `in^(1/Value)`; `> 1` brightens midtones. `<= 0` or `1.0` is a passthrough. Advertises `eq`. |
| `Invert` | `Invert(start...)` | `Start` | Video | Photographic negative. Go-only. |
| `ColorBalance` | `.Fx(...)` | `Shadows, Mids, Highlights [3]float64`, `Start` | Video | Per-range RGB shift in `[-1,1]` (triangular tonal weighting). All-zero is a passthrough. Advertises `colorbalance`. |
| `HSL` | `.Fx(...)` | `Hue float64` (deg), `Sat, Light float64`, `Start` | Video | Rotate hue, scale saturation by `1+Sat`, shift lightness by `Light`. Zero value is identity. Advertises `hue` (Go-only when `Light != 0`). |
| `LUT` | `LUT(path)` | `Path string`, `Start` | Video | Trilinear 3D LUT from a Resolve/Adobe `.cube` file. A parse error is carried on the handle. Advertises `lut3d`. |
| `Vignette` | `Vignette(amount, start...)` | `Amount, Radius float64`, `Start` | Video | Darkens toward the corners; `Amount` `[0,1]` is corner strength (`<= 0` passthrough), `Radius` is where falloff begins (default `0.5`). Go-only. |

```go
clip.Brightness(0.05).Contrast(1.15).Saturation(1.2).Gamma(1.1)
clip.Grayscale()
clip.LUT("film.cube")
clip.Fx(effect.ColorBalance{Mids: [3]float64{0.05, 0, -0.05}})
clip.Fx(effect.HSL{Hue: 15, Sat: 0.2})
```

### Blur / sharpen

| Effect | Facade | Fields | Targets | Notes |
|---|---|---|---|---|
| `Blur` | `Blur(radius, start...)` | `Radius float64`, `Start clip.Time` | Video, Mask | Down/up-resample blur; the cheap path, larger radius is softer. `Radius <= 0` is a passthrough. Go-only. |
| `GaussianBlur` | `GaussianBlur(radius, start...)` | `Radius float64`, `Start clip.Time` | Video, Mask | True separable Gaussian (two 1D passes, `O(2r)`); `Radius` is the sigma in pixels. Higher quality than `Blur`. `Radius <= 0` is a passthrough. Advertises FFmpeg `gblur`. |
| `MotionBlur` | `.Fx(...)` | `Angle float64` (deg), `Distance float64`, `Start clip.Time` | Video, Mask | Directional smear of `Distance` pixels along `Angle`. `Distance <= 0` is a passthrough. Go-only. |
| `Sharpen` | `Sharpen(amount, start...)` | `Amount float64`, `Start clip.Time` | Video | Unsharp mask using a fixed blur radius of 2px; larger amount sharpens more. `Amount <= 0` is a passthrough. Mask is untouched. Go-only. |

```go
clip.Blur(6)                       // cheap resample blur
clip.GaussianBlur(4)               // higher-quality separable Gaussian
clip.Sharpen(1.5)
clip.Fx(effect.MotionBlur{Angle: 90, Distance: 12})
```

### Fades

| Effect | Facade | Fields | Targets | Notes |
|---|---|---|---|---|
| `FadeIn` | `FadeIn(d)` | `Dur clip.Time`, `Color [3]byte` | Video | Ramps up from `Color` (default black) over `Dur`. `Dur <= 0` is a passthrough. Use `.Fx(effect.FadeIn{..., Color: ...})` for non-black. |
| `FadeOut` | `FadeOut(d)` | `Dur clip.Time`, `Color [3]byte` | Video | Ramps down to `Color` over the last `Dur`. Needs a known duration (`clip.ErrNoDuration` otherwise). `Dur <= 0` is a passthrough. Use `.Fx(effect.FadeOut{..., Color: ...})` for non-black. |

```go
clip.FadeIn(time.Second).FadeOut(time.Second)
clip.Fx(effect.FadeIn{Dur: time.Second, Color: [3]byte{255, 255, 255}})  // fade from white
```

### Chroma key (green screen)

`ChromaKey` removes a background color, writing a mask so the subject can be
composited. `ChromaKey{}` defaults to green-screen removal.

| Field | Meaning |
|---|---|
| `Color [3]byte` | Key color. Ignored when `Hex` is set. If `Color` is `[0,0,0]` and `UseColor` is false, the default key is green (`[0,255,0]`). |
| `Hex string` | Key color as `RRGGBB` or `#RRGGBB`; takes precedence over `Color`. Invalid strings return `ErrInvalidChromaKeyColor`. |
| `UseColor bool` | Makes `Color` explicit even when it is `[0,0,0]`, useful for black-keying. |
| `Similarity float64` | How close a pixel must be to the key to be fully transparent. Range `[0,1]`; zero defaults to `0.10`. |
| `Blend float64` | Softness of the key edge / partial-alpha band. Range `[0,1]`; zero defaults to `0.08`. |
| `Spill float64` | Suppresses color spill on the subject's edges. Range `[0,1]`; zero defaults to `1.0` (on). |
| `ClipBlack`, `ClipWhite float64` | Remap/clamp the matte after keying. Both must be in `[0,1]` and `ClipBlack < ClipWhite`; `ClipWhite` zero defaults to `1`. |
| `ShrinkEdge float64` | Erodes the matte inward by roughly this many pixels. Must be `>= 0`; zero is off. |
| `SoftenEdge float64` | Feathers the matte by roughly this many pixels. Must be `>= 0`; zero is off. |
| `Start clip.Time` | When keying begins. Before `Start`, RGB is unchanged and the existing mask is preserved (or treated as opaque). |

```go
keyed := clip.Fx(effect.ChromaKey{Hex: "#00FF00", Similarity: 0.3, Blend: 0.1})
out := mgo.Composite(background, keyed)
```

### Time

| Effect | Facade | Fields | Targets | Notes |
|---|---|---|---|---|
| `MultiplySpeed` | `Speed(factor)` | `Factor float64` | Video, Mask, Audio | `Factor > 1` is faster, `0 < Factor < 1` is slower. `Factor <= 0` returns `ErrInvalidFactor`; reverse is not supported here. |
| `Loop` | `Loop(n)` | `N int` | Video, Mask, Audio | Repeats the clip `N` times. `N < 1` returns `ErrInvalidLoopCount`. |

```go
clip.Speed(2.0)       // 2x faster
clip.Speed(0.5)       // half speed
clip.Loop(3)
```

### Stylized

| Effect | Facade | Fields | Targets | Notes |
|---|---|---|---|---|
| `WaterDrop` | `WaterDrop(d)` | `Start, Dur clip.Time`, `Amplitude, Wavelength, Speed, Damping float64`, `X, Y float64`, `UseOrigin bool` | Video, Mask | Expanding ripple from a point. `WaterDrop(d)` only sets `Dur`; use `.Fx(effect.WaterDrop{...})` for every other setting. |

| Field | Default / meaning |
|---|---|
| `Start clip.Time` | When the drop lands. Zero starts at the beginning. |
| `Dur clip.Time` | Active ripple duration. `Dur <= 0` defaults to `2s`. |
| `Amplitude float64` | Maximum radial displacement in pixels. Zero defaults to `8`. |
| `Wavelength float64` | Distance between wave rings in pixels. `<= 0` defaults to `24`. |
| `Speed float64` | Outward wave speed in pixels per second. `<= 0` defaults to `180`. |
| `Damping float64` | How quickly trailing waves fade with distance. `<= 0` defaults to `Wavelength * 3`. |
| `X, Y float64` | Drop landing point in pixels. Any non-zero coordinate makes the point explicit; otherwise the default is the clip center. |
| `UseOrigin bool` | Makes `(X,Y)` explicit even when both are zero, for a top-left-origin ripple. |

```go
clip.WaterDrop(2 * time.Second)
clip.Fx(effect.WaterDrop{X: 400, Y: 300, Amplitude: 12})
```

---

## Writing your own effect

### The easy path: `PixelFn`

For a per-pixel, same-size effect you only need one function. `PixelFn` supplies
the full `VideoClip` plumbing by wrapping the per-frame transform:

```go
tintRed := effect.PixelFn{
    Name: "tint-red",
    Fn: func(t clip.Time, dst, src *clip.Frame) error {
        copy(dst.Pix, src.Pix)
        for i := 0; i < len(dst.Pix); i += 3 {
            dst.Pix[i] = 255 // boost the red channel
        }
        return nil
    },
}

v := clip.Fx(tintRed)
```

| Field | Meaning |
|---|---|
| `Name string` | Label for debug output (optional). |
| `Sidecars EffectTargets` | Which sidecars the effect touches — the single source of truth. `Video` is always implied. Setting `Sidecars.Mask` is exactly what reuses `Fn` for the Gray8 alpha sidecar; otherwise the mask passes through unchanged. (Named `Sidecars`, not `Targets`, because Go cannot share a name between a field and the `Targets()` method.) |
| `Fn func(t, dst, src *clip.Frame) error` | The transform. `dst` and `src` share dimensions and format; do not retain them past the call. |
| `Start clip.Time` | When `Fn` begins applying. Before `Start`, RGB and mask frames pass through unchanged. Zero applies from the beginning. |
| `Filter func(ffmpeg.FilterContext) (ffmpeg.FilterFragment, bool)` | Optional FFmpeg-filter advertisement for fusion. Leave nil for a Go-only effect. Honored only when `Start` is zero, because partial-timeline effects do not map to one static filter. |

`PixelFn` is **same-size only**. Size-changing operations (pad, fit, crop,
resize) are not pixel-for-pixel and use the package's `geometry` helper instead —
see `Pad`/`FitTo` for the pattern.

### The full path: implement the interface

Any value implementing `VideoEffect` works with `.Fx`:

```go
type VideoEffect interface {
    ApplyVideo(video.VideoClip) (video.VideoClip, error)
    Targets() EffectTargets
}
```

Most effects in this package build their node with `video.NewTransform` (same
size) or the `geometry` helper (size-changing), and optionally attach
`TransformNode.WithFilter` to advertise an equivalent FFmpeg filter so a linear
chain can fuse into one FFmpeg invocation. Effects with no faithful filtergraph
form simply leave it unset and render on the Go path.
