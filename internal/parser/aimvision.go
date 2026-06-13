package parser

import (
	"sort"
	"time"

	"github.com/f-gillmann/demolens/internal/geom"
	"github.com/golang/geo/r3"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/common"
)

// losClear: can eyeFrom see any part of an enemy at eyeTo through the map mesh.
// We sample the silhouette at head/chest/feet, both dead centre and at the
// left/right edges perpendicular to the sightline. So a shoulder or gun poking
// round a corner still counts.
func losClear(mesh *geom.Mesh, eyeFrom, eyeTo r3.Vector) bool {
	perp := eyeTo.Sub(eyeFrom).Cross(r3.Vector{X: 0, Y: 0, Z: 1})
	if perp.Norm() > 1e-6 {
		perp = perp.Normalize().Mul(16) // approx half a player width
	}
	for _, lat := range []r3.Vector{{}, perp, perp.Mul(-1)} {
		for _, dz := range []float64{0, -20, -55} {
			t := r3.Vector{X: eyeTo.X + lat.X, Y: eyeTo.Y + lat.Y, Z: eyeTo.Z + dz}
			if !mesh.Occluded(eyeFrom, t) {
				return true
			}
		}
	}
	return false
}

// engagement is per (shooter, enemy) state for two things. The crosshair
// sub-sighting: enemy enters the appearance cone, we stash appearView, and the
// move to the hit is the placement. And the TTD clock: starts when the enemy is
// first seen this life and stops at first damage. Everything resets at round
// start only, engagements never span rounds or lives.
type engagement struct {
	xIn, xPending bool          // crosshair: enemy inside the appearance cone
	appearView    r3.Vector     // shooter's view at the moment the enemy hit the cone
	tPending      bool          // TTD clock running
	consumed      bool          // already produced a TTD sample this sighting
	visSince      time.Duration // start of the current unbroken visibility window, 0 if not visible
	seeTime       time.Duration // first seen this sighting
	lastSeen      time.Duration // last frame seen, bridges brief look-aways
}

// hitNear binary-searches the chronological hit-time slice for any hit within
// windowUs of t. A shot and its impact land on the same tick, but the window
// absorbs any sub-tick drift between the fire and damage events.
func hitNear(hits []int64, t, windowUs int64) bool {
	lo, hi := 0, len(hits)
	for lo < hi { // first hit >= t-window
		mid := (lo + hi) / 2
		if hits[mid] < t-windowUs {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	return lo < len(hits) && hits[lo] <= t+windowUs
}

// los-blocking radius of a CS2 smoke, in game units
const smokeRadius = 144.0

// smokeBlocked is true if the from..to sightline clips an active smoke, i.e. the
// segment-to-sphere distance drops under smokeRadius.
func smokeBlocked(from, to r3.Vector, smokes map[int]r3.Vector) bool {
	d := to.Sub(from)
	ll := d.Dot(d)
	for _, c := range smokes {
		t := 0.0
		if ll > 0 {
			t = c.Sub(from).Dot(d) / ll
			if t < 0 {
				t = 0
			} else if t > 1 {
				t = 1
			}
		}
		if c.Sub(from.Add(d.Mul(t))).Norm() < smokeRadius {
			return true
		}
	}
	return false
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

// adaptiveTTD averages a player's time-to-damage with a trigger-discipline
// exclusion: throw out their own freakishly long duels (anything over factor x
// their median), cap whatever's left, then mean it. The rule scales to each
// player's median, so it fits fast and slow players alike. A fixed cutoff can't.
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
