package parser

import (
	"fmt"
	"strconv"
	"time"

	"github.com/f-gillmann/demolens/v2/internal/geom"
	"github.com/f-gillmann/demolens/v2/model"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/common"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/events"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/msg"
)

// defaultBuyTimeSec is the mp_buytime fallback when the convar is missing or
// unparseable. 20s matches tier-1 and most MM demos.
const defaultBuyTimeSec = 20

// buyTime reads mp_buytime (integer seconds as a string) from the round's convars
// and returns it as a Duration, falling back to defaultBuyTimeSec.
func buyTime(convars map[string]string) time.Duration {
	sec := defaultBuyTimeSec
	if v, err := strconv.Atoi(convars["mp_buytime"]); err == nil {
		sec = v
	}
	return time.Duration(sec) * time.Second
}

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

// finalize flushes the current round (including post-round damage/shots) into the
// match. It assembles shot_stats and, when their streams are on, the round-end
// inventory and ground-item snapshots before appending.
func (st *parseState) finalize() {
	if st.pending == nil {
		return
	}
	// economy is derived here, after onKill (death-cap) and onBuyWindowClose
	// (survivors) have locked each RoundPlayer's buy-window-close equipment value.
	st.pending.Economy = roundEconomy(st.pendingPlayers)
	st.pending.Players = finalizeRoundPlayers(st.pendingPlayers)
	st.applyFlashAlpha()
	st.pending.Grenades = finalizeGrenades(st.grenades.pendingGrenades)
	st.pending.ShotStats = finalizeShotStats(st.shotStats, st.pending.Damages)
	st.snapshotInventories("round_end")
	st.flushGroundItems() // any ground stints still open after the post-round
	sortStreams(st.pending.Streams, st.framePeriod)
	st.match.Rounds = append(st.match.Rounds, *st.pending)
	st.pending = nil
	st.pendingPlayers = nil
	st.grenades.pendingGrenades = nil
	st.grenades.entityToUnique = nil
	st.grenades.liveInfernos = nil
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
func (st *parseState) onTeamSideSwitch(_ events.TeamSideSwitch) {
	if st.parsed.GameState().IsWarmupPeriod() {
		return
	}
	ct, t := common.TeamCounterTerrorists, common.TeamTerrorists
	st.sideToTeam[ct], st.sideToTeam[t] = st.sideToTeam[t], st.sideToTeam[ct]
}

// onFreezetimeEnd means buys are done and the round goes live. finalize the
// previous round first (so its post-round events make it in), then open a new one.
func (st *parseState) onFreezetimeEnd(_ events.RoundFreezetimeEnd) {
	gs := st.parsed.GameState()
	if gs.IsWarmupPeriod() {
		return
	}
	st.finalize()

	if st.match.Meta.GameMode == "" {
		st.match.Meta.GameMode = gameMode(gs.Rules().ConVars())
	}
	if st.opts.MapsDir != "" && !st.vision.meshTried {
		st.vision.meshTried = true
		st.vision.mesh, _ = geom.Load(geom.MapFile(st.opts.MapsDir, st.match.Meta.WorkshopID, st.match.Meta.MapName))
	}

	st.drainLOS()                                       // pending sighting frames reference the old round's engagements
	st.vision.engagements = map[[2]uint64]*engagement{} // none of these carry across rounds
	st.grenades.flashLead = map[uint64]pendingFlash{}
	st.grenades.flashAlpha = map[flashAlphaKey]float64{}
	st.vision.activeSmokes = map[int]activeSmoke{}
	st.dmgToVictim = map[uint64]int{}
	st.roundStart = st.parsed.CurrentTime()
	// buy window: go-live + mp_buytime. read the convar fresh each round (it can
	// change per round); GameRules has no BuyTime() accessor.
	st.econ.buyDeadline = st.roundStart + buyTime(gs.Rules().ConVars())
	st.econ.buyCaptured = map[uint64]bool{}
	st.econ.buyWindowClosed = false
	st.pending = &model.Round{
		Number: len(st.match.Rounds) + 1,
	}
	// explicit phase markers: the round timeline is relative to go-live, so freeze
	// ends at 0 and the freeze (round) start is the negative offset back to the
	// matching RoundStart. 0 when that start is unknown (e.g. the opening round).
	st.pending.FreezeEndMs = 0
	if st.freezeStart > 0 && st.freezeStart <= st.roundStart {
		st.pending.RoundStartMs = (st.freezeStart - st.roundStart).Milliseconds()
	}
	captureTeams(gs, st.players, st.sideToTeam)
	st.pendingPlayers = st.roundRoster(gs)
	st.grenades.pendingGrenades = map[int64]*parseGrenade{}
	st.grenades.entityToUnique = map[int]int64{}
	st.grenades.liveInfernos = map[int64]*liveInferno{}

	// reset the per-round accumulators.
	st.shotStats = map[uint64]map[string]*shotStatAcc{}
	st.grenades.grenadeSeq = 0
	st.lastInvHash = map[uint64]string{}
	st.firstContact = false
	st.groundItemsOpen = map[int]*model.DroppedItem{}
	st.groundItemSerial = map[int]int{}
	st.kitOpen = map[int]*kitStint{}
	st.kitHad = map[uint64]bool{}
	st.deathTimes = map[uint64]int64{}

	st.roundLive = true
	st.framePhase = phaseLive
	st.flushPreroll()
}

// flushPreroll rebases the buffered freezetime frames onto the new round's timeline
// (negative time = before go-live) and appends those within prerollWindow to the
// round's positions stream, then clears the buffer. Called once the new pending and
// roundStart are set. The per-frame state was captured at buffer time, which is what
// we want; only the timestamp is rewritten here.
func (st *parseState) flushPreroll() {
	if len(st.prerollBuf) == 0 {
		return
	}
	cutoff := st.roundStart - prerollWindow
	if streams := st.ensureStreams(); streams != nil {
		for _, b := range st.prerollBuf {
			if b.abs < cutoff {
				continue
			}
			b.frame.TMs = (b.abs - st.roundStart).Milliseconds()
			streams.Positions = append(streams.Positions, b.frame)
		}
	}
	st.prerollBuf = nil
}

// onRoundStart: the next round's freezetime starting means the exit window just
// closed. record how long it was open.
func (st *parseState) onRoundStart(_ events.RoundStart) {
	if st.parsed.GameState().IsWarmupPeriod() {
		return
	}
	// the upcoming round's freezetime begins: start buffering its pre-roll and stop
	// attaching frames to the previous round (still the current pending until its
	// finalize at the next freezetime end). Stamp this as the next round's freeze
	// (round) start for the round_start_t marker.
	st.framePhase = phaseFreeze
	st.prerollBuf = nil
	st.freezeStart = st.parsed.CurrentTime()
	if st.pending == nil || st.roundLive {
		return
	}
	st.pending.PostRoundMs = (st.parsed.CurrentTime() - st.roundEndTime).Milliseconds()
}

func (st *parseState) onRoundEnd(end events.RoundEnd) {
	if st.parsed.GameState().IsWarmupPeriod() || st.pending == nil {
		return
	}
	st.pending.WinnerSide = sideString(end.Winner)
	st.pending.WinnerTeam = st.sideToTeam[end.Winner]
	st.pending.Reason = reasonString(end.Reason)
	st.pending.RoundEndMs = st.roundMs()
	st.roundEndTime = st.parsed.CurrentTime()
	st.roundLive = false // post-round: damage/shots/exit-kills still count, K/D doesn't
	st.framePhase = phasePost
	// ground items keep polling through the post-round (exit-frag drops), so the
	// open ground stints are flushed at finalize rather than here, to avoid re-opening
	// and double-counting guns that stay down across round end.
}

// snapshotInventories appends an inventory-change-log entry per playing player for
// the given phase. A per-player fingerprint skips unchanged inventories, so the log
// only carries actual changes. Entries are sorted at finalize for determinism.
func (st *parseState) snapshotInventories(phase string) {
	if !st.opts.Inventory || st.pending == nil {
		return
	}
	streams := st.ensureStreams()
	if streams == nil {
		return
	}
	into := st.roundMs()
	for _, pl := range st.parsed.GameState().Participants().Playing() {
		side := sideString(pl.Team)
		if side == "" {
			continue
		}
		ic := st.inventorySnapshot(pl, side, phase, into)
		fp := inventoryFingerprint(ic)
		if st.lastInvHash[pl.SteamID64] == fp {
			continue // nothing changed since the last snapshot for this player
		}
		st.lastInvHash[pl.SteamID64] = fp
		streams.Inventory = append(streams.Inventory, ic)
	}
}

// inventoryFingerprint is the change-detection key for an inventory snapshot: the
// held set plus money/armor/health. Phase and time are excluded so an unchanged
// loadout at a later phase is skipped.
func inventoryFingerprint(ic model.InventoryChange) string {
	var b []byte
	for _, w := range ic.Weapons {
		b = append(b, fmt.Sprintf("w%s:%d|", w.Name, w.Count)...)
	}
	for _, g := range ic.Grenades {
		b = append(b, fmt.Sprintf("g%s:%d|", g.Name, g.Count)...)
	}
	for _, e := range ic.Equipment {
		b = append(b, fmt.Sprintf("e%s:%d|", e.Name, e.Count)...)
	}
	b = append(b, fmt.Sprintf("h%d.a%d.m%d.k%t.d%t", ic.Health, ic.Armor, ic.Money, ic.HasHelmet, ic.HasDefuseKit)...)
	return string(b)
}
