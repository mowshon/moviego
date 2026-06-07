// Package audioio owns the low-level FFmpeg audio boundary: decoding a media
// file to interleaved float32 PCM and encoding interleaved float32 PCM to a
// compressed temp file. It knows nothing about the clip graph or mixing; the
// audio nodes in package audio build on it.
package audioio
