package parser

import (
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/f-gillmann/demolens/v2/internal/csdata"
	"github.com/f-gillmann/demolens/v2/internal/demosource"
	"github.com/f-gillmann/demolens/v2/internal/geom"
	"github.com/f-gillmann/demolens/v2/model"
	"github.com/golang/geo/r3"
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
// inventory and dropped-weapon snapshots before appending.
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
	st.flushDroppedWeapons() // any ground stints still open after the post-round
	sortStreams(st.pending.Streams)
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
		st.match.Meta.GameMode = demosource.GameMode(gs.Rules().ConVars())
	}
	if st.opts.MapsDir != "" && !st.vision.meshTried {
		st.vision.meshTried = true
		st.vision.mesh, _ = geom.Load(geom.MapFile(st.opts.MapsDir, st.match.Meta.WorkshopID, st.match.Meta.MapName))
	}

	st.vision.engagements = map[[2]uint64]*engagement{} // none of these carry across rounds
	st.grenades.flashLead = map[uint64]pendingFlash{}
	st.grenades.flashAlpha = map[flashAlphaKey]float64{}
	st.vision.activeSmokes = map[int]r3.Vector{}
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
	st.droppedOpen = map[int]*model.DroppedWeapon{}

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
			b.frame.TimeMicroseconds = (b.abs - st.roundStart).Microseconds()
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
	// finalize at the next freezetime end).
	st.framePhase = phaseFreeze
	st.prerollBuf = nil
	if st.pending == nil || st.roundLive {
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
	st.pending.Bomb.PlantTimeMicroseconds = st.roundMicros()
	if e.Player != nil {
		st.track(e.Player.SteamID64, e.Player.Name)
		st.pending.Bomb.Planter = e.Player.SteamID64
		st.pending.Bomb.PlantPosition = positionOf(e.Player)
	}
	st.snapshotInventories("bomb_plant")
}

// onBombDefuseStart logs a started defuse (fake or real) as an open attempt on the
// current round's bomb. Defuse only happens post-plant, so the bomb exists.
func (st *parseState) onBombDefuseStart(e events.BombDefuseStart) {
	if st.pending == nil || st.pending.Bomb == nil {
		return
	}
	att := model.DefuseAttempt{
		TimeMicroseconds: st.roundMicros(),
		HasKit:           e.HasKit,
	}
	if e.Player != nil {
		st.track(e.Player.SteamID64, e.Player.Name)
		att.Defuser = e.Player.SteamID64
	}
	st.pending.Bomb.DefuseAttempts = append(st.pending.Bomb.DefuseAttempts, att)
}

// onBombDefuseAborted marks the last still-open (non-aborted, uncompleted) attempt
// as aborted: the defuser started then cancelled or got forced off.
func (st *parseState) onBombDefuseAborted(_ events.BombDefuseAborted) {
	if st.pending == nil || st.pending.Bomb == nil {
		return
	}
	if att := lastOpenDefuse(st.pending.Bomb.DefuseAttempts); att != nil {
		att.Aborted = true
	}
}

func (st *parseState) onBombDefused(e events.BombDefused) {
	if st.pending == nil || st.pending.Bomb == nil {
		return
	}
	st.pending.Bomb.Defused = true
	st.pending.Bomb.DefuseTimeMicroseconds = st.roundMicros()
	if e.Player != nil {
		st.track(e.Player.SteamID64, e.Player.Name)
		st.pending.Bomb.Defuser = e.Player.SteamID64
		st.pending.Bomb.DefusePosition = positionOf(e.Player)
	}
	// pull the successful defuse's start/kit from its matching open attempt.
	if att := lastOpenDefuse(st.pending.Bomb.DefuseAttempts); att != nil {
		st.pending.Bomb.DefuseStartTimeMicroseconds = att.TimeMicroseconds
		st.pending.Bomb.DefuseHasKit = att.HasKit
	}
}

// lastOpenDefuse returns the last attempt that is neither aborted, scanning from the
// end. nil when none are open.
func lastOpenDefuse(attempts []model.DefuseAttempt) *model.DefuseAttempt {
	for i := len(attempts) - 1; i >= 0; i-- {
		if !attempts[i].Aborted {
			return &attempts[i]
		}
	}
	return nil
}

