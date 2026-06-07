package clip

// Clip is the timeline-metadata contract shared by video and audio clips.
// start/end are composition placement, distinct from media trim (a time
// transform). The "With" methods return new clips; implementations never mutate
// the receiver.
type Clip interface {
	Start() Time
	Duration() Time // Infinite => unknown/unbounded
	End() Time      // Infinite => unknown/unbounded
	WithStart(Time) Clip
	WithDuration(Time) Clip
	// WithEnd sets the end; if changeDuration, duration follows, otherwise start moves.
	WithEnd(end Time, changeDuration bool) Clip
	Close() error // idempotent
}

// DurationOr returns dur when ok, else Infinite. It centralizes the common
// (dur, hasDur) bookkeeping behind a node's Duration() accessor.
func DurationOr(dur Time, ok bool) Time {
	if ok {
		return dur
	}
	return Infinite
}

// EndOr returns start+dur when ok, else Infinite. It is the End() companion to
// DurationOr, guarding against start+Infinite overflow for unbounded clips.
func EndOr(start, dur Time, ok bool) Time {
	if ok {
		return start + dur
	}
	return Infinite
}
