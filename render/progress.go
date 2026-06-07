package render

// Progress receives export progress callbacks. SetTotal is called once with the
// scheduled frame count (which may be revised down if the source ends early);
// Step is called once per frame written, in output order. Implementations must
// be safe for the single goroutine (the encode-feed) that calls them.
type Progress interface {
	SetTotal(frames int)
	Step()
}

// NopProgress is a Progress that ignores every callback.
type NopProgress struct{}

func (NopProgress) SetTotal(int) {}
func (NopProgress) Step()        {}
