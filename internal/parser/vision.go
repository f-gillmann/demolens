package parser

import (
	"math"
	"time"

	"github.com/f-gillmann/demolens/internal/geom"
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

// seesTarget is true when target is in shooter's vision now: on screen with
// clear los and no smoke, or seen within the last recentSightingMs.
func seesTarget(shooter, target *common.Player, mesh *geom.Mesh, smokes map[int]r3.Vector, eng map[[2]uint64]*engagement, fovHalfDeg float64, now time.Duration, recentSightingMs float64) bool {
	eyes, ok := shooter.PositionEyes()
	if !ok || mesh == nil {
		return false
	}
	pos, ok := target.PositionEyes()
	if !ok {
		return false
	}

	if enemyInFrustum(viewVector(shooter), pos.Sub(eyes), fovHalfDeg) && losClear(mesh, eyes, pos) && !smokeBlocked(eyes, pos, smokes) {
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
func shooterHasVision(gs dem.GameState, shooter *common.Player, mesh *geom.Mesh, smokes map[int]r3.Vector, eng map[[2]uint64]*engagement, fovHalfDeg float64, now time.Duration, recentSightingMs float64) bool {
	for _, e := range gs.Participants().Playing() {
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
