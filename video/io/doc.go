// Package videoio is the low-level FFmpeg video decode/encode boundary. It turns
// a media file into a sequential stream of packed RGB frames (Decoder) and a
// stream of RGB frames into an encoded file (Encoder), and nothing more: it
// knows about pixels, process lifecycle, and decoder seek semantics, but not
// about effects, composition, or the clip graph.
//
// The Decoder's seek behaviour (skip vs restart, two-pass -ss, frame-index
// epsilon) is specified in docs/decoder-semantics.md and pinned by tests that
// assert the skip-path and restart-path return identical pixels for the same
// frame index.
package videoio
