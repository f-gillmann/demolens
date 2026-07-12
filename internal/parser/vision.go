package parser

import (
	"math"
	"time"

	"github.com/f-gillmann/demolens/v2/internal/geom"
	"github.com/golang/geo/r3"
	dem "github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/common"
)

// losClear: can eyeFrom see any part of an enemy at eyeTo through the map mesh. We
// sample the silhouette at head/chest/feet, both dead centre and at the left/right
// edges perpendicular to the sightline, so a shoulder or gun round a corner counts.
var losDZ = [3]float64{0, -20, -55}

// half a player's width in game units, used to offset the silhouette edge samples
const playerHalfWidth = 16.0

func losClear(mesh *geom.Mesh, eyeFrom, eyeTo r3.Vector) bool {
	perp := eyeTo.Sub(eyeFrom).Cross(r3.Vector{X: 0, Y: 0, Z: 1})
	if perp.Norm() > 1e-6 {
		perp = perp.Normalize().Mul(playerHalfWidth)
	}
	lats := [3]r3.Vector{{}, perp, perp.Mul(-1)}

	for i := 0; i < 3; i++ {
		lat := lats[i]
		for j := 0; j < 3; j++ {
			dz := losDZ[j]
			t := r3.Vector{X: eyeTo.X + lat.X, Y: eyeTo.Y + lat.Y, Z: eyeTo.Z + dz}
			if !mesh.Occluded(eyeFrom, t) {
				return true
			}
		}
	}
	return false
}

// lateral offsets in game units: body half-width plus the real arm/elbow reach.
// capped at 20 (the widest a player hitbox actually extends, arms out); samples
// past that land in the air beside the body and trip the sighting clock early.
// Shared by the body-column losAnyPartFeet sample below.
var losAnyPartLat = [...]float64{0, 16, -16, 20, -20}

// losAnyPartFeet is the body-column silhouette test for the time-to-damage /
// crosshair sighting pass. It anchors at the enemy feet origin and walks straight
// up the real standing body, so the sample heights track the body at any view
// pitch: an eye-relative column drifts off the body on steep maps and undershoots
// the vertical angle. For each lateral offset perpendicular to the sightline
// (gun/shoulder reach, same set as the silhouette edge samples) and each height
// above the feet spanning the standing body 0..~64, it returns true if any sampled
// point is unoccluded. Kept separate from losClear so the spotted / counter-strafe
// / spray gates stay on their calibrated 9-point sampling.
var losAnyPartFeetZ = [...]float64{4, 16, 28, 40, 52, 64}

func losAnyPartFeet(mesh *geom.Mesh, eyeFrom, eyeFeet r3.Vector) bool {
	perp := eyeFeet.Sub(eyeFrom).Cross(r3.Vector{X: 0, Y: 0, Z: 1})
	if perp.Norm() > 1e-6 {
		perp = perp.Normalize()
	}
	for _, lw := range losAnyPartLat {
		lat := perp.Mul(lw)
		for _, z := range losAnyPartFeetZ {
			t := r3.Vector{X: eyeFeet.X + lat.X, Y: eyeFeet.Y + lat.Y, Z: eyeFeet.Z + z}
			if !mesh.Occluded(eyeFrom, t) {
				return true
			}
		}
	}
	return false
}

// losTorso is the strictest aim-debug LOS probe: only the body-centre column at
// chest / stomach / lower-chest heights, no gun/shoulder lateral reach, so it flips
// to "seen" a touch later than losAnyPart. Not wired into any metric; filled only
// when AimDebugPath is set. True if ANY sampled point is unoccluded.
var losTorsoDZ = [...]float64{-8, -24, -44}

func losTorso(mesh *geom.Mesh, eyeFrom, eyeTo r3.Vector) bool {
	for _, dz := range losTorsoDZ {
		t := r3.Vector{X: eyeTo.X, Y: eyeTo.Y, Z: eyeTo.Z + dz}
		if !mesh.Occluded(eyeFrom, t) {
			return true
		}
	}
	return false
}

// los-blocking radius of a CS2 smoke, in game units
const smokeRadius = 144.0

// visual lifetime of a CS2 smoke cloud from detonation: full density until
// ~16s, edge-thinning until it is gone at 18s. The SmokeExpired entity event
// fires ~22s in, ~4s after the cloud visually disappeared, so occlusion must
// track this clock and not the entity's.
const (
	smokeFullMs = 16000.0
	smokeGoneMs = 18000.0
)

