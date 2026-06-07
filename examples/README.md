# MovieGo Examples

These examples are small Go programs that run directly from the repository root.
They expect FFmpeg and FFprobe to be available on `PATH` (or through the
library's `MGO_FFMPEG` / `MGO_FFPROBE` environment variables).

On first run, the examples download small public sample media into
`examples/input/`. Rendered videos are written to `examples/output/`. Both
directories are gitignored.

The `text` and `subtitles` examples download fonts through the shared asset
helper (Inter, Noto Sans, and Roboto Mono are stored under
`examples/input/fonts/`); text rendering also has a built-in default font, so a
font path is optional.

## Examples

`trim` opens a real MP4, keeps seconds `1..4`, resizes to 50%, and fades in.

`still-image` turns a downloaded photo into a 3-second video with resize and
fade-out.

`slideshow` builds an image sequence from three same-size downloaded photos.

`composite` overlays a photo and a delayed picture-in-picture video on top of a
real video background.

`concat` combines two real video trims and a still-photo segment into one
timeline.

`transitions` stitches video and still clips with every built-in transition, an
eased decorator, and a custom diagonal transition defined outside the library.

`transparent` composites a downloaded PNG with alpha over a transparent canvas
and exports a `.mov` that preserves alpha.

`pipeline` exports the same real-video graph through the sequential engine and
the parallel worker pipeline, printing progress for both.

`audiomix` opens a real MP4 and a real MP3, trims and fades the music bed, mixes
it with the video's audio when present, and muxes the result into an MP4.

`reframe` probes a real video, describes its render plan without rendering, then
reframes it (fit-to-square letterbox, horizontal flip, blur), extracts a single
still as PNG, and exports with the built-in progress bar.

`text` overlays a stroked, centered title and a tagline (rendered with the Inter
font) on a real video background.

`motion` shows the motion-graphics toolkit: a vector-drawn lower third (the
`draw` canvas), an animated slide-in title and a typewriter subtitle, a circular
watermark logo, and a burned-in frame timecode — all composited over a real
video. It then writes a second file, a scrolling credits roll over black.

`subtitles` writes a small `demo.srt`, parses it, and burns the cues onto a real
video as wrapped, bottom-anchored captions.

`subtitles-json` is the JSON counterpart of `subtitles`: it writes a flat
`[{start,end,text}]` array (string timestamps), loads it with `SubtitlesJSON`,
and burns wrapped, bottom-anchored captions over a real video.

`word-captions` renders social-style, one-word-at-a-time captions over a
vertical (9:16) video. It writes the rich JSON form (`{styles, cues:[{…,words}]}`
with millisecond timestamps and per-word timing), loads it with
`LayoutWordCenter`, and centers each word (color from the JSON styles block,
nudged to the lower third with `VPos`).
