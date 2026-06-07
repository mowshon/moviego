# Compatibility — deliberate divergences from MoviePy

This is the authoritative registry of where MovieGo intentionally differs from
MoviePy 2.x. Each entry is a *choice*, not a bug; tests pin the chosen behaviour.
New divergences land here in the same change that introduces them.

## Timing

- **Rational frame rate.** Rates are exact `Num/Den`, never `float64`. `30000/1001`
  is represented exactly; MoviePy's float `fps` drifts over long clips. See
  [timing.md](timing.md).
- **`FrameTime` is exact and overflow-safe.** `i*Den*Second/Num` is computed in
  int64 on the common path and in big.Int beyond ~9.2e9 (`i*Den`), so a
  multi-day clip keeps an exact timestamp instead of overflowing.
- **`TimeToFrame` integer truncation.** Reading an existing file maps a
  timestamp with `int(fps*t + 1e-5)` computed in integer math, truncating toward
  zero (Python `int()`). For `t < 0` this truncates upward rather than flooring
  (e.g. `-1ms @ 30fps → 0`). Used only for *reading*; export is index-driven.
- **Floor export frame count.** Export emits frame `i` while
  `FrameTime(i) < duration`. MoviePy uses `int(duration*fps)`; the two agree
  except at exact boundaries, where MovieGo never emits a frame whose timestamp
  equals the duration. The decoder's EOF is the backstop; the smaller of the two
  wins and the actual count is reported.
- **Subclip boundary.** Negative bounds count back from the source
  duration; `b == 0` means "to the end". The end tolerates one rational tick of
  overshoot before clamping. The window is clamped into `[0, srcDur]` and to a
  non-negative duration — an inverted or out-of-range request yields an empty
  clip, never a negative-duration one. (MoviePy raises on some of these.) On the
  facade the end is optional — `Subclip(start)` runs to the end of the media —
  for both video and audio, which resolve the open/negative end against their own
  source durations by the same rule.
- **Duration/End sentinel, not `None`.** MoviePy uses `None` for an unknown or
  infinite duration. MovieGo's `Duration()`/`End()` return a single `Time` and
  report `clip.Infinite` (test with `clip.Finite`) instead of a `(value, ok)`
  pair. Export requires a finite duration and rejects `Infinite` with
  `ErrNoDuration`; an open-ended `Subclip` of an unbounded source stays
  `Infinite` rather than fabricating a length.

## Rate approximation

- **`RateFromFloat` thousandths.** A non-integer fps with no exact rational (and
  not matching an NTSC snap) is approximated as `round(fps*1000)/1000`. This is
  only reached for genuinely odd probed rates; standard rates resolve exactly or
  via the NTSC snap.
- **NTSC snap.** A probed rate within `0.01` of a standard `x*1000/1001` value
  (x ∈ 23,24,25,30,50) snaps to the exact rational. Other near-misses (e.g. 60)
  are left as the exact stored ratio.

## Decode / encode

- **Subprocess FFmpeg, not cgo libav.** A decoder fault is an isolated process
  crash, not a memory-safety hole. Frame-accurate seeking relies on two-pass
  `-ss` (see [decoder-semantics.md](decoder-semantics.md)).
- **Display-oriented decode.** Rotated inputs are decoded already rotated
  (FFmpeg autorotation); `Size()` is post-rotation. MoviePy exposes the same
  display size.
- **`x/image` resize kernel.** The Go resize path uses CatmullRom, not
  FFmpeg/OpenCV bicubic. When a resize is delegated to FFmpeg (filtergraph
  fusion, below), it maps to FFmpeg's default `scale` (bicubic), so a fused
  export and a Go export of the same `Resize` can differ slightly. Documented
  visual divergence; golden tests use tolerance.

## Filtergraph fusion (FFmpeg-only export)

