package clip

import (
	"math"
	"math/big"
	"time"
)

// Rate is a rational frame rate. It is never float64: float cannot represent
// 30000/1001 and drifts over long clips, whereas frame i's timestamp stays
// exact (see FrameTime).
type Rate struct {
	Num int // e.g. 30000
	Den int // e.g. 1001
}

// Float returns the rate as a float64, for display and probing only.
func (r Rate) Float() float64 {
	if r.Den == 0 {
		return 0
	}
	return float64(r.Num) / float64(r.Den)
}

// maxFrameNum bounds i*Den before the *Second multiply overflows int64
// (~9.2e9, i.e. roughly 85h at 29.97fps with Den=1001).
const maxFrameNum = int64(math.MaxInt64) / int64(time.Second)

// FrameTime returns the exact timestamp of frame i: i*Den*Second/Num. The
// common case is plain int64; a very long clip falls back to big.Int so the
// timestamp stays exact instead of silently overflowing.
func (r Rate) FrameTime(i int) Time {
	if r.Num == 0 {
		return 0
	}
	num := int64(i) * int64(r.Den)
	if num >= -maxFrameNum && num <= maxFrameNum {
		return time.Duration(num * int64(time.Second) / int64(r.Num))
	}
	b := new(big.Int).Mul(big.NewInt(num), big.NewInt(int64(time.Second)))
	b.Quo(b, big.NewInt(int64(r.Num))) // truncate toward zero, matching the int64 path
	return time.Duration(b.Int64())
}

// epsilonNanoTicks reproduces MoviePy's +1e-5s frame-index bias in the integer
// units used by TimeToFrame: 1e-5s * Den * 1e9ns = Den * 1e4.
const epsilonNanoFactor = 10000

// TimeToFrame maps a timestamp to a source frame index when reading an existing
// file. It mirrors MoviePy's int(fps*t + 1e-5) in pure integer math:
// trunc((t_ns*Num + Den*1e4) / (Den*1e9)). Export is index-driven and never
// calls this.
func (r Rate) TimeToFrame(t Time) int {
	if r.Den == 0 {
		return 0
	}
	num := new(big.Int).Mul(big.NewInt(int64(t)), big.NewInt(int64(r.Num)))
	num.Add(num, big.NewInt(int64(r.Den)*epsilonNanoFactor))
	den := new(big.Int).Mul(big.NewInt(int64(r.Den)), big.NewInt(int64(time.Second)))
	// Quo truncates toward zero, matching Python int(): for t>=0 this is floor;
	// for t<0 it truncates like MoviePy rather than flooring to a lower frame.
	return int(new(big.Int).Quo(num, den).Int64())
}

// MaxRate returns the higher of two rates by exact cross-multiplication; ties
// keep a. big.Int avoids overflow on pathological denominators.
func MaxRate(a, b Rate) Rate {
	left := new(big.Int).Mul(big.NewInt(int64(a.Num)), big.NewInt(int64(b.Den)))
	right := new(big.Int).Mul(big.NewInt(int64(b.Num)), big.NewInt(int64(a.Den)))
	if right.Cmp(left) > 0 {
		return b
	}
	return a
}

// SnapNTSC snaps a float fps within 0.01 of a standard x*1000/1001 rate (x in
// 23,24,25,30,50) to the exact NTSC rational, matching MoviePy's correction.
// Other rates fall back to RateFromFloat.
func SnapNTSC(fps float64) Rate {
	if r, ok := SnapNTSCMatch(fps); ok {
		return r
	}
	return RateFromFloat(fps)
}

// SnapNTSCMatch reports whether fps is within 0.01 of a standard NTSC rate and,
// if so, returns the exact x*1000/1001 rational. Callers that already hold an
// exact rational use this to apply the snap only when it actually fires.
func SnapNTSCMatch(fps float64) (Rate, bool) {
	const coef = 1000.0 / 1001.0
	for _, x := range []int{23, 24, 25, 30, 50} {
		if math.Abs(fps-float64(x)*coef) < 0.01 {
			return Rate{Num: x * 1000, Den: 1001}, true
		}
	}
	return Rate{}, false
}

// RateFromFloat converts a float fps to a Rate: exact for integers, otherwise
// approximated in thousandths.
func RateFromFloat(fps float64) Rate {
	if fps == math.Trunc(fps) {
		return Rate{Num: int(fps), Den: 1}
	}
	return Rate{Num: int(math.Round(fps * 1000)), Den: 1000}
}
