package parser

import (
	"strings"

	"github.com/f-gillmann/demolens/v2/model"
	dem "github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/common"
)

// captureTeams pins each player to a stable team id (A/B) and clan name based on
// the side they started on. Sides flip at halftime, so we set identity once and
// leave it alone. sideToTeam is the current side-to-team mapping.
func captureTeams(gs dem.GameState, players map[uint64]*model.Player, sideToTeam map[common.Team]string) {
	for _, pl := range gs.Participants().Playing() {
		rec, ok := players[pl.SteamID64]
		if !ok {
			rec = &model.Player{SteamID: pl.SteamID64, Name: pl.Name}
			players[pl.SteamID64] = rec
		}

		if rec.Team == "" {
			rec.Team = sideToTeam[pl.Team]
			if pl.TeamState != nil {
				rec.TeamName = pl.TeamState.ClanName()
			}
		}

		// Valve MM rank, 0 on third-party and GOTV demos. Last value seen wins.
		rec.Rank = pl.Rank()
		rec.RankType = pl.RankType()
		rec.CompetitiveWins = pl.CompetitiveWins()

		// Premier-only rank predictions, read straight off the controller
		// entity (same source as Rank()). Defensive int reads, 0/absent elsewhere
		// so omitempty drops them on non-Premier demos. Last value seen wins.
		if v, ok := propI(pl.Entity, "m_iCompetitiveRankingPredicted_Win"); ok {
			rec.RankIfWin = v
		}
		if v, ok := propI(pl.Entity, "m_iCompetitiveRankingPredicted_Loss"); ok {
			rec.RankIfLoss = v
		}
		if v, ok := propI(pl.Entity, "m_iCompetitiveRankingPredicted_Tie"); ok {
			rec.RankIfTie = v
		}
		if cc := pl.CrosshairCode(); cc != "" {
			rec.CrosshairCode = cc
		}

		// minimap slot color + per-player clan tag. ColorOrErr() panics on demos
		// without the prop (it uses PropertyValueMust), so read the color prop
		// defensively here. grey (-1) / unknown stay empty (omitempty drops them).
		if ci, ok := propI(pl.Entity, "m_iCompTeammateColor"); ok && common.Color(ci) != common.Grey {
			rec.Color = strings.ToLower(common.Color(ci).String())
		}
		if ct := pl.ClanTag(); ct != "" {
			rec.ClanTag = ct
		}
	}
}
