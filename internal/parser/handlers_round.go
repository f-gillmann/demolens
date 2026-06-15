package parser

import (
	"strconv"

	"github.com/f-gillmann/demolens/internal/demosource"
	"github.com/f-gillmann/demolens/internal/geom"
	"github.com/f-gillmann/demolens/model"
	"github.com/golang/geo/r3"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/common"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/events"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/msg"
)

// track upserts a player name into the match-level map.
func (st *parseState) track(id uint64, name string) {
	pl, ok := st.players[id]
	if !ok {
		pl = &model.Player{SteamID: id, Name: name}
		st.players[id] = pl
	}
	if name != "" {
		pl.Name = name
	}
}

// finalize flushes the current round (including post-round damage/shots) into the match.
func (st *parseState) finalize() {
	if st.pending == nil {
		return
	}
	st.pending.Players = finalizeRoundPlayers(st.pendingPlayers)
	st.pending.Grenades = finalizeGrenades(st.pendingGrenades)
	st.match.Rounds = append(st.match.Rounds, *st.pending)
	st.pending = nil
	st.pendingPlayers = nil
	st.pendingGrenades = nil
	st.entityToUnique = nil
	st.liveInfernos = nil
}

func (st *parseState) onServerInfo(info *msg.CSVCMsg_ServerInfo) {
	st.match.Meta.MapName = info.GetMapName()
	st.match.Meta.IsHltv = info.GetIsHltv()
	st.match.Meta.IsDedicatedServer = info.GetIsDedicated()
	if st.match.Meta.ServerName == "" {
		st.match.Meta.ServerName = info.GetHostName()
	}
	if st.match.Meta.WorkshopID == "" {
		st.match.Meta.WorkshopID = info.GetAddonName()
	}
}

func (st *parseState) onFileHeader(header *msg.CDemoFileHeader) {
	st.match.Meta.BuildNum = strconv.Itoa(int(header.GetPatchVersion()))
	if sn := header.GetServerName(); sn != "" {
		st.match.Meta.ServerName = sn
	}
	st.match.Meta.ClientName = header.GetClientName()
	if a := header.GetAddons(); a != "" {
		st.match.Meta.WorkshopID = a
	}
}

// onTeamSideSwitch flips the side-to-team mapping at halftime and at each OT half
// so identity stays anchored to the first-half side.
func (st *parseState) onTeamSideSwitch(events.TeamSideSwitch) {
	if st.parsed.GameState().IsWarmupPeriod() {
		return
	}
	ct, t := common.TeamCounterTerrorists, common.TeamTerrorists
	st.sideToTeam[ct], st.sideToTeam[t] = st.sideToTeam[t], st.sideToTeam[ct]
}

// onFreezetimeEnd means buys are done and the round goes live. finalize the
// previous round first (so its post-round events make it in), then open a new one.
func (st *parseState) onFreezetimeEnd(events.RoundFreezetimeEnd) {
	gs := st.parsed.GameState()
	if gs.IsWarmupPeriod() {
		return
	}
	st.finalize()

	if st.match.Meta.GameMode == "" {
		st.match.Meta.GameMode = demosource.GameMode(gs.Rules().ConVars())
	}
	if st.opts.MapsDir != "" && !st.meshTried {
		st.meshTried = true
		st.mesh, _ = geom.Load(geom.MapFile(st.opts.MapsDir, st.match.Meta.WorkshopID, st.match.Meta.MapName))
	}

	st.engagements = map[[2]uint64]*engagement{} // none of these carry across rounds
	st.flashLead = map[uint64]pendingFlash{}
	st.activeSmokes = map[int]r3.Vector{}
	st.roundStart = st.parsed.CurrentTime()
	st.pending = &model.Round{
		Number:  len(st.match.Rounds) + 1,
		Economy: roundEconomy(gs),
	}
	captureTeams(gs, st.players, st.sideToTeam)
	st.pendingPlayers = roundRoster(gs)
	st.pendingGrenades = map[int64]*model.Grenade{}
	st.entityToUnique = map[int]int64{}
	st.liveInfernos = map[int64]*liveInferno{}
	st.roundLive = true
}

// onRoundStart: the next round's freezetime starting means the exit window just
// closed. record how long it was open.
func (st *parseState) onRoundStart(events.RoundStart) {
	if st.parsed.GameState().IsWarmupPeriod() || st.pending == nil || st.roundLive {
		return
	}
	st.pending.PostRoundMicroseconds = (st.parsed.CurrentTime() - st.roundEndTime).Microseconds()
}

func (st *parseState) onBombPlanted(e events.BombPlanted) {
	if st.pending == nil {
		return
	}
	if st.pending.Bomb == nil {
		st.pending.Bomb = &model.Bomb{}
	}
	st.pending.Bomb.Site = bombSite(e.Site)
	st.pending.Bomb.PlantTimeMicroseconds = (st.parsed.CurrentTime() - st.roundStart).Microseconds()
	if e.Player != nil {
		st.track(e.Player.SteamID64, e.Player.Name)
		st.pending.Bomb.Planter = e.Player.SteamID64
		st.pending.Bomb.PlantPosition = positionOf(e.Player)
	}
}

func (st *parseState) onBombDefused(e events.BombDefused) {
	if st.pending == nil || st.pending.Bomb == nil {
		return
	}
	st.pending.Bomb.Defused = true
	st.pending.Bomb.DefuseTimeMicroseconds = (st.parsed.CurrentTime() - st.roundStart).Microseconds()
	if e.Player != nil {
		st.track(e.Player.SteamID64, e.Player.Name)
		st.pending.Bomb.Defuser = e.Player.SteamID64
		st.pending.Bomb.DefusePosition = positionOf(e.Player)
	}
}

func (st *parseState) onBombExplode(events.BombExplode) {
	if st.pending != nil && st.pending.Bomb != nil {
		st.pending.Bomb.Exploded = true
	}
}

func (st *parseState) onRoundEnd(end events.RoundEnd) {
	if st.parsed.GameState().IsWarmupPeriod() || st.pending == nil {
		return
	}
	st.pending.WinnerSide = sideString(end.Winner)
	st.pending.WinnerTeam = st.sideToTeam[end.Winner]
	st.pending.Reason = reasonString(end.Reason)
	st.roundEndTime = st.parsed.CurrentTime()
	st.roundLive = false // post-round: damage/shots/exit-kills still count, K/D doesn't
}
