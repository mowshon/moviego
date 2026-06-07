// Package transition renders the frames shown during the overlap window where
// an outgoing clip A hands over to an incoming clip B.
//
// The design observation is that nearly every common transition (crossfade,
// dissolve, fade-through-color, wipe, slide, push, iris) is a pure function of
// three things: a normalized progress p in [0,1], the outgoing frame A, and the
// incoming frame B — the same shape as the compositor's per-row blends. So a
// Transition is one method, Frame, and that single method is the entire
// extension surface: a contributor writes one function and gets a fully working
// transition.
//
// Three escalating ways to extend it:
//
//   - Built-in transitions (CrossFade, Wipe, …) ship here as config structs with
//     useful zero-value defaults, exactly like effects. Each lives in its own
//     file.
//   - A custom transition implements the one-method interface in any package, no
//     moviego changes required.
//   - For a one-off, Func adapts a plain function without declaring a type.
//
// The Eased and Reversed decorators keep individual transitions simple while
// still allowing rich behavior: any transition can be eased through an ease.Func
// or have its direction flipped without a second implementation.
//
// See README.md for the catalog of built-ins and a guide to writing a custom
// transition.
package transition
