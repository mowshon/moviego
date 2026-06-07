# Composite

The composite package layers and concatenates video clips. It provides three
node types, a blend subpackage, and the `Sequence` assembler that wires them
together with transitions.

## CompositeNode — z-order layering

`CompositeNode` is the timeline compositor. At each output frame it draws active
children in z-order (by `Layer`, stable by input order on ties) onto a canvas,
blending each child over the accumulating result via one of four integer paths.

```go
comp := composite.New([]composite.CompositeChild{
    {Clip: background, Start: 0, Pos: composite.Position{}, Layer: 0},
    {Clip: overlay,    Start: time.Second, Pos: composite.Position{Keyword: composite.PosCenter}, Layer: 1},
}, composite.Options{
    Size:    clip.Size{W: 1920, H: 1080},
    BGColor: [3]byte{0, 0, 0},
})
```

**Canvas size.** A zero `Options.Size` adopts the first child's size.

**Duration.** The composite's duration is the latest child end (`child.Start +
child.Duration()`). If any child has an unknown duration, the composite's
duration is also unknown.

**Rate.** The output rate is the max child rate.

**Transparency.** Set `Options.Transparent: true` to start from a fully
transparent canvas. The output carries an alpha sidecar and `HasMask()` returns
true. Without it the canvas is filled with `BGColor` (default black) and the
output is opaque.

**Audio.** Each child's audio sidecar is placed at `child.Start` on the
composite timeline and mixed. The total mix is truncated to the composite's
current duration when known.

**Parallel safety.** `RenderInto` allocates all scratch per call (no shared
mutable state in the node itself), so it is safe to call concurrently for
different times when all children are.

### Positioning

```go
// Absolute pixels
composite.Position{X: 100, Y: 50}

// Relative: fraction of the free space (canvas − child), not fraction of canvas.
// 0.5/0.5 always centers regardless of child size.
composite.Position{X: 0.5, RelX: true, Y: 0.5, RelY: true}

// Keywords (unspecified axis is centered)
composite.Position{Keyword: composite.PosCenter}
composite.Position{Keyword: composite.PosLeft}
composite.Position{Keyword: composite.PosRight}
composite.Position{Keyword: composite.PosTop}
composite.Position{Keyword: composite.PosBottom}

// Animated: absolute pixels as a function of the child's local time.
// A child starting at 2s has local time 0 at global t=2s, so the animation
// is always relative to when the child starts, not the composite start.
composite.Position{Animated: func(lt clip.Time) (x, y float64) {
    return float64(lt / time.Millisecond), 0
}}
```

> **Note:** Relative positioning is fraction of *free space* (`canvas − child`),
> not fraction of canvas. This means `RelPos(0.5, 0.5)` always centers, and
> `RelPos(0, 0)` / `RelPos(1, 1)` are flush at the edges — regardless of child
> size. See `docs/compatibility.md` for the deliberate divergence from MoviePy.

### Blend paths

The compositor selects one of four integer kernels per row based on two flags:
canvas transparency and whether the source's overlap region is fully opaque
(`regionMin == 255`).

| Canvas | Source | Path | Kernel |
|--------|--------|------|--------|
| opaque | opaque | a | `CopyRGBRow` — direct paste |
| opaque | masked | c | `OverOpaqueRow` — `fg*a + bg*(1-a)` |
| transparent | opaque | b | `CopyRGBRow` + `FillAlphaRow(255)` |
| transparent | masked | d | `OverRow` — premultiplied "over" with recovered RGB |

## ConcatChainNode — same-size end-to-end

`ConcatChain` plays clips back-to-back. All clips must share a size and each
must have a known duration. The active child is selected by binary searching the
cumulative timing array, and rendered at its local time.

```go
chain, err := composite.ConcatChain([]video.VideoClip{intro, body, outro})
```

If any child has a mask the whole chain carries a mask; a maskless child's
window is filled opaque (alpha 255).

## ConcatComposeNode — mixed-size end-to-end

`ConcatCompose` joins clips of different sizes by centering each on a canvas
sized to the largest width and height. It is a `CompositeNode` underneath, so
it inherits the blend paths and one-pass rendering.

```go
comp, err := composite.ConcatCompose([]video.VideoClip{a, b, c}, composite.ConcatOptions{
    Padding: -500 * time.Millisecond, // negative padding overlaps consecutive clips
})
```

## Sequence — clips + transitions

`Sequence` stitches clips into one timeline with transitions between them. Each
interior clip's body is trimmed to make room for the overlaps on both sides.

```go
clips := []video.VideoClip{a, b, c}
specs := []*composite.TransitionSpec{
    {T: transition.CrossFade{}, Dur: 500 * time.Millisecond, Curve: composite.CrossfadeEqualPower},
    nil, // nil = hard cut between b and c
}
seq, err := composite.Sequence(clips, specs)
```

`len(specs)` must equal `len(clips) - 1`. A nil or zero-duration entry is a
hard cut. A clip whose two overlaps together exceed its duration is an error.

When a clip is completely consumed by its overlaps (e.g., a crossfade as long
as both clips), the body is empty and only the overlap node is emitted.

**Duration:** `sum(clip durations) − sum(active overlap durations)`.

**Audio:** The transition overlap node mixes A's tail (faded out) with B's head
(faded in). Three curves are available:

| Constant | Behavior |
|----------|----------|
| `CrossfadeEqualPower` (default) | `√p` / `√(1−p)` — flat summed power |
| `CrossfadeLinear` | linear ramps |
| `CrossfadeNone` | no blend; A's tail plays through, B starts at body boundary |

## TransitionNode — two-clip overlap

`NewTransition` builds the overlap node directly, without the full `Sequence`
assembler. The node's duration equals the overlap, its output time `t` maps A
to `durA−overlap+t` (the tail) and B to `t` (the head).

```go
tn, err := composite.NewTransition(a, b, transition.Wipe{Dir: transition.Right}, time.Second, composite.CrossfadeEqualPower)
```

Constraints checked at construction: both clips must share a size, each must be
at least as long as the overlap, and the transition must be non-nil.

The node reports `HasMask()` only when an input clip carries a mask — a
`MaskTransition` does not introduce alpha into opaque sources. If a
`MaskTransition` is not available on the transition, the alpha sidecar defaults
to a linear lerp between the two alpha channels.

## blend subpackage

`composite/blend` contains the four integer compositing kernels. It also
exports `LerpRow` (used by crossfade transitions for per-row time-domain
blending) and `Blend1` (the single-channel porter-duff alpha blend helper).

All functions operate on flat packed rows (RGB24 = 3 bytes/pixel, Gray8 = 1
byte/pixel) and are free of allocation. `ref.go` holds the float64 reference
implementations used in tolerance tests (not on the hot path).

## Rendering notes

- Transitions are Go-only (no FFmpeg filter advertisement). A timeline built
  with `Sequence` always uses the sequential engine when any clip's straight-
  through body and its adjacent transition both reach the same decoder
  (multiplicity ≥ 2). A bare transition with no remaining bodies stays
  pipeline-eligible.
- `CompositeNode` and `ConcatChainNode` never own the children passed to them;
  the handle that opened each source is responsible for closing it.

See `examples/composite`, `examples/transitions`, and `examples/slideshow` for
runnable demos.
