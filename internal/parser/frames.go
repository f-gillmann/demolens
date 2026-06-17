package parser

import (
	"math"

	"github.com/f-gillmann/demolens/model"
	dem "github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/common"
)

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
		IsWalking:        pl.IsWalking(),
		InBuyZone:        pl.IsInBuyZone(),
		InBombZone:       pl.IsInBombZone(),
		Stamina:          playerProp(pl, "m_pMovementServices.m_flStamina"),
		DuckAmount:       playerProp(pl, "m_pMovementServices.m_flDuckAmount"),
	}

	if w := pl.ActiveWeapon(); w != nil {
		frame.ActiveWeapon = w.String()
	}

	return frame
}

// playerProp reads a float movement prop off the player's pawn entity, 0 when the
// prop or entity is absent (the field is omitempty, so 0 drops out).
func playerProp(pl *common.Player, prop string) float64 {
	if v, ok := propF64(pl.PlayerPawnEntity(), prop); ok {
		return v
	}
	return 0
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
