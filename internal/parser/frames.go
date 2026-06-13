package parser

import (
	"math"
	"time"

	"github.com/f-gillmann/demolens/internal/geom"
	"github.com/f-gillmann/demolens/model"
	"github.com/golang/geo/r3"
	dem "github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/common"
)

// seesTarget answers "is this enemy in the shooter's vision right now". Either
// they're on screen (inside the frustum, clear los, not behind smoke), or we saw
// them in the last recentMs. We rebuild visibility from geometry because the
// engine spotted flag lags 0-500ms, and the recently-seen window keeps a shot at
// someone who just ducked behind cover counting.
func seesTarget(shooter, target *common.Player, mesh *geom.Mesh, smokes map[int]r3.Vector, eng map[[2]uint64]*engagement, hHalfDeg float64, now time.Duration, recentMs float64) bool {
	eyes, ok := shooter.PositionEyes()
	if !ok || mesh == nil {
		return false
	}
	pos, ok := target.PositionEyes()
	if !ok {
		return false
	}
	if enemyInFrustum(viewVector(shooter), pos.Sub(eyes), hHalfDeg) && losClear(mesh, eyes, pos) && !smokeBlocked(eyes, pos, smokes) {
		return true
	}
	if recentMs > 0 {
		if en := eng[[2]uint64{shooter.SteamID64, target.SteamID64}]; en != nil && en.lastSeen > 0 &&
			float64((now-en.lastSeen).Microseconds())/1000 <= recentMs {
			return true
		}
	}
	return false
}

// shooterHasVision is true if any living enemy is in vision (per seesTarget).
// This is the gate for spotted accuracy, counter-strafe and spray.
func shooterHasVision(gs dem.GameState, shooter *common.Player, mesh *geom.Mesh, smokes map[int]r3.Vector, eng map[[2]uint64]*engagement, hHalfDeg float64, now time.Duration, recentMs float64) bool {
	for _, e := range gs.Participants().Playing() {
		if e.Team == shooter.Team || !e.IsAlive() {
			continue
		}
		if seesTarget(shooter, e, mesh, smokes, eng, hHalfDeg, now, recentMs) {
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

// horizontalSpeed is the X/Y speed in units/sec between two positions dt apart.
// Z is ignored.
func horizontalSpeed(cur, prev model.Position, dt float64) float64 {
	if dt <= 0 {
		return 0
	}
	dx, dy := cur.X-prev.X, cur.Y-prev.Y
	return math.Sqrt(dx*dx+dy*dy) / dt
}

// playerFrame snapshots one player's position and state for this frame.
func playerFrame(pl *common.Player, side string, into int64) model.PlayerFrame {
	frame := model.PlayerFrame{
		TimeMicroseconds: into,
		SteamID:          pl.SteamID64,
		Side:             side,
		Position:         toPosition(pl.Position()),
		Yaw:              float64(pl.ViewDirectionX()),
		Pitch:            float64(pl.ViewDirectionY()),
		Health:           pl.Health(),
		Armor:            pl.Armor(),
		Money:            pl.Money(),
		IsAlive:          pl.IsAlive(),
		IsAirborne:       pl.IsAirborne(),
		IsScoped:         pl.IsScoped(),
		IsDucking:        pl.IsDucking(),
		HasDefuseKit:     pl.HasDefuseKit(),
	}
	if w := pl.ActiveWeapon(); w != nil {
		frame.ActiveWeapon = w.String()
	}
	return frame
}

// aliveSnapshot grabs the position of everyone alive this tick. Feeds the
// proximity metrics like trade opportunities.
func aliveSnapshot(gs dem.GameState) []model.AlivePlayer {
	var alive []model.AlivePlayer
	for _, pl := range gs.Participants().Playing() {
		if !pl.IsAlive() {
			continue
		}
		alive = append(alive, model.AlivePlayer{
			SteamID:  pl.SteamID64,
			Position: toPosition(pl.Position()),
		})
	}
	return alive
}
