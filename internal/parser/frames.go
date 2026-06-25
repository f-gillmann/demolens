package parser

import (
	"math"

	"github.com/f-gillmann/demolens/v2/model"
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
func (st *parseState) playerFrame(pl *common.Player, side string, into int64) model.PlayerFrame {
	return model.PlayerFrame{
		TimeMicroseconds: into,
		SteamID:          pl.SteamID64,
		Side:             side,
		Position:         toPosition(pl.Position()),
		Velocity:         st.frameVelocity(pl),
		Yaw:              round2(float64(pl.ViewDirectionX())),
		Pitch:            round2(float64(pl.ViewDirectionY())),
		Health:           pl.Health(),
		Armor:            pl.Armor(),
		Money:            pl.Money(),
		IsAlive:          pl.IsAlive(),
		IsAirborne:       pl.IsAirborne(),
		IsScoped:         pl.IsScoped(),
		IsDucking:        pl.IsDucking(),
		HasDefuseKit:     pl.HasDefuseKit(),
		ActiveWeapon:     st.activeWeapon(pl),
		IsWalking:        pl.IsWalking(),
		InBuyZone:        pl.IsInBuyZone(),
		InBombZone:       pl.IsInBombZone(),
		Stamina:          playerProp(pl, "m_pMovementServices.m_flStamina"),
		DuckAmount:       playerProp(pl, "m_pMovementServices.m_flDuckAmount"),
		Place:            pl.LastPlaceName(),
	}
}

// frameVelocity returns the player's instantaneous velocity vector (units/sec).
// CS2 doesn't network m_vecVelocity on the pawn (verified absent), so onSpeedSample
// derives it from the per-frame position delta, the same source as KillerSpeed.
// nil before the player's first delta is known (dropped by omitempty).
func (st *parseState) frameVelocity(pl *common.Player) *model.Position {
	if v, ok := st.frames.playerVelocity[pl.SteamID64]; ok {
		return &v
	}
	return nil
}

// activeWeapon resolves the frame's active weapon and never returns empty. The
// engine reports no active weapon on weapon-switch/defuse/dead ticks, so it falls
// back to a sentinel ("defuse_kit"/"c4") or the player's last-known weapon.
func (st *parseState) activeWeapon(pl *common.Player) string {
	if w := pl.ActiveWeapon(); w != nil {
		if name := w.String(); name != "" {
			st.frames.lastActiveWeapon[pl.SteamID64] = name
			return name
		}
	}
	if pl.IsDefusing {
		return "defuse_kit"
	}
	if pl.IsPlanting || carryingBomb(pl) {
		return "c4"
	}
	return st.frames.lastActiveWeapon[pl.SteamID64]
}

// carryingBomb reports whether the player holds the C4 in their inventory. The
// planted bomb is its own entity, so only the live carrier matches.
func carryingBomb(pl *common.Player) bool {
	for _, w := range pl.Weapons() {
		if w.Type == common.EqBomb {
			return true
		}
	}
	return false
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