// smokeBlocked is true if the from..to sightline clips an active smoke. Clouds
// with decoded voxel data are tested against their exact networked shape, and
// their lifetime comes from the stream itself (blocking ends when the fade
// phase flips). Clouds without it fall back to the sphere: segment-to-sphere
// distance under smokeRadius at full density, tapering across the thinning
// window.
func smokeBlocked(from, to r3.Vector, smokes map[int]activeSmoke, now time.Duration) bool {
	d := to.Sub(from)
	ll := d.Dot(d)
	for _, s := range smokes {
		if s.vox != nil && s.vox.ready {
			if !s.vox.fading && s.vox.blocked(from, to) {
				return true
			}
			continue
		}
		ageMs := float64((now - s.start).Microseconds()) / 1000
		r := smokeRadius
		if ageMs >= smokeGoneMs {
			continue
		} else if ageMs > smokeFullMs {
			r *= (smokeGoneMs - ageMs) / (smokeGoneMs - smokeFullMs)
		}
		c := s.pos
		t := 0.0
		if ll > 0 {
			t = c.Sub(from).Dot(d) / ll
			if t < 0 {
				t = 0
			} else if t > 1 {
				t = 1
			}
		}
		if c.Sub(from.Add(d.Mul(t))).Norm() < r {
			return true
		}
	}
	return false
}

// los-blocking radius of a single active inferno fire cell, in game units. A short
// physical flame would suggest a lower volume, but empirically a generous spherical
// block matches the reference best (the reference treats molotov fire as a tall
// occluder, and the block also stands in for the fire's smoke/visual obscuration we
// do not model). A shorter anisotropic volume was tested and regressed the match.
const fireRadius = 60.0

// fireBlocked is true if the from..to sightline passes through an active inferno's
// fire, i.e. the segment-to-cell closest distance drops under fireRadius for any
// live fire cell. Mirrors smokeBlocked's closest-point-on-segment math; cells is the
// precomputed set of active fire-cell positions across all live infernos.
func fireBlocked(from, to r3.Vector, cells []r3.Vector) bool {
	d := to.Sub(from)
	ll := d.Dot(d)
	for _, c := range cells {
		t := 0.0
		if ll > 0 {
			t = c.Sub(from).Dot(d) / ll
			if t < 0 {
				t = 0
			} else if t > 1 {
				t = 1
			}
		}
		if c.Sub(from.Add(d.Mul(t))).Norm() < fireRadius {
			return true
		}
	}
	return false
}

// vertical span from a player's eye down to their feet, in game units, for the
// body-occlusion capsule
const playerBodyHeight = 64.0

// playerBlocked is true if any alive blocker's body intersects the from..to
// sightline. Each blocker is modelled as a vertical capsule whose axis runs from
// the feet (eye minus playerBodyHeight) up to the eye, with radius playerHalfWidth;
// the sightline is blocked when its closest distance to that axis drops under the
// radius. skipA and skipB are the shooter and target ids, excluded so only third
// parties (teammate or enemy) count.
func playerBlocked(from, to r3.Vector, blockers []pv, skipA, skipB uint64) bool {
	for i := range blockers {
		b := blockers[i]
		if b.id == skipA || b.id == skipB {
			continue
		}
		feet := r3.Vector{X: b.eye.X, Y: b.eye.Y, Z: b.eye.Z - playerBodyHeight}
		if segSegDist(from, to, feet, b.eye) < playerHalfWidth {
			return true
		}
	}
	return false
}

// segSegDist returns the shortest distance between segment p1..q1 and segment
// p2..q2, via the standard clamped closest-points solve.
func segSegDist(p1, q1, p2, q2 r3.Vector) float64 {
	d1 := q1.Sub(p1)
	d2 := q2.Sub(p2)
	r := p1.Sub(p2)
	a := d1.Dot(d1)
	e := d2.Dot(d2)
	f := d2.Dot(r)

	const eps = 1e-9
	var s, t float64
	switch {
	case a <= eps && e <= eps:
		// both segments degenerate to points
		return p1.Sub(p2).Norm()
	case a <= eps:
		// first segment degenerates to a point
		t = clamp01(f / e)
	case e <= eps:
		// second segment degenerates to a point
		s = clamp01(-d1.Dot(r) / a)
	default:
		c := d1.Dot(r)
		b := d1.Dot(d2)
		denom := a*e - b*b
		if denom > eps {
			s = clamp01((b*f - c*e) / denom)
		}
		t = (b*s + f) / e
		if t < 0 {
			t = 0
			s = clamp01(-c / a)
		} else if t > 1 {
			t = 1
			s = clamp01((b - c) / a)
		}
	}
	c1 := p1.Add(d1.Mul(s))
	c2 := p2.Add(d2.Mul(t))
	return c1.Sub(c2).Norm()
}

// clamp01 clamps x into the [0,1] range.
func clamp01(x float64) float64 {
	if x < 0 {
		return 0
	}
	if x > 1 {
		return 1
	}
	return x
}

