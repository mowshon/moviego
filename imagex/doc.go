// Package imagex provides pure image decode, encode, resize, crop, rotate, and
// alpha operations over clip.Frame buffers. It depends only on clip and the Go
// image stack; it knows nothing of the video graph, FFmpeg, or composition, so
// its operations are deterministic and unit-testable without external tools.
//
// Resize uses golang.org/x/image/draw with the CatmullRom kernel. This differs
// from FFmpeg/OpenCV bicubic, a documented visual divergence (see the plan's
// compatibility notes); golden tests that compare against FFmpeg use tolerance.
package imagex
