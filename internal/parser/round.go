package parser

import "math"

// round2 rounds f to 2 decimals for output and clears the negative-zero sign bit
// so the JSON shows 0 instead of -0. Output-only; never feeds a metric.
func round2(f float64) float64 {
	r := math.Round(f*100) / 100
	if r == 0 {
		r = 0
	}
	return r
}

// round3 rounds f to 3 decimals for output and clears negative zero. Used for the
// spray ideal/actual display values, applied after avg_deviation is computed.
func round3(f float64) float64 {
	r := math.Round(f*1000) / 1000
	if r == 0 {
		r = 0
	}
	return r
}
