# Chroma Key

Removing a solid-color background (green/blue screen) and turning it into a mask,
with an After Effects–style matte pipeline tuned to preserve fine edge detail
such as hair. Implemented in `effect/chromakey.go`; behavioral tests live in
`effect/effect_test.go` (`TestChromaKey*`).

## Quick start

`ChromaKey` is a `video.VideoEffect`, applied through `Fx`:

```go
import fx "github.com/mowshon/moviego/v2/effect"

green = green.Fx(fx.ChromaKey{
    Color:      [3]byte{87, 184, 0}, // measured screen color (defaults to pure green)
    Blend:      0.15,
    SoftenEdge: 1,
})

final := mgo.Composite(bg, green) // the mask makes the keyed pixels transparent
```

The effect produces transparency in the **mask sidecar**; you see the result
only once the clip is composited over another layer.

## The pipeline

The matte is built in floating point and quantized to the 8-bit mask **once at
the end**, so the spatial passes never compound banding. The stages, in order:

1. **Chroma key.** Each pixel is projected into BT.601 `Cb`/`Cr` chroma space
   (luma is discarded, so the key is brightness-independent) and its distance
   from the key color is measured. Inside the `Similarity` radius → fully
   transparent; beyond `Similarity + Blend` → fully opaque; a linear ramp
   between, which is where semi-transparent hair lives.
2. **`ClipBlack` / `ClipWhite` levels.** Remap the raw matte so faint background
   haze clamps to transparent and near-solid foreground clamps to opaque.
3. **`ShrinkEdge`** (separable min/erode) — pulls the matte inward by N pixels to
   trim the key-color fringe ring on hard edges.
4. **`SoftenEdge`** (separable box blur, running-sum so cost is independent of
   radius) — feathers the edge.
5. **Despill.** Pulls the key color's dominant channel down toward the mean of
   the other two, across the **whole foreground** — this strips the green/blue
   cast from edges, hair, and light wrap, not just the matte boundary.

## Parameters

All distances are normalized to `[0, 1]`. A zero value selects the listed
default; `ShrinkEdge`/`SoftenEdge`/`ClipBlack` are off at their zero value.

| Field | Default | Range | What it does |
| --- | --- | --- | --- |
| `Color` | `{0, 255, 0}` | RGB | Key color. Set to the *measured* average screen color for best results. |
| `Hex` | — | `RRGGBB` / `#RRGGBB` | Key color as hex; takes precedence over `Color`. |
| `UseColor` | `false` | bool | Makes `Color` explicit even when it is `{0,0,0}` (otherwise `ChromaKey{}` defaults to green). |
| `Similarity` | `0.10` | `[0,1]` | Fully-transparent core radius. Higher removes more of the screen. |
| `Blend` | `0.08` | `[0,1]` | Width of the soft partial-alpha band past `Similarity`. **The main control for soft edges and hair.** |
| `Spill` | `1.0` | `[0,1]` | Despill strength over the whole foreground. `0` disables despill. |
| `ClipBlack` | `0` | `[0,1)` | Matte values at or below this clamp to transparent. Cleans haze; high values eat faint detail. |
| `ClipWhite` | `1` | `(0,1]` | Matte values at or above this clamp to opaque. Fills pinholes in the subject. Must be `> ClipBlack`. |
| `ShrinkEdge` | `0` (off) | `≥0` px | Erodes the matte inward to trim the fringe ring. **Eats thin hair — keep low.** |
| `SoftenEdge` | `0` (off) | `≥0` px | Blur radius applied to the matte to feather edges. |
| `Start` | `0` | time | When keying begins on the clip timeline; before it the source passes through. |

Out-of-range values, or `ClipBlack >= ClipWhite`, return `ErrInvalidChromaKey`;
a malformed `Hex` returns `ErrInvalidChromaKeyColor`.

## Recommended settings

**Hair / fine detail** — wide blend, strong despill, no erosion:

```go
fx.ChromaKey{
    Color:      [3]byte{87, 184, 0}, // your measured screen color
    Similarity: 0.10,
    Blend:      0.15, // widen toward 0.20 for fine/backlit hair
    Spill:      1.0,
    ClipWhite:  0.9,  // solidify the body, fill pinholes
    ShrinkEdge: 0,    // erosion shaves off thin strands — leave off
    SoftenEdge: 1,
}
```

**Hard-edged graphics / clean CGI** — tight, crisp, no feather:

```go
fx.ChromaKey{Color: [3]byte{0, 255, 0}, ShrinkEdge: 1, SoftenEdge: 0}
```

### Why hair is special

Hair strands are a sub-pixel **mix of hair and screen color**, so their chroma
lands *between* the key and the subject. That single fact drives the tuning:

- **`Blend` is hair's friend** — a wide band gives strands smooth partial alpha
  instead of an all-or-nothing cut. This is the highest-impact setting.
- **`Spill` is hair's friend** — it removes the green left *inside* the kept
  strands.
- **`ShrinkEdge` and a high `ClipBlack` are hair's enemies** — both attack the
  thin, low-alpha features that fine hair is made of. Keep `ShrinkEdge` at `0`
  (or `1` only for a hard rim on solid edges) and `ClipBlack` near `0`.

### Tuning guide

| Symptom | Fix |
| --- | --- |
| Green/grey patches remain in the keyed-out area | Raise `Similarity` to `0.12`; if it persists, `ClipBlack: 0.05` (last resort — costs the faintest hair). |
| Hair looks chewed / strands missing | Widen `Blend` toward `0.20`; confirm `ShrinkEdge: 0`. |
| Hard, jagged matte edge | Add `SoftenEdge: 1`. |
| Pinholes inside the subject | Lower `ClipWhite` to `~0.9`. |
| Green tinge on kept edges | Despill is already at max (`Spill: 1.0`); the remaining cure is light-wrap — see below. |

## Sidecar behavior

`Targets()` reports `{Video: true, Mask: true}`: the effect writes the mask and
also modifies the RGB (despill). Audio passes through untouched. See
[sidecar-propagation.md](sidecar-propagation.md) for the full matrix.

## Implementation notes

- **Float internally, Gray8 out.** The `video.VideoClip` contract carries an
  8-bit (`Gray8`) mask, so the matte is computed and processed in `float32`
  (`clip.MaskF32`) and quantized only at the final step. Float buffers are
  recycled through a `sync.Pool` shared across `WithStart`/`WithDuration`/
  `WithEnd` copies, mirroring the frame `scratch` pool.
- **Cost.** Per-pixel work is integer chroma math; the `sqrt` runs only inside
  the blend band. The optional shrink/soften passes are separable, and the blur
  uses a running sum, so its cost does not grow with radius. Adding a native
  imaging library would be *slower* here (cgo crossing + per-frame copy), not
  faster — the keyer is memory-bandwidth bound, not compute bound.

## Known gap (tracked, not silent)

- **No light-wrap.** Despill neutralizes the key color but cannot tint an edge
  with the *new* background's color, because that requires the composited
  backdrop, which lives in the compositor, not in this single-clip effect node.
  This is the last feature between `ChromaKey` and a full Keylight-style result;
  it would hook in at `composite/` where both layers are available.
