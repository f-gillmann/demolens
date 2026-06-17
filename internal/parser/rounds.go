package parser

import (
	"time"

	"github.com/f-gillmann/demolens/model"
	dem "github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/common"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/events"
)

// roundKill turns a Kill event into our kill record. into is time since the
// round went live.
func roundKill(e events.Kill, into time.Duration) model.RoundKill {
	rk := model.RoundKill{
		TimeMicroseconds: into.Microseconds(),
		Headshot:         e.IsHeadshot,
		Wallbang:         e.IsWallBang(),
		Penetration:      e.PenetratedObjects,
		ThroughSmoke:     e.ThroughSmoke,
		NoScope:          e.NoScope,
		Distance:         float64(e.Distance),
		AttackerBlind:    e.AttackerBlind,
	}

	if e.Killer != nil {
		rk.Killer = e.Killer.SteamID64
		rk.KillerPosition = positionOf(e.Killer)
		rk.KillerAirborne = e.Killer.IsAirborne()
	}
	if e.Victim != nil {
		rk.Victim = e.Victim.SteamID64
		rk.VictimPosition = positionOf(e.Victim)
		rk.VictimBlind = e.Victim.IsBlinded()
		rk.VictimAirborne = e.Victim.IsAirborne()
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

func bombSite(s events.Bombsite) string {
	switch s {
	case events.BombsiteA:
		return "A"
	case events.BombsiteB:
		return "B"
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

// positionOf is a player's world position, which for a victim at the kill event
// is the death spot. Zero Position when p is nil.
func positionOf(p *common.Player) model.Position {
	if p == nil {
		return model.Position{}
	}
	return toPosition(p.Position())
}

// roundRoster makes a fresh RoundPlayer per playing SteamID with side and freeze-end
// economy. The freeze-end Loadout doubles as the round's inventory snapshot.
func (st *parseState) roundRoster(gs dem.GameState) map[uint64]*model.RoundPlayer {
	roster := map[uint64]*model.RoundPlayer{}
	for _, pl := range gs.Participants().Playing() {
		side := sideString(pl.Team)
		if side == "" {
			continue
		}

		spent := pl.MoneySpentThisRound()
		roster[pl.SteamID64] = &model.RoundPlayer{
			SteamID:    pl.SteamID64,
			Side:       side,
			MoneySpent: spent,
			StartMoney: pl.Money() + spent,
			// freeze-end value is the SEED/floor. onKill (death) and onBuyWindowClose
			// (connected survivors) overwrite it with the buy-window value. A player
			// disconnected through the buy window keeps this seed, never 0.
			EquipmentValue:   pl.EquipmentValueFreezeTimeEnd(),
			Loadout:          st.buildLoadout(pl),
			IsConnected:      pl.IsConnected,
			IsControllingBot: pl.IsControllingBot(),
		}
	}
	return roster
}
