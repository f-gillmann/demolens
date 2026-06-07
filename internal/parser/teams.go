package parser

import (
	"github.com/f-gillmann/demolens/model"
	dem "github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/common"
)

// captureTeams records each playing player's team id + clan name for the current round.
func captureTeams(gs dem.GameState, players map[uint64]*model.Player) {
	ctClan := gs.TeamCounterTerrorists().ClanName()
	tClan := gs.TeamTerrorists().ClanName()

	for _, pl := range gs.Participants().Playing() {
		rec, ok := players[pl.SteamID64]
		if !ok {
			rec = &model.Player{SteamID: pl.SteamID64, Name: pl.Name}
			players[pl.SteamID64] = rec
		}
		switch pl.Team {
		case common.TeamCounterTerrorists:
			rec.Team, rec.TeamName = "A", ctClan
		case common.TeamTerrorists:
			rec.Team, rec.TeamName = "B", tClan
		}
	}
}

// aliveSnapshot records the position of every player alive at the current tick,
// used for proximity-based metrics (e.g. trade opportunities).
func aliveSnapshot(gs dem.GameState) []model.AlivePlayer {
	var alive []model.AlivePlayer
	for _, pl := range gs.Participants().Playing() {
		if !pl.IsAlive() {
			continue
		}
		pos := pl.Position()
		alive = append(alive, model.AlivePlayer{
			SteamID:  pl.SteamID64,
			Position: model.Position{X: pos.X, Y: pos.Y, Z: pos.Z},
		})
	}
	return alive
}
