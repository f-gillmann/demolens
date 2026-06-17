package parser

import (
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/f-gillmann/demolens/internal/csdata"
	"github.com/f-gillmann/demolens/internal/demosource"
	"github.com/f-gillmann/demolens/internal/geom"
	"github.com/f-gillmann/demolens/model"
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
	st.snapshotDroppedWeapons("round_end")
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
	st.pending.Bomb.PlantTimeMicroseconds = st.roundMicros()
	if e.Player != nil {
		st.track(e.Player.SteamID64, e.Player.Name)
		st.pending.Bomb.Planter = e.Player.SteamID64
		st.pending.Bomb.PlantPosition = positionOf(e.Player)
	}
	st.snapshotInventories("bomb_plant")
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

// snapshotDroppedWeapons scans world guns (Owner == nil) and appends them to the
// dropped_weapons stream for the given phase. Entries are sorted at finalize for
// determinism.
func (st *parseState) snapshotDroppedWeapons(phase string) {
	if !st.opts.DroppedWeapons || st.pending == nil {
		return
	}
	streams := st.ensureStreams()
	if streams == nil {
		return
	}
	into := st.roundMicros()
	for _, w := range st.parsed.GameState().Weapons() {
		if w == nil || w.Owner != nil || !csdata.IsGun(w) {
			continue // only guns count; held weapons, grenades, c4 and kits are not "dropped guns"
		}
		dw := model.DroppedWeapon{
			TimeMicroseconds: into,
			Phase:            phase,
			Name:             w.String(),
			Class:            csdata.EquipmentClassName(w.Class()),
			Position:         equipmentPosition(w),
			AmmoMagazine:     w.AmmoInMagazine(),
			AmmoReserve:      w.AmmoReserve(),
			LastOwner:        st.weaponPrevOwner(w),
			OriginalOwner:    weaponOriginalOwner(w),
		}
		streams.DroppedWeapons = append(streams.DroppedWeapons, dw)
	}
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
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		ax, ay := droppedXY(a)
		bx, by := droppedXY(b)
		if ax != bx {
			return ax < bx
		}
		return ay < by
	})
}

// droppedXY is a dropped weapon's x/y for sorting, 0/0 when its position is absent.
func droppedXY(w model.DroppedWeapon) (float64, float64) {
	if w.Position == nil {
		return 0, 0
	}
	return w.Position.X, w.Position.Y
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
