package mgo

import "github.com/mowshon/moviego/v2/render"

// Progress receives per-frame export callbacks. See render.Progress. Set one on
// ExportOptions.Progress to report rendering progress.
type Progress = render.Progress

// BarProgress is a built-in Progress that draws a single-line percentage bar to
// stderr, replacing the bar each example used to copy. The zero value works; set
// Label to title it. See render.BarProgress.
type BarProgress = render.BarProgress
