package mgo

import (
	"github.com/mowshon/moviego/v2/composite"
	"github.com/mowshon/moviego/v2/transition"
)

// Transition is the one-method extension surface for the frames shown while one
// clip hands over to the next. Implement it (or use transition.Func) to add a
// custom transition; see the transition package.
type Transition = transition.Transition

// CrossfadeCurve selects the audio crossfade shape across a transition's
// overlap (equal-power by default).
type CrossfadeCurve = composite.CrossfadeCurve

// Audio crossfade curves, re-exported from composite.
const (
	CrossfadeEqualPower = composite.CrossfadeEqualPower
	CrossfadeLinear     = composite.CrossfadeLinear
	CrossfadeNone       = composite.CrossfadeNone
)

// TransitionStep is one boundary in a Sequence: a transition and the overlap
// duration it spans, plus an optional audio crossfade curve (equal-power when
// left zero). The Crossfade/WipeLeft/… helpers build common ones;
// UseTransition wraps any custom transition.
type TransitionStep struct {
	T     Transition
	Dur   Time
	Curve CrossfadeCurve
}

// Transition-step constructors. Each returns a TransitionStep spanning d, ready
// to pass to Sequence.Then or to compose into ConcatWith.

// Crossfade dissolves one clip into the next over d.
func Crossfade(d Time) TransitionStep { return TransitionStep{T: transition.CrossFade{}, Dur: d} }

// Dissolve is a film-style ordered-dither dissolve over d.
func Dissolve(d Time) TransitionStep { return TransitionStep{T: transition.Dissolve{}, Dur: d} }

// FadeThroughBlack fades down to black and back up into the next clip over d.
func FadeThroughBlack(d Time) TransitionStep {
	return TransitionStep{T: transition.FadeThroughColor{}, Dur: d}
}

// FadeThroughColor fades down to col and back up into the next clip over d.
func FadeThroughColor(d Time, col [3]byte) TransitionStep {
	return TransitionStep{T: transition.FadeThroughColor{Color: col}, Dur: d}
}

// Wipe reveals the next clip behind an edge sweeping in dir over d.
func WipeLeft(d Time) TransitionStep  { return wipe(transition.Left, d) }
func WipeRight(d Time) TransitionStep { return wipe(transition.Right, d) }
func WipeUp(d Time) TransitionStep    { return wipe(transition.Up, d) }
func WipeDown(d Time) TransitionStep  { return wipe(transition.Down, d) }

func wipe(dir transition.Dir, d Time) TransitionStep {
	return TransitionStep{T: transition.Wipe{Dir: dir}, Dur: d}
}

// Slide slides the next clip in over the current one, moving in dir over d.
func SlideLeft(d Time) TransitionStep {
	return TransitionStep{T: transition.Slide{Dir: transition.Left}, Dur: d}
}
func SlideRight(d Time) TransitionStep {
	return TransitionStep{T: transition.Slide{Dir: transition.Right}, Dur: d}
}
func SlideUp(d Time) TransitionStep {
	return TransitionStep{T: transition.Slide{Dir: transition.Up}, Dur: d}
}
func SlideDown(d Time) TransitionStep {
	return TransitionStep{T: transition.Slide{Dir: transition.Down}, Dur: d}
}

// Push slides the next clip in while pushing the current one out, in dir over d.
func PushLeft(d Time) TransitionStep {
	return TransitionStep{T: transition.Push{Dir: transition.Left}, Dur: d}
}
func PushRight(d Time) TransitionStep {
	return TransitionStep{T: transition.Push{Dir: transition.Right}, Dur: d}
}
func PushUp(d Time) TransitionStep {
	return TransitionStep{T: transition.Push{Dir: transition.Up}, Dur: d}
}
func PushDown(d Time) TransitionStep {
	return TransitionStep{T: transition.Push{Dir: transition.Down}, Dur: d}
}

// IrisOpen reveals the next clip through a circular aperture growing from the
// center over d; IrisClose hides the current clip through a contracting one.
func IrisOpen(d Time) TransitionStep  { return TransitionStep{T: transition.Iris{}, Dur: d} }
func IrisClose(d Time) TransitionStep { return TransitionStep{T: transition.Iris{Out: true}, Dur: d} }

// UseTransition is the generic step for any custom or specially-configured
// transition, so nothing is locked down:
//
//	seq.Then(mgo.UseTransition(Glitch{Intensity: 0.6}, 400*time.Millisecond))
func UseTransition(t Transition, d Time) TransitionStep {
	return TransitionStep{T: t, Dur: d}
}

// ConcatOptions configures ConcatWith. Transition (with Duration) applies the
// same transition on every boundary; leave Transition nil for a plain concat.
type ConcatOptions struct {
	Transition Transition
	Duration   Time
	AudioCurve CrossfadeCurve
}

// ConcatWith joins clips with one uniform transition between every pair (the
// common case). With a nil Transition or non-positive Duration it is a plain
// end-to-end concat.
func ConcatWith(opts ConcatOptions, clips ...*Video) *Video {
	inners, err := unwrap(clips)
	if err != nil {
		return &Video{err: err}
	}
	if len(inners) == 0 {
		return &Video{err: composite.ErrEmptyConcat}
	}
	specs := make([]*composite.TransitionSpec, len(inners)-1)
	if opts.Transition != nil && opts.Duration > 0 {
		for i := range specs {
			specs[i] = &composite.TransitionSpec{T: opts.Transition, Dur: opts.Duration, Curve: opts.AudioCurve}
		}
	}
	out, err := composite.Sequence(inners, specs)
	if err != nil {
		return &Video{err: err}
	}
	return &Video{inner: out}
}

// Sequence is a fluent builder for a timeline with per-boundary transitions, the
// clearest API for mixed transitions:
//
//	v := mgo.NewSequence().
//	    Add(intro).
//	    Then(mgo.Crossfade(time.Second)).
//	    Add(body).
//	    Then(mgo.WipeLeft(500 * time.Millisecond)).
//	    Add(outro).
//	    Video()
//
// Two clips added without a Then between them are a hard cut. A build error from
// any added clip, or from assembly, is carried onto the returned Video.
type Sequence struct {
	clips   []*Video
	steps   []*TransitionStep // step before clip i+1; len grows toward len(clips)-1
	pending *TransitionStep
}

// NewSequence starts an empty transition sequence.
func NewSequence() *Sequence { return &Sequence{} }

// Add appends a clip. Any pending Then becomes the transition into this clip.
func (s *Sequence) Add(v *Video) *Sequence {
	if len(s.clips) > 0 {
		s.steps = append(s.steps, s.pending)
		s.pending = nil
	}
	s.clips = append(s.clips, v)
	return s
}

// Then sets the transition used on the next boundary (consumed by the following
// Add). A trailing Then with no further Add is ignored.
func (s *Sequence) Then(step TransitionStep) *Sequence {
	st := step
	s.pending = &st
	return s
}

// Video assembles the sequence into a *Video carrying any build error.
func (s *Sequence) Video() *Video {
	inners, err := unwrap(s.clips)
	if err != nil {
		return &Video{err: err}
	}
	if len(inners) == 0 {
		return &Video{err: composite.ErrEmptyConcat}
	}
	specs := make([]*composite.TransitionSpec, len(inners)-1)
	for i, st := range s.steps {
		if st != nil {
			specs[i] = &composite.TransitionSpec{T: st.T, Dur: st.Dur, Curve: st.Curve}
		}
	}
	out, err := composite.Sequence(inners, specs)
	if err != nil {
		return &Video{err: err}
	}
	return &Video{inner: out}
}
