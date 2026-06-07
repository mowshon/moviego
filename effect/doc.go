// Package effect holds the video and audio effects that transform clips into
// new clips. Effects never mutate their input: each Apply returns a new graph
// node. Every effect declares, via Targets, whether it changes the RGB, the
// mask, and/or the audio sidecar; that declaration is checked against the
// sidecar-propagation matrix in the package tests, replacing MoviePy's hidden
// apply_to_mask / apply_to_audio decorators with an explicit, tested contract.
package effect