func (st *parseState) onBombExplode(_ events.BombExplode) {
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
	st.pending.RoundEndMicroseconds = st.roundMicros()
	st.roundEndTime = st.parsed.CurrentTime()
	st.roundLive = false // post-round: damage/shots/exit-kills still count, K/D doesn't
	st.framePhase = phasePost
	// dropped weapons keep polling through the post-round (exit-frag drops), so the
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
	into := st.roundMicros()
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

// onDroppedWeaponsPoll tracks each gun-on-the-ground stint by polling weapon owner
// transitions every frame (CS2 has no ground/pickup event). A gun with no owner
// opens an interval; the same gun regaining an owner closes it as a pickup. Keyed
// by the weapon entity id, which is stable for the life of the entity and reset
// each round. Still-open intervals are flushed at round end.
func (st *parseState) onDroppedWeaponsPoll(_ events.FrameDone) {
	// run during the live round and the post-round (exit-frag drops), but not during
	// freezetime, where pending is the previous round.
	if !st.opts.DroppedWeapons || st.pending == nil || (!st.roundLive && st.framePhase != phasePost) {
		return
	}
	for _, w := range st.parsed.GameState().Weapons() {
		if w == nil || w.Entity == nil || !csdata.IsGun(w) {
			continue // only guns; grenades, c4 and kits are not "dropped guns"
		}
		id := w.Entity.ID()
		if w.Owner == nil {
			if st.droppedOpen[id] != nil {
				continue // already tracking this ground stint
			}
			lastOwner := st.weaponPrevOwner(w)
			st.droppedOpen[id] = &model.DroppedWeapon{
				Weapon:                w.String(),
				Class:                 csdata.EquipmentClassName(w.Class()),
				Position:              equipmentPosition(w),
				DroppedAtMicroseconds: st.roundMicros(),
				LastOwner:             lastOwner,
				OnDeath:               st.playerDead(lastOwner),
				AmmoMagazine:          w.AmmoInMagazine(),
				AmmoReserve:           w.AmmoReserve(),
			}
			continue
		}
		// held again: close the open ground stint as a pickup.
		if dw := st.droppedOpen[id]; dw != nil {
			dw.PickedUpAtMicroseconds = st.roundMicros()
			dw.PickedUpBy = w.Owner.SteamID64
			st.appendDroppedWeapon(dw)
			delete(st.droppedOpen, id)
		}
	}
}

// flushDroppedWeapons appends every still-open ground stint to the round stream
// (no picked_up_* = still down at round end) and clears the open map.
func (st *parseState) flushDroppedWeapons() {
	if !st.opts.DroppedWeapons {
		return
	}
	for _, dw := range st.droppedOpen {
		st.appendDroppedWeapon(dw)
	}
	st.droppedOpen = map[int]*model.DroppedWeapon{}
}

// appendDroppedWeapon adds a finished ground stint to the round's dropped_weapons
// stream, lazily allocating the stream holder.
func (st *parseState) appendDroppedWeapon(dw *model.DroppedWeapon) {
	if streams := st.ensureStreams(); streams != nil {
		streams.DroppedWeapons = append(streams.DroppedWeapons, *dw)
	}
}

// playerDead reports whether the player with this SteamID64 is currently not alive.
// Unknown id (0 or not found among playing players) reports false.
func (st *parseState) playerDead(id uint64) bool {
	if id == 0 {
		return false
	}
	for _, pl := range st.parsed.GameState().Participants().Playing() {
		if pl.SteamID64 == id {
			return !pl.IsAlive()
		}
	}
	return false
}

// sortStreams sorts the map-derived stream slices for deterministic output. shots
// are appended in event order already; positions, grenade_paths, inventory and
// dropped-weapons are built by ranging maps and need a stable order. No-op when nil.
func sortStreams(s *model.RoundStreams) {
	if s == nil {
		return
	}
	sort.SliceStable(s.Positions, func(i, j int) bool {
		a, b := s.Positions[i], s.Positions[j]
		if a.TimeMicroseconds != b.TimeMicroseconds {
			return a.TimeMicroseconds < b.TimeMicroseconds
		}
		return a.SteamID < b.SteamID
	})
	s.Positions = compressPositions(s.Positions)
	sort.SliceStable(s.GrenadePaths, func(i, j int) bool {
		return s.GrenadePaths[i].GrenadeID < s.GrenadePaths[j].GrenadeID
	})
	sort.SliceStable(s.Inventory, func(i, j int) bool {
		a, b := s.Inventory[i], s.Inventory[j]
		if a.SteamID != b.SteamID {
			return a.SteamID < b.SteamID
		}
		if a.TimeMicroseconds != b.TimeMicroseconds {
			return a.TimeMicroseconds < b.TimeMicroseconds
		}
		return a.Phase < b.Phase
	})
	sort.SliceStable(s.DroppedWeapons, func(i, j int) bool {
		a, b := s.DroppedWeapons[i], s.DroppedWeapons[j]
		if a.DroppedAtMicroseconds != b.DroppedAtMicroseconds {
			return a.DroppedAtMicroseconds < b.DroppedAtMicroseconds
		}
		if a.Weapon != b.Weapon {
			return a.Weapon < b.Weapon
		}
		return a.LastOwner < b.LastOwner
	})
}

// compressPositions collapses runs of byte-identical consecutive per-player frames
// into a single frame carrying a hold count (RLE). Input MUST already be time-then-
// steam_id sorted (sortStreams does this just above). Within each player's frames a
// run of identical states is folded onto one kept frame whose HoldFrames counts the
// dropped samples; a time gap larger than ~1.5 sample periods (e.g. a disconnect)
// breaks the run even when the state is identical. Output is re-sorted by time then
// steam_id so it stays deterministic and consistent with the rest of the stream.
func compressPositions(frames []model.PlayerFrame) []model.PlayerFrame {
	if len(frames) == 0 {
		return frames
	}

	// max gap that still counts as the immediate next sample of a run.
	maxGap := int64(frameSamplePeriod/time.Microsecond) * 3 / 2

	// group by steam_id, preserving the incoming time order within each group.
	order := make([]uint64, 0)
	byPlayer := make(map[uint64][]model.PlayerFrame)
	for _, f := range frames {
		if _, ok := byPlayer[f.SteamID]; !ok {
			order = append(order, f.SteamID)
		}
		byPlayer[f.SteamID] = append(byPlayer[f.SteamID], f)
	}

	out := make([]model.PlayerFrame, 0, len(frames))
	for _, id := range order {
		group := byPlayer[id]
		kept := group[0]
		lastSeen := kept.TimeMicroseconds
		for i := 1; i < len(group); i++ {
			f := group[i]
			gap := f.TimeMicroseconds - lastSeen
			if gap > 0 && gap <= maxGap && sameFrameState(&f, &kept) {
				kept.HoldFrames++
				lastSeen = f.TimeMicroseconds
				continue
			}
			out = append(out, kept)
			kept = f
			lastSeen = f.TimeMicroseconds
		}
		out = append(out, kept)
	}

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].TimeMicroseconds != out[j].TimeMicroseconds {
			return out[i].TimeMicroseconds < out[j].TimeMicroseconds
		}
		return out[i].SteamID < out[j].SteamID
	})
	return out
}

