package parser

import (
	"sort"
	"time"

	"github.com/golang/geo/r3"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/common"
)

// engagement is per (shooter, enemy) state for crosshair placement and TTD; it resets
// at round start only. The two metrics run independent sighting machines: their
// calibrated los, fov, gap and debounce differ, so a shared "first seen" event would
// pull one or the other off its optimum.
type engagement struct {
	// TTD sighting: dense LOS (losAnyPart), fov TTDFovDeg, gap TTDGapMs, debounce TTDDebounceMs.
	ttdVisSince time.Duration
	ttdSeeTime  time.Duration
	ttdLastSeen time.Duration
	ttdRunning  bool
	ttdConsumed bool
	// crosshair sighting: dense LOS (losAnyPart), fov CrosshairFovDeg, gap CrosshairGapMs,
	// debounce CrosshairDebounceMs. appearView re-anchors at each fresh visible window (peek).
	chVisSince   time.Duration
	chLastSeen   time.Duration
	chRunning    bool
	chConsumed   bool
	chArmed      bool
	chAppearView r3.Vector
	// shared: last frame this pair was seen with a clear (losClear) unsmoked sightline.
	// Feeds only the recently-seen window in seesTarget (counter-strafe / spotted), not
	// the two sighting machines above.
	lastSeen time.Duration
}

// hitNear binary-searches the chronological hit-time slice for any hit within
// windowMicros of t. A shot and its impact land on the same tick, but the window
// absorbs any sub-tick drift between the fire and damage events.
func hitNear(hits []int64, t, windowMicros int64) bool {
	lo, hi := 0, len(hits)
	for lo < hi { // first hit >= t-window
		mid := (lo + hi) / 2
		if hits[mid] < t-windowMicros {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	return lo < len(hits) && hits[lo] <= t+windowMicros
}

// percentileInterp returns the linear-interpolated pct-th percentile (pct in 0..100)
// of an already-sorted slice, clamping the fractional index to the slice bounds.
func percentileInterp(sorted []float64, pct float64) float64 {
	n := len(sorted)
	if n == 0 {
		return 0
	}
	i := pct / 100 * float64(n-1)
	if i < 0 {
		i = 0
	}
	lo := int(i)
	hi := lo + 1
	if hi > n-1 {
		hi = n - 1
	}
	return sorted[lo] + (sorted[hi]-sorted[lo])*(i-float64(lo))
}

// ttdPercentile drops trigger-discipline samples at or above excludeMs, then returns
// the pct-th percentile time-to-damage of what's left. The floor is applied earlier,
// at sample time.
func ttdPercentile(samples []float64, excludeMs, pct float64) float64 {
	kept := make([]float64, 0, len(samples))
	for _, v := range samples {
		if v < excludeMs {
			kept = append(kept, v)
		}
	}
	if len(kept) == 0 {
		return 0
	}
	sort.Float64s(kept)
	return percentileInterp(kept, pct)
}

// lowinsorMean clamps every sample below the pct-th percentile up to that threshold,
// then returns the mean. Unlike a winsor it never drops a sample; it just keeps the
// lucky pre-aimed outliers from dragging the crosshair placement down.
func lowinsorMean(samples []float64, pct float64) float64 {
	n := len(samples)
	if n == 0 {
		return 0
	}
	s := append([]float64(nil), samples...)
	sort.Float64s(s)
	idx := int(pct / 100 * float64(n-1))
	if idx > n-1 {
		idx = n - 1
	}
	lo := s[idx]
	var sum float64
	for _, v := range samples {
		if v < lo {
			v = lo
		}
		sum += v
	}
	return sum / float64(n)
}

// crosshairDelta is the angle in degrees from the appearance direction to where
// the shooter is aiming now.
func crosshairDelta(appearView r3.Vector, shooter *common.Player) float64 {
	return appearView.Angle(viewVector(shooter)).Degrees()
}
