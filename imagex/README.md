# imagex

Package `imagex` provides pure image decode, encode, resize, crop, rotate,
flip, pad, and alpha operations over `clip.Frame` buffers.

It depends only on `clip` and the Go image stack — no FFmpeg, no video graph,
no compositor — so every function is deterministic and unit-testable without
external tools.

## Operations

### Decode

```go
// From a file path. Returns RGBA if any pixel is non-opaque, RGB24 otherwise.
f, err := imagex.DecodeFile("photo.png")

// From in-memory bytes (same format selection).
f, err := imagex.DecodeBytes(data)

// From an in-memory image.Image (used by the text rasterizer).
f := imagex.FromImage(img)
```

Supported formats (via registered stdlib decoders): PNG, JPEG, GIF.

### Encode

```go
// PNG: RGBA frames keep their alpha; RGB24 and Gray8 are written opaque.
err := imagex.EncodePNG(w, f)

// JPEG: alpha is discarded. quality is 1–100.
err := imagex.EncodeJPEG(w, f, quality)

// Wrap a frame as a stdlib image.Image (a copy).
img := imagex.ToImage(f)
```

### Resize

```go
// Allocates a new frame of the target size. Preserves the source format.
dst, err := imagex.Resize(src, clip.Size{W: 1280, H: 720})

// Resize into a pre-allocated destination (W/H determine target size).
err := imagex.ResizeInto(dst, src)
```

Resize uses the **CatmullRom kernel** (`golang.org/x/image/draw`). This
differs from FFmpeg's bicubic — outputs are visually close but not
byte-identical (documented divergence; see `docs/compatibility.md`).

### Geometry

```go
// Crop: copy a W×H window starting at (x,y) from src into dst.
err := imagex.CropInto(dst, src, x, y)

// Lossless quarter turns: quarter is 0/1/2/3 (CW multiples of 90°).
// For quarter=1 or 3, dst must have src's dimensions swapped.
err := imagex.RotateInto(dst, src, quarter)

// Arbitrary-angle CW rotation with bilinear interpolation.
// dst may be larger than src (expanded canvas). col fills pixels that
// map outside the source. Prefer RotateInto for exact quarter turns.
err := imagex.RotateAngleInto(dst, src, angleRadians, [3]byte{r, g, b})

// Mirror left-to-right.
err := imagex.FlipHInto(dst, src)

// Mirror top-to-bottom.
err := imagex.FlipVInto(dst, src)

// Place src into a larger dst at (x,y), filling margins with col.
// For Gray8 masks, margins are filled 0 (transparent), not col.
err := imagex.PadInto(dst, src, x, y, [3]byte{r, g, b})
```

All geometry functions require `dst` and `src` to share the same `PixelFormat`.
They work for any of the three formats: `RGB24`, `RGBA`, `Gray8`.

### Alpha split / stack

These are the bridge between the RGBA wire format and the RGB + Gray8 sidecar
model used inside the video graph.

```go
// Split an RGBA frame into an RGB24 color frame and a Gray8 mask.
rgb, alpha, err := imagex.SplitAlpha(rgba)

// Split into pre-allocated destinations.
err := imagex.SplitAlphaInto(rgbDst, alphaDst, rgba)

// Interleave RGB24 + Gray8 back into an RGBA frame (inverse of split).
err := imagex.StackAlphaInto(rgbaDst, rgb, alpha)
```

## Format rules

| Format  | Bytes/px | Notes |
|---------|----------|-------|
| `RGB24` | 3        | Opaque color. |
| `RGBA`  | 4        | Non-premultiplied. Returned by decode when any pixel is non-opaque. |
| `Gray8` | 1        | Mask sidecar. 0 = transparent, 255 = opaque. |

Decode chooses between `RGB24` and `RGBA` automatically by scanning the alpha
channel after decode. `Gray8` is never returned by decode — it is only
produced by `SplitAlpha` and used internally for masks.

## Constraints

- `dst` and `src` must share the same `PixelFormat` for all geometry operations.
- For `CropInto`, the crop window must lie entirely within `src`.
- For `RotateInto` with quarter=1 or 3, `dst` must have `src`'s width and
  height swapped.
- For `PadInto`, `dst` must be at least as large as `src + offset`.
- For `SplitAlphaInto` / `StackAlphaInto`, all frames must share dimensions.
- In-place operation (dst == src) is not supported and produces incorrect output.