// sameFrameState reports whether two player frames carry an identical state,
// ignoring TimeMicroseconds and HoldFrames (the only fields RLE is allowed to vary
// within a run). Velocity is a pointer so the struct can't be compared with ==.
// Floats are compared exactly: they are already rounded to 2dp at populate time, so
// identical static frames compare equal.
func sameFrameState(a, b *model.PlayerFrame) bool {
	if a.SteamID != b.SteamID || a.Side != b.Side {
		return false
	}
	if a.Position != b.Position {
		return false
	}
	if a.Yaw != b.Yaw || a.Pitch != b.Pitch {
		return false
	}
	if a.Health != b.Health || a.Armor != b.Armor || a.Money != b.Money {
		return false
	}
	if a.IsAlive != b.IsAlive || a.IsAirborne != b.IsAirborne || a.IsScoped != b.IsScoped ||
		a.IsDucking != b.IsDucking || a.HasDefuseKit != b.HasDefuseKit {
		return false
	}
	if a.ActiveWeapon != b.ActiveWeapon {
		return false
	}
	if a.IsWalking != b.IsWalking || a.InBuyZone != b.InBuyZone || a.InBombZone != b.InBombZone {
		return false
	}
	if a.Stamina != b.Stamina || a.DuckAmount != b.DuckAmount || a.Place != b.Place {
		return false
	}
	if (a.Velocity == nil) != (b.Velocity == nil) {
		return false
	}
	if a.Velocity != nil && *a.Velocity != *b.Velocity {
		return false
	}
	return true
}

// finalizeShotStats flattens the per-round shot tally into the sorted shot_stats
// slice. hits are computed from this round's own damages, deduped by shot time per
// (attacker, weapon) so a wallbang/collateral counts once, matching accuracy.go.
func finalizeShotStats(tally map[uint64]map[string]*shotStatAcc, damages []model.Damage) []model.ShotStat {
	if len(tally) == 0 {
		return nil
	}

	// hits per (attacker, weapon), deduped by shot time. bullet damage only, since
	// shot_stats is about gun fire.
	type hk struct {
		id     uint64
		weapon string
	}
	seen := map[hk]map[int64]bool{}
	hits := map[hk]int{}
	for _, d := range damages {
		if d.DamageType != "bullet" || d.Attacker == 0 {
			continue
		}
		k := hk{d.Attacker, d.Weapon}
		if seen[k] == nil {
			seen[k] = map[int64]bool{}
		}
		if seen[k][d.TimeMicroseconds] {
			continue
		}
		seen[k][d.TimeMicroseconds] = true
		hits[k]++
	}

	var out []model.ShotStat
	for id, byWeapon := range tally {
		for weapon, acc := range byWeapon {
			out = append(out, model.ShotStat{
				SteamID:      id,
				Weapon:       weapon,
				Shots:        acc.shots,
				SpottedShots: acc.spotted,
				Hits:         hits[hk{id, weapon}],
			})
		}
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].SteamID != out[j].SteamID {
			return out[i].SteamID < out[j].SteamID
		}
		return out[i].Weapon < out[j].Weapon
	})
	return out
}
