package parser

import (
	"sort"
	"time"

	"github.com/golang/geo/r3"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/common"
)

// engagement is per (shooter, enemy) state for crosshair placement and TTD; it resets at round start only.
type engagement struct {
	crosshairInFrustum bool          // crosshair: enemy currently inside the appearance cone
	crosshairPending   bool          // crosshair: a placement sample is pending its closing hit
	appearView         r3.Vector     // shooter's view at the moment the enemy hit the cone
	ttdRunning         bool          // TTD clock running
	consumed           bool          // already produced a TTD sample this sighting
	visSince           time.Duration // start of the current unbroken visibility window, 0 if not visible
	seeTime            time.Duration // first seen this sighting
	lastSeen           time.Duration // last frame seen, bridges brief look-aways
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

// median of an already-sorted slice. TTD and crosshair placement aggregate by
// median to shrug off outliers.
func median(sorted []float64) float64 {
	n := len(sorted)
	if n == 0 {
		return 0
	}
	if n%2 == 1 {
		return sorted[n/2]
	}
	return (sorted[n/2-1] + sorted[n/2]) / 2
}

// adaptiveTTD means a player's time-to-damage after dropping their own outliers (over factor x median) and capping the rest.
func adaptiveTTD(samples []float64, factor, capMs float64) float64 {
	sorted := append([]float64(nil), samples...)
	sort.Float64s(sorted)
	med := median(sorted)
	limit := factor * med

	sum, n := 0.0, 0
	for _, v := range samples {
		if v > limit {
			continue // their own trigger-discipline outlier
		}
		if v > capMs {
			v = capMs
		}
		sum += v
		n++
	}

	if n == 0 { // everything got flagged, can't happen with factor>1, just mean them
		for _, v := range samples {
			sum += v
		}
		n = len(samples)
	}

	return sum / float64(n)
}

// crosshairDelta is the angle in degrees from the appearance direction to where
// the shooter is aiming now.
func crosshairDelta(appearView r3.Vector, shooter *common.Player) float64 {
	return appearView.Angle(viewVector(shooter)).Degrees()
}