- **Fusion is opt-in, off by default.** Set
  `ExportOptions.EnableFusion` to allow it. When enabled and the whole graph is a
  single file source through a chain of forward trim/speed/scale/crop/fade, the
  planner runs one FFmpeg `-filter_complex` invocation (`Engine=ffmpeg-only`)
  instead of decoding to Go pixels and re-encoding, avoiding the rawvideo
  round-trip (the biggest common-case speedup). It is **off by default** so a
  plain export always uses the Go engines — the correctness oracle — keeping the
  documented behavior (e.g. the CatmullRom resize kernel) and avoiding the fused
  path's divergences below. The decision and the fallback reason are reported via
  `ExportOptions.Debugf`.
- **What fuses (v1).** Exactly one file source; forward `Subclip` (`trim`+
  `setpts`), positive `Speed` (`setpts`), `Resize` (`scale`), `Crop` (`crop`),
  `FadeIn`/`FadeOut` (`fade`). The chain ends with an `fps` filter at the export
  rate and is capped to the scheduled frame count (`-frames:v`), so a fused
  export and a Go export agree on length and both stop early at source EOF.
- **Color grading & blur (v2.1) advertise filters but diverge numerically.** The
  color-grading effects map to FFmpeg as: `Brightness`/`Contrast`/`Saturation`/
  `Gamma` → `eq`; `ColorBalance` → `colorbalance`; `HSL` → `hue` (only when its
  `Light` is zero); `LUT` → `lut3d`; `GaussianBlur` → `gblur`; arbitrary `Rotate`
  → `rotate`. These let a straight color chain fuse, but the Go path is the
  oracle and the two are **not bit-identical**: the Go ops use precomputed 8-bit
  LUTs / a fixed Gaussian kernel / bilinear rotation, whereas FFmpeg uses its own
  float pipeline and kernels. Goldens pin the Go path; the fused path is tolerated
  as a documented divergence, the same stance as the `scale` kernel above. An
  effect with a non-zero `Start` drops its filter (it is no longer a single static
  filter) and renders on Go. `Invert`, `MotionBlur` and `Vignette` advertise no
  filter and are always Go-only.
- **What does not fuse (falls back to Go, never silently wrong).** Transparent
  (masked) output; multi-layer composites and concatenations; image/color
  sources; a **source carrying display-matrix rotation** (the Go decode
  autorotates it, but FFmpeg does not autorotate a raw `[0:v]` fed into
  `-filter_complex`, so the source declines fusion); the **quarter-turn** `Rotate`
  path (exact multiples of 90° use the lossless Go transpose and advertise no
  filter, since the FFmpeg `transpose` direction has not been parity-gated yet —
  the arbitrary-angle path does advertise `rotate`, see the color/blur note
  above); reverse/`Loop` and any other random-access time map; and any effect that
  does not advertise a filter (e.g. `BlackAndWhite`, `WaterDrop`).
- **Backlog — rotated-source fusion is a deliberate coverage gap.** Letting a
  rotated source fuse is gated on adding an explicit `transpose`/`setsar` stage
  (or `-autorotate`-equivalent handling) to the fused graph and proving it
  matches the Go autorotated decode. When that parity work lands, the switch to
  flip is `render.TestRotatedSourceDeclinesFusion` (it currently asserts a
  rotated source does *not* fuse); invert it to assert the fused output matches
  the Go render instead. Until then the decline in `VideoFileNode.FilterInput`
  stays.
- **Trim frame boundary differs when fused.** FFmpeg `trim=start=S` keeps frames
  whose timestamp is `>= S`, whereas the Go path selects the source frame by
  `int(fps*t+1e-5)`. At a start that lands between frames the two can pick
  adjacent source frames (an off-by-one at the cut). This is the documented
  timing divergence above applied to the fused path, not a fusion bug.
- **Audio on the fused path.** Audio is still rendered to a temp file from the
  same timeline (MoviePy order) and muxed into the single FFmpeg invocation with
  `-c:a copy -shortest`; the source's own audio track is not copied through.

## Audio

