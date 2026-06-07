package clip

import (
	"math"
	"math/big"
	"time"
)

// SampleTime and SampleIndex map between a sample-frame index and a timestamp at
// sample rate sr. They are an exact inverse pair for idx >= 0: SampleIndex
// (floor) ∘ SampleTime (ceil) is the identity, so a chunked audio render can
// drive contiguous, gap-free sample ranges through the Time-based SamplesInto
// API without ever dropping or duplicating a sample at a chunk boundary.
//
//	SampleTime(idx)  = ceil(idx * 1e9 / sr)   // the earliest time that maps back to idx
//	SampleIndex(t)   = floor(t * sr / 1e9)    // the sample playing at time t
//
// The common case is plain int64; a pathologically large index or timestamp
// (tens of hours at a high rate) falls back to big.Int so the result stays exact
// instead of overflowing, mirroring Rate.FrameTime / Rate.TimeToFrame.
//
// SampleTime requires idx >= 0. For negative idx the returned timestamp is
// implementation-defined and the round-trip guarantee does not hold.
func SampleTime(idx, sr int) Time {
	if sr <= 0 {
		return 0
	}
	n := int64(idx)
	// ceil(n*Second/sr) via floor-division of (n*Second + sr - 1). Fast path
	// while n*Second fits int64 (|n| <= MaxInt64/1e9).
	if n >= -maxFrameNum && n <= maxFrameNum {
		return Time((n*int64(time.Second) + int64(sr) - 1) / int64(sr))
	}
	num := new(big.Int).Mul(big.NewInt(n), big.NewInt(int64(time.Second)))
	num.Add(num, big.NewInt(int64(sr)-1))
	return Time(num.Quo(num, big.NewInt(int64(sr))).Int64())
}

// SampleIndex returns the sample-frame index playing at time t for sample rate
// sr. It is the floor of t*sr (true floor, not truncation toward zero), the
// left inverse of SampleTime. Flooring matters for negative t — a sample that
// begins just before 0 maps to index -1, not 0 — so a clip placed at a negative
// or sub-sample offset gates correctly at zero and at child-mix boundaries.
func SampleIndex(t Time, sr int) int {
	if sr <= 0 {
		return 0
	}
	tt := int64(t)
	// Fast path while t*sr fits int64 (|t| <= MaxInt64/sr).
	if max := int64(math.MaxInt64) / int64(sr); tt >= -max && tt <= max {
		prod := tt * int64(sr)
		q := prod / int64(time.Second)
		// Go truncates toward zero; bias a negative non-exact quotient down to
		// the floor.
		if prod < 0 && prod%int64(time.Second) != 0 {
			q--
		}
		return int(q)
	}
	prod := new(big.Int).Mul(big.NewInt(tt), big.NewInt(int64(sr)))
	q, r := new(big.Int).QuoRem(prod, big.NewInt(int64(time.Second)), new(big.Int))
	if prod.Sign() < 0 && r.Sign() != 0 {
		q.Sub(q, big.NewInt(1))
	}
	return int(q.Int64())
}
