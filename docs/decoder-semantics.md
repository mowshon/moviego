# Decoder semantics

This pins the exact behaviour of the sequential video decoder
(`video/io.Decoder`) so its output matches MoviePy frame-for-frame and so the
skip-path and restart-path are provably equivalent.

## Position model

`pos` is the index of the **next** frame `ReadInto` will return. After reading
frame `n`, `pos == n+1`. A freshly opened decoder has `pos == 0` and an FFmpeg
process already positioned at frame 0.

`ReadInto(dst)` reads the frame at `pos` into `dst`, advances `pos`, and returns
`ErrEOF` at end of stream. `SeekToFrame(idx)` repositions so the next `ReadInto`
returns frame `idx`.

## Frame-index epsilon

Mapping a timestamp to a source frame index uses MoviePy's biased truncation,
computed in integer math: `int(fps*t + 1e-5)`, i.e. `Rate.TimeToFrame`. The
`1e-5s` bias keeps a frame's own exact timestamp from rounding down into the
previous frame. The same epsilon backs the seek target off by `1e-5s` so a
precise `-ss` lands on the intended frame rather than the next one.

## Two-pass `-ss`

A restart seeks in two passes, matching MoviePy:

- `start = FrameTime(idx) − 1e-5s`
- `offset = min(1s, start)`
- coarse `-ss (start − offset)` **before** `-i` (fast, keyframe-granular)
- precise `-ss offset` **after** `-i` (exact, decodes-and-discards to the frame)

When `start ≤ 0` (frame 0, or a target inside the first epsilon) no `-ss` is
emitted and decoding begins at the start of the file.

## Seek state table

`SeekToFrame(idx)` with `pos` = index of the next frame to be returned:

| State | Action |
| --- | --- |
| `idx == pos` | no-op; the next `ReadInto` already returns `idx` |
| `idx < pos` (backward) | **restart** at `FrameTime(idx) − 1e-5s` |
| `idx > pos + 100` (far ahead) | **restart** at `FrameTime(idx) − 1e-5s` |
| `pos < idx ≤ pos + 100` (nearby ahead) | **skip** exactly `idx − pos` frames into scratch, then the next `ReadInto` returns `idx` |

The threshold is 100 frames (video). The skip path and the restart path return
**identical pixels** for the same `idx`, because decoding a given source frame
is deterministic regardless of how the decoder arrived there; this is asserted
by test.

### `idx == lastRead` (node-level cache)

The "return the cached current frame" row of MoviePy's reader is implemented one
level up, in `VideoFileNode`: it caches the last decoded frame and serves a
repeated index from that copy without touching the decoder. The decoder itself
only distinguishes the five positioning states above. Keeping the cache in the
node preserves the single-owner buffer contract (the decoder never hands back a
buffer it still owns).

## End of stream

A clean end (FFmpeg exits 0 and closes stdout) surfaces as `ErrEOF`; the export
schedule also stops on its own floor frame count, so EOF is the backstop for
short or variable-rate files. A canceled context surfaces the context error,
never `ErrEOF` or `ErrVideoCorrupted`.
