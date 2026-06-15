package parser

import (
	"github.com/f-gillmann/demolens/model"
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
	}
}