- **One index-rounding rule.** A timestamp maps to a sample frame with a
  single floor (`clip.SampleIndex`, true floor including negatives), with no
  scalar-vs-array asymmetry. MoviePy truncates a scalar `t` but rounds an array
  `t`; MovieGo floors everywhere, and `SampleTime`/`SampleIndex` are an exact
  inverse pair so chunked reads are gap-free.
- **Mixer resampling is Go linear interpolation.** When a mix combines sources of
  differing sample rates, the mixer linearly interpolates each child to the mix
  rate (max child rate) rather than resampling through FFmpeg. MoviePy does not
  resample in its compositor at all (it relies on per-clip `get_frame` rounding);
  MovieGo converts explicitly. Lower quality than a polyphase resampler for large
  ratios, and it currently restarts the source decoder per chunk — see the audio
  decode note in [ffmpeg-policy.md](ffmpeg-policy.md). Same-rate mixes (the common
  case) skip interpolation entirely.
- **Mono→stereo up-mix duplicates the channel.** A mono source mixed into a
  multi-channel output is copied into every channel (the last source channel fills
  any extra destination channel), so its perceived loudness rises ~3 dB versus a
  `×1/√2` up-mix. The mix sum is left un-clamped until the quantization boundary,
  where it is clamped to `[-0.99, 0.99]`.

## Text and subtitles

- **Caption-stable, not pixel-identical to Pillow.** Text is rasterized
  with the pure-Go `golang.org/x/image/font/opentype` stack over the embedded Go
  Regular face (the default font), not Pillow/FreeType. Line layout, wrapping,
  and the clip's reported width/height are reproducible and follow MoviePy's
  height math (baseline anchor, `ascent + descent + (lines-1)*lineGap + 2*stroke`),
  but anti-aliased glyph pixels are not asserted byte-for-byte. Golden tests pin
  geometry and ink presence, not exact pixels.
- **Stroke is an outline pass, not Pillow's `stroke_width`.** An outline is drawn
  by painting the glyphs in the stroke color at every integer offset within the
  stroke radius, then the fill on top. It matches MoviePy's height contribution
  (`2*stroke` on each axis) and reads as a clean outline, but the exact edge
  pixels differ from Pillow's stroked glyph rendering.
- **Caption auto-fit is integer-pixel bisection.** When a caption gives a fixed
  `Size` and no `FontSize`, the largest integer pixel size whose wrapped text
  fits the box is found by bisection (MoviePy's `__find_optimum_font_size`). It
  returns at least 1px so a frame is always produced, even for an impossible box.
- **Subtitles are a canvas-sized transparent clip.** `SubtitlesNode` is a
  full-canvas transparent video clip that paints the active cue (centered
  horizontally, anchored to the bottom margin) and is fully transparent between
  cues, so it composites directly over video. MoviePy's `SubtitlesClip` instead
  returns a text-sized frame the caller positions. Rendered cue frames are held
  in a bounded LRU and the active cue is found by binary search; MoviePy
  re-renders and grows an unbounded dict, and scans cues linearly.
- **One index-rounding rule already covers subtitle decimals.** SRT `,` and VTT
  `.` millisecond separators are both accepted by the parser, and a short
  fractional part is right-padded (`,5` → 500ms).
- **JSON subtitles in two shapes, one parser.** A flat `[{start,end,text}]` array
  (string timestamps) and a rich `{styles,cues:[{…,words:[…]}]}` object
  (millisecond-number timestamps, optional per-word timing) both parse through
  the same auto-detecting reader (`[` vs `{` on the first non-space byte). This
  is a MovieGo addition with no MoviePy equivalent.
- **Word-centered layout.** `LayoutWordCenter` shows one `Cue.Words` token at a
  time, centered, for vertical/social captions; a cue without word timing falls
  back to its whole text centered. `VPos` (a `(0,1]` fraction) moves the vertical
  center for either layout. MoviePy has no built-in word-by-word layout.
- **Style font names are not resolved.** A `styles.font` value is used only when
  it names a readable font file; a family name like `"Arial"` is ignored and the
  embedded default face is used. There is no system-font lookup. A hex color
  (`#RGB`/`#RRGGBB`/`#RRGGBBAA`) from the style block is applied only when the
  caller did not set a color explicitly.
- **ASS/SSA is parsed to the common subset.** `SubtitlesASS` reads the `[Events]`
  Dialogue lines, mapping columns through the section's `Format` declaration (so
  column order is honored) and stripping override blocks (`{\…}`), with `\N`/`\n`
  → newline and `\h` → space. Styling, positioning (`\pos`, `\an`), karaoke, and
  drawing commands are intentionally ignored — only the text and its
  `[Start, End)` interval are used. Timestamps are `H:MM:SS.cc` (centiseconds).

