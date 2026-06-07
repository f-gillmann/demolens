package parser

import (
	"time"

	"github.com/f-gillmann/demolens/model"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/common"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/events"
)

// roundKill maps a demoinfocs Kill event into our per-round kill record,
// with the time elapsed since the round went live.
func roundKill(e events.Kill, into time.Duration) model.RoundKill {
	rk := model.RoundKill{
		TimeMicroseconds: into.Microseconds(),
		Headshot:         e.IsHeadshot,
		Wallbang:         e.IsWallBang(),
		ThroughSmoke:     e.ThroughSmoke,
		NoScope:          e.NoScope,
	}
	if e.Killer != nil {
		rk.Killer = e.Killer.SteamID64
		rk.KillerPosition = positionOf(e.Killer)
	}
	if e.Victim != nil {
		rk.Victim = e.Victim.SteamID64
		rk.VictimPosition = positionOf(e.Victim)
	}
	if e.Assister != nil && !e.AssistedFlash {
		rk.Assister = e.Assister.SteamID64
	}
	if e.Weapon != nil {
		rk.Weapon = e.Weapon.String()
	}
	return rk
}

func sideString(team common.Team) string {
	switch team {
	case common.TeamCounterTerrorists:
		return "CT"
	case common.TeamTerrorists:
		return "T"
	default:
		return ""
	}
}

func reasonString(reason events.RoundEndReason) string {
	switch reason {
	case events.RoundEndReasonTargetBombed:
		return "bomb_exploded"
	case events.RoundEndReasonBombDefused:
		return "bomb_defused"
	case events.RoundEndReasonCTWin, events.RoundEndReasonTerroristsWin:
		return "elimination"
	case events.RoundEndReasonTargetSaved:
		return "time_expired"
	default:
		return "other"
	}
}

// positionOf returns a player's world position (death spot for a victim at the
// kill event), or the zero Position if the player is nil.
func positionOf(p *common.Player) model.Position {
	if p == nil {
		return model.Position{}
	}
	v := p.Position()
	return model.Position{X: v.X, Y: v.Y, Z: v.Z}
}