// seesTarget is true when target is in shooter's vision now: on screen with
// clear los and no smoke, or seen within the last recentSightingMs.
func seesTarget(shooter, target *common.Player, mesh *geom.Mesh, smokes map[int]activeSmoke, eng map[[2]uint64]*engagement, fovHalfDeg float64, now time.Duration, recentSightingMs float64) bool {
	eyes, ok := shooter.PositionEyes()
	if !ok || mesh == nil {
		return false
	}
	pos, ok := target.PositionEyes()
	if !ok {
		return false
	}

	if enemyInFrustum(viewVector(shooter), pos.Sub(eyes), fovHalfDeg) && losClear(mesh, eyes, pos) && !smokeBlocked(eyes, pos, smokes, now) {
		return true
	}

	if recentSightingMs > 0 {
		if en := eng[[2]uint64{shooter.SteamID64, target.SteamID64}]; en != nil && en.lastSeen > 0 &&
			float64((now-en.lastSeen).Microseconds())/1000 <= recentSightingMs {
			return true
		}
	}
	return false
}

// shooterHasVision is true if any living enemy is in vision (per seesTarget).
// This is the gate for spotted accuracy, counter-strafe and spray.
func shooterHasVision(gs dem.GameState, shooter *common.Player, mesh *geom.Mesh, smokes map[int]activeSmoke, eng map[[2]uint64]*engagement, fovHalfDeg float64, now time.Duration, recentSightingMs float64) bool {
	for _, e := range playingStable(gs) {
		if e.Team == shooter.Team || !e.IsAlive() {
			continue
		}
		if seesTarget(shooter, e, mesh, smokes, eng, fovHalfDeg, now, recentSightingMs) {
			return true
		}
	}
	return false
}

// viewVector turns a player's view angles into an eye-direction unit vector.
func viewVector(p *common.Player) r3.Vector {
	yaw := float64(p.ViewDirectionX()) * math.Pi / 180
	pitch := float64(p.ViewDirectionY()) * math.Pi / 180
	return r3.Vector{X: math.Cos(pitch) * math.Cos(yaw), Y: math.Cos(pitch) * math.Sin(yaw), Z: -math.Sin(pitch)}
}

// enemyInFrustum checks whether dir (enemy eyes minus shooter eyes) lands inside
// the view frustum: within hHalfDeg horizontally and the matching 16:9 vertical
// half-angle. This is genuinely "on screen", not a circular cone.
func enemyInFrustum(viewFwd, dir r3.Vector, hHalfDeg float64) bool {
	f := dir.Dot(viewFwd)
	if f <= 0 {
		return false // behind the player
	}
	right := viewFwd.Cross(r3.Vector{X: 0, Y: 0, Z: 1})
	if right.Norm() < 1e-9 {
		return false // looking straight up/down
	}
	right = right.Normalize()
	up := right.Cross(viewFwd).Normalize()

	// derive vertical half-angle from the horizontal one in tangent space, 16:9
	vHalfDeg := math.Atan(math.Tan(hHalfDeg*math.Pi/180)*9.0/16.0) * 180 / math.Pi
	h := math.Atan2(dir.Dot(right), f) * 180 / math.Pi
	v := math.Atan2(dir.Dot(up), f) * 180 / math.Pi
	return math.Abs(h) <= hHalfDeg && math.Abs(v) <= vHalfDeg
}

// frustumVHalfDeg is enemyInFrustum's 16:9 vertical half-angle derivation, split
// out so the per-frame pair loop computes it once (it depends only on the fov).
func frustumVHalfDeg(hHalfDeg float64) float64 {
	return math.Atan(math.Tan(hHalfDeg*math.Pi/180)*9.0/16.0) * 180 / math.Pi
}

// frustumBasis is a shooter's precomputed view basis for enemyInFrustum's test:
// the basis depends only on the shooter's view vector, so the pair loop derives
// it once per shooter instead of once per enemy. ok is false for the degenerate
// straight up/down view, where the original test always answers false.
type frustumBasis struct {
	fwd, right, up r3.Vector
	ok             bool
}

func makeFrustumBasis(viewFwd r3.Vector) frustumBasis {
	right := viewFwd.Cross(r3.Vector{X: 0, Y: 0, Z: 1})
	if right.Norm() < 1e-9 {
		return frustumBasis{}
	}
	right = right.Normalize()
	up := right.Cross(viewFwd).Normalize()
	return frustumBasis{fwd: viewFwd, right: right, up: up, ok: true}
}

// contains mirrors enemyInFrustum exactly (same operations, same order) over the
// precomputed basis, so its boolean matches the original bit for bit.
func (fb frustumBasis) contains(dir r3.Vector, hHalfDeg, vHalfDeg float64) bool {
	f := dir.Dot(fb.fwd)
	if f <= 0 {
		return false // behind the player
	}
	if !fb.ok {
		return false // looking straight up/down
	}
	h := math.Atan2(dir.Dot(fb.right), f) * 180 / math.Pi
	v := math.Atan2(dir.Dot(fb.up), f) * 180 / math.Pi
	return math.Abs(h) <= hHalfDeg && math.Abs(v) <= vHalfDeg
}