## Motion graphics, animated text & drawing

- **Animated text is a self-contained transparent clip.** `AnimatedText` renders
  the title onto a canvas the size of the resting text and animates it per frame;
  the typewriter path re-rasterizes and caches one frame per revealed prefix.
  Built-in entrances and a `Custom` progress→`AnimState` function cover motion;
  MoviePy expresses this through generic per-frame position/opacity functions
  instead. The clip defaults to the entrance duration and holds afterward.
- **Credits scroll within their own viewport.** `Credits` stacks the lines into
  one block and scrolls it up through the `Size` viewport; the clip duration is
  the full traversal time. It is a MovieGo convenience with no direct MoviePy
  equivalent.
- **Timecode canvas is fixed to the widest readout.** `BurnTimecode` sizes its
  raster to the format's all-`8` width and renders each frame right-aligned into
  it, so the overlay never jitters as digits change width. `TCFrames` uses the
  underlying clip's rate and falls back to `.mmm` when the rate is unknown.
- **Drawing is `x/image/vector` fills only.** The `draw` canvas rasterizes shapes
  with the same antialiased fill rasterizer used for glyphs. Strokes (and shape
  outlines) are expressed as filled bands — an outer path plus a reversed inner
  path leaves a ring under the rasterizer's absolute-winding rule — so there is no
  separate stroking engine. Lines are round-capped capsules. There is no MoviePy
  drawing API; this is a MovieGo addition that avoids a second 2D/font stack.

## Transitions

- **Go-only, no FFmpeg fusion.** A transition is a pure `(p, A, B) → frame`
  function rendered on the Go path; it advertises no filtergraph equivalent, so a
  transition timeline never takes the FFmpeg-only fast path. MoviePy has no
  built-in transition system (crossfades are hand-built with `CompositeVideoClip`
  and `crossfadein`/`crossfadeout`).
- **Assembled transition timelines render sequentially.** A sequence trims each
  clip's body and adds a two-input overlap node between bodies. Because a clip's
  body and its adjacent transition both reach that same source, the planner's
  source-multiplicity guard sees ≥ 2 paths to the source and routes the whole
  timeline to the sequential engine — one forward decoder cannot serve two
  disjoint windows of the same source per output frame. This applies even when
  adjacent clips are distinct files, since each file is still reached by its own
  body plus the transition. A bare transition with no straight-through bodies can
  remain pipeline-eligible. `mgo.Describe` reports the chosen engine.
- **Equal-power audio crossfade by default.** Across the overlap, A's tail and
  B's head are mixed with `gain = √p` / `√(1-p)` so the summed power stays flat;
  a linear pair dips ~3 dB at the midpoint. `CrossfadeLinear` and `CrossfadeNone`
  are opt-in. The video blend and the audio crossfade are independent.
- **Every built-in blends alpha to match its shape.** Alpha is handled through
  the optional `MaskTransition` extension (the engine type-asserts for it), and
  all built-ins implement it: the wipe edge, slide/push translation, iris
  aperture, and dissolve stipple drive the alpha sidecar with the *same* spatial
  decision as RGB, so a wipe from opaque A to transparent B keeps one side opaque
  and the other transparent — not a uniform half-transparency. A custom
  transition that does *not* implement `MaskTransition` is treated as opaque over
  opaque inputs; over transparent inputs its alpha falls back to a straight
  crossfade.

## Still to be recorded

- Composite alpha keyed by frame index, not float `t`.
