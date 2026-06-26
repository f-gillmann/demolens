package parser

import (
	"fmt"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/f-gillmann/demolens/v2/internal/csdata"
	"github.com/f-gillmann/demolens/v2/internal/demosource"
	"github.com/f-gillmann/demolens/v2/internal/geom"
	"github.com/f-gillmann/demolens/v2/model"
	"github.com/golang/geo/r3"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/common"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/events"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/msg"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/sendtables"
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

// onBombPlantBegin logs a started plant (real or fake) as an open attempt on the
// current round's bomb. A fake plant can happen with no completed plant ever firing,
// so the bomb is created here when it does not exist yet.
func (st *parseState) onBombPlantBegin(e events.BombPlantBegin) {
	if st.pending == nil {
		return
	}
	if st.pending.Bomb == nil {
		st.pending.Bomb = &model.Bomb{}
	}
	att := model.PlantAttempt{TMs: st.roundMs()}
	if e.Player != nil {
		st.track(e.Player.SteamID64, e.Player.Name)
		att.Planter = e.Player.SteamID64
	}
	st.pending.Bomb.PlantAttempts = append(st.pending.Bomb.PlantAttempts, att)
}

// onBombPlantAborted marks the last still-open (non-aborted, uncompleted) plant
// attempt as aborted: the planter started then cancelled (a fake plant).
func (st *parseState) onBombPlantAborted(_ events.BombPlantAborted) {
	if st.pending == nil || st.pending.Bomb == nil {
		return
	}
	if att := lastOpenPlant(st.pending.Bomb.PlantAttempts); att != nil {
		att.Aborted = true
	}
}

// lastOpenPlant returns the last plant attempt that is neither aborted nor completed,
// scanning from the end. nil when none are open. Mirrors lastOpenDefuse.
func lastOpenPlant(attempts []model.PlantAttempt) *model.PlantAttempt {
	for i := len(attempts) - 1; i >= 0; i-- {
		if !attempts[i].Aborted && !attempts[i].Completed {
			return &attempts[i]
		}
	}
	return nil
}

func (st *parseState) onBombPlanted(e events.BombPlanted) {
	if st.pending == nil {
		return
	}
	if st.pending.Bomb == nil {
		st.pending.Bomb = &model.Bomb{}
	}
	st.pending.Bomb.Site = bombSite(e.Site)
	st.pending.Bomb.PlantMs = st.roundMs()
	if e.Player != nil {
		st.track(e.Player.SteamID64, e.Player.Name)
		st.pending.Bomb.Planter = e.Player.SteamID64
		pos := positionOf(e.Player)
		st.pending.Bomb.PlantPosition = &pos
	}
	// the plant completed: close its matching open attempt so a later stray abort
	// can't re-mark the completed plant as aborted.
	if att := lastOpenPlant(st.pending.Bomb.PlantAttempts); att != nil {
		att.Completed = true
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
		TMs:    st.roundMs(),
		HasKit: e.HasKit,
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
	st.pending.Bomb.DefuseMs = st.roundMs()
	if e.Player != nil {
		st.track(e.Player.SteamID64, e.Player.Name)
		st.pending.Bomb.Defuser = e.Player.SteamID64
		pos := positionOf(e.Player)
		st.pending.Bomb.DefusePosition = &pos
	}
	// pull the successful defuse's start/kit from its matching open attempt.
	if att := lastOpenDefuse(st.pending.Bomb.DefuseAttempts); att != nil {
		st.pending.Bomb.DefuseStartedMs = att.TMs
		st.pending.Bomb.HasKit = att.HasKit
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

// onGroundItemsPoll tracks each item-on-the-ground stint by polling weapon owner
// transitions every frame (CS2 has no ground/pickup event). A gun (or the loose c4)
// with no owner opens an interval; the same entity regaining an owner closes it as a
// pickup. Keyed by the weapon entity id, which is stable for the life of the entity
// and reset each round. While a stint is open the entity's ground position is sampled
// at the positions cadence (see groundItemSampleTick), building an RLE-collapsible track
// so a nade-shoved or re-dropped item shows its new positions. Still-open intervals
// are flushed at round end.
func (st *parseState) onGroundItemsPoll(_ events.FrameDone) {
	// run during the live round and the post-round (exit-frag drops), but not during
	// freezetime, where pending is the previous round.
	if !st.opts.GroundItems || st.pending == nil || (!st.roundLive && st.framePhase != phasePost) {
		return
	}
	// re-sample the position track only during the live round: the post-round poll
	// still opens/closes stints (exit-frag drops keep their seed position) but the
	// engine mass-relocates resting weapons at round end, which is not a real shove.
	sample := st.roundLive && st.groundItemSampleTick()
	for _, w := range st.parsed.GameState().Weapons() {
		if w == nil || w.Entity == nil {
			continue
		}
		// trackable droppables (guns, grenades, zeus, knife) plus the loose c4; the
		// defuse kit is a separate prop tracked via pollKits. A dropped grenade WEAPON
		// entity is a nade lying on the ground, distinct from the in-flight projectile
		// in grenade_paths (a separate CBaseCSGrenadeProjectile), so no double-count.
		// the carried c4 has an owner (skipped below); the planted c4 is a separate
		// entity that never appears in Weapons(), so plant_position is not duplicated.
		if !csdata.IsGroundTrackable(w) && w.Type != common.EqBomb {
			continue
		}
		id := w.Entity.ID()
		serial := w.Entity.SerialNum()
		if w.Owner == nil {
			dw := st.groundItemsOpen[id]
			if dw == nil {
				st.groundItemsOpen[id] = st.openGroundStint(w)
				st.groundItemSerial[id] = serial
				continue
			}
			if st.groundItemSerial[id] != serial {
				// the entity slot was reused by a different physical weapon (Source 2
				// recycles ids): close the old stint and open a fresh one so the track
				// doesn't teleport from the old gun's rest spot to the new gun's.
				st.closeGroundStint(dw)
				st.groundItemsOpen[id] = st.openGroundStint(w)
				st.groundItemSerial[id] = serial
				continue
			}
			if sample {
				st.appendGroundSample(dw, w)
			}
			continue
		}
		// held again: close the open ground stint as a pickup.
		if dw := st.groundItemsOpen[id]; dw != nil {
			dw.PickedUpAtMs = st.roundMs()
			dw.PickedUpBy = w.Owner.SteamID64
			st.closeGroundStint(dw)
			delete(st.groundItemsOpen, id)
		}
	}
	st.pollKits(sample)
}

// groundItemSampleTick reports whether the current frame is a position-track sample tick
// for dropped entities, advancing the cursor when it is. It uses the same framePeriod
// as the player positions stream (its own cursor so the two handlers don't contend),
// so ground items sample at the same cadence as players.
func (st *parseState) groundItemSampleTick() bool {
	cur := st.parsed.CurrentTime()
	if cur-st.frames.lastGroundItemSample < st.framePeriod {
		return false
	}
	st.frames.lastGroundItemSample = cur
	return true
}

// groundItemClass is the ground_items class token for a dropped world entity: guns
// keep their gun class (pistol/smg/heavy/rifle), the loose c4 is "c4", a knife is
// "knife", and grenades and the zeus carry "grenade"/"equipment" from their broad
// equipment class. Kept local to ground items so the kill weapon_class and loadout
// class (both csdata.EquipmentClassName) stay unchanged.
func groundItemClass(w *common.Equipment) string {
	switch {
	case w.Type == common.EqBomb:
		return "c4"
	case w.Type == common.EqKnife:
		return "knife"
	case w.Class() == common.EqClassGrenade:
		return "grenade"
	case w.Class() == common.EqClassEquipment:
		return "equipment" // the zeus (and any other equipment-class droppable)
	default:
		return csdata.EquipmentClassName(w.Class()) // gun classes
	}
}

// openGroundStint begins a ground stint for a just-dropped gun, grenade, zeus, knife
// or the loose c4, seeding the position track with the entity's current position.
func (st *parseState) openGroundStint(w *common.Equipment) *model.DroppedItem {
	t := st.roundMs()
	class := groundItemClass(w)
	lastOwner := st.weaponPrevOwner(w)
	dw := &model.DroppedItem{
		Item:         w.String(),
		Class:        class,
		DroppedAtMs:  t,
		IsInitial:    t == 0, // round-start/spawn state, not a real mid-round drop
		LastOwner:    lastOwner,
		AmmoMagazine: w.AmmoInMagazine(),
		AmmoReserve:  w.AmmoReserve(),
	}
	// on_death is resolved at close (see closeGroundStint), not here: the weapon
	// un-owns ~1 tick before the owner's death event fires, so the death time is
	// not yet recorded when the stint opens.
	st.appendGroundSample(dw, w)
	return dw
}

// closeGroundStint finalizes a gun/grenade/c4 ground stint: it resolves on_death
// from the last owner's recorded death time (now known, since the death event has
// fired by pickup/flush) then appends it. The defuse-kit path keeps its own
// death-based on_death (owner != 0) and appends via appendGroundItem directly.
func (st *parseState) closeGroundStint(dw *model.DroppedItem) {
	dw.OnDeath = st.droppedOnDeath(dw.LastOwner, dw.DroppedAtMs)
	st.appendGroundItem(dw)
}

// droppedOnDeath reports whether a ground stint opened because its last owner died,
// rather than a manual G-key drop or a buy/weapon-swap while alive. The weapon
// un-owns ~1 tick before the owner's death event fires, so this compares the drop
// time to the owner's recorded death time within a small grace window. An unknown
// owner (0 / never died this round) or a drop far from the owner's death is false.
func (st *parseState) droppedOnDeath(owner uint64, dropMs int64) bool {
	if owner == 0 {
		return false
	}
	deathMs, ok := st.deathTimes[owner]
	if !ok {
		return false
	}
	// the race is ~1 tick; a modest grace absorbs frame/event-order jitter without
	// flagging a manual drop the owner made well before they later died.
	const graceMs = 500
	d := dropMs - deathMs
	if d < 0 {
		d = -d
	}
	return d <= graceMs
}

// appendGroundSample adds the entity's current ground position to the stint's track.
// No-op when the entity position is unreadable (entity gone).
func (st *parseState) appendGroundSample(dw *model.DroppedItem, w *common.Equipment) {
	if pos := equipmentPosition(w); pos != nil {
		dw.Positions = append(dw.Positions, model.GroundItemFrame{TMs: st.roundMs(), Position: *pos})
	}
}

// onDataTablesParsed wires the defuse-kit world entity once the net tables exist.
// Only needed when the ground_items stream is on.
func (st *parseState) onDataTablesParsed(_ events.DataTablesParsed) {
	if !st.opts.GroundItems {
		return
	}
	sc := st.kitServerClass()
	if sc == nil {
		return
	}
	_, _ = fmt.Fprintf(os.Stderr, "demolens: defuse-kit world entity class = %s\n", sc.Name())
	sc.OnEntityCreated(st.onKitCreated)
}

// kitServerClass finds the server class the dropped defuse kit spawns as. It
// prefers a typed defuser class (Source 1 item_defuser / CItemDefuser) if present,
// else falls back to CBaseAnimGraph: CS2 ships no typed defuser class in the net
// tables and spawns the dropped kit as a generic anim-graph prop, confirmed per
// instance by its model handle in onKitCreated.
func (st *parseState) kitServerClass() sendtables.ServerClass {
	classes := st.parsed.ServerClasses()
	for _, sc := range classes.All() {
		if strings.Contains(strings.ToLower(sc.Name()), "defus") {
			return sc
		}
	}
	return classes.FindByName("CBaseAnimGraph")
}

// onKitCreated opens a defuse-kit ground stint for a freshly-spawned kit prop. The
// kit only enters the world when a carrier dies, so it is tracked during the live
// round only (post-round drops follow the same mass-relocation artifact the gun
// tracks avoid). Every kit prop shares one model handle; the first one's handle is
// locked and any later anim-graph prop with a different model is ignored, so a
// stray non-kit CBaseAnimGraph cannot masquerade as a kit.
func (st *parseState) onKitCreated(e sendtables.Entity) {
	if !st.opts.GroundItems || !st.roundLive || st.pending == nil || e == nil {
		return
	}
	modelHandle, _ := propU64(e, "CBodyComponent.m_hModel")
	if !st.kitModelSet {
		st.kitModel = modelHandle
		st.kitModelSet = true
	} else if modelHandle != st.kitModel {
		return
	}
	raw, ok := safePosition(e)
	if !ok {
		return
	}
	t := st.roundMs()
	pos := toPosition(raw)
	owner := st.nearestDeadCT(raw)
	stint := &kitStint{
		ent:     e,
		lastPos: pos,
		dw: &model.DroppedItem{
			Item:        common.EqDefuseKit.String(), // "Defuse Kit"
			Class:       "equipment",                 // distinct class so the consumer renders a kit icon, not a pistol
			DroppedAtMs: t,
			LastOwner:   owner,
			OnDeath:     owner != 0, // kits only enter the world on a carrier's death
			Positions:   []model.GroundItemFrame{{TMs: t, Position: pos}},
		},
	}
	st.kitOpen[e.ID()] = stint
	e.OnDestroy(func() { stint.gone = true })
}

// nearestDeadCT returns the SteamID64 of the dead CT nearest pos within
// kitDropRadius: the kit carrier who just died there. 0 when none is close, so a
// kit whose carrier can't be pinned down is left unattributed.
func (st *parseState) nearestDeadCT(pos r3.Vector) uint64 {
	const kitDropRadius = 200.0
	var best uint64
	bestD := kitDropRadius
	for _, pl := range st.parsed.GameState().Participants().Playing() {
		if pl == nil || pl.IsAlive() || sideString(pl.Team) != "CT" {
			continue
		}
		if d := pl.Position().Sub(pos).Norm(); d <= bestD {
			bestD = d
			best = pl.SteamID64
		}
	}
	return best
}

// pollKits advances the open defuse-kit ground stints each frame: it samples the
// live kit positions on the shared dropped cadence, then pairs a CT's defuse-kit
// flag gaining next to an open stint as a real pickup. The kit prop never networks
// an owner, so the picker is the CT whose flag flips on at the entity (not an owner
// handle); a flag gain with no kit nearby is a buy and is ignored.
func (st *parseState) pollKits(sample bool) {
	if sample {
		for _, s := range st.kitOpen {
			if s.gone {
				continue
			}
			if raw, ok := safePosition(s.ent); ok {
				s.lastPos = toPosition(raw)
				s.dw.Positions = append(s.dw.Positions, model.GroundItemFrame{TMs: st.roundMs(), Position: s.lastPos})
			}
		}
	}
	const pickupRadius = 150.0
	for _, pl := range st.parsed.GameState().Participants().Playing() {
		if pl == nil {
			continue
		}
		id := pl.SteamID64
		has := pl.HasDefuseKit()
		gained := has && !st.kitHad[id]
		st.kitHad[id] = has
		if !gained || len(st.kitOpen) == 0 {
			continue
		}
		ppos := pl.Position()
		bestID, bestD := -1, pickupRadius
		for eid, s := range st.kitOpen {
			dx, dy, dz := s.lastPos.X-ppos.X, s.lastPos.Y-ppos.Y, s.lastPos.Z-ppos.Z
			if d := math.Sqrt(dx*dx + dy*dy + dz*dz); d <= bestD {
				bestD, bestID = d, eid
			}
		}
		if bestID < 0 {
			continue
		}
		dw := st.kitOpen[bestID].dw
		dw.PickedUpAtMs = st.roundMs()
		dw.PickedUpBy = id
		st.appendGroundItem(dw)
		delete(st.kitOpen, bestID)
	}
}

// flushGroundItems appends every still-open ground stint to the round stream
// (no picked_up_* = still down at round end) and clears the open map.
func (st *parseState) flushGroundItems() {
	if !st.opts.GroundItems {
		return
	}
	for _, dw := range st.groundItemsOpen {
		st.closeGroundStint(dw)
	}
	st.groundItemsOpen = map[int]*model.DroppedItem{}
	st.groundItemSerial = map[int]int{}
	for _, s := range st.kitOpen {
		st.appendGroundItem(s.dw) // still down at round end: no picked_up_*
	}
	st.kitOpen = map[int]*kitStint{}
}

// appendGroundItem adds a finished ground stint to the round's ground_items
// stream, RLE-collapsing its position track first and lazily allocating the holder.
func (st *parseState) appendGroundItem(dw *model.DroppedItem) {
	dw.Positions = collapseGroundItemTrack(dw.Positions)
	if streams := st.ensureStreams(); streams != nil {
		streams.GroundItems = append(streams.GroundItems, *dw)
	}
}

// collapseGroundItemTrack RLE-compresses a dropped entity's position track: runs of
// consecutive samples at the same position fold onto one frame whose HoldFrames counts
// the dropped duplicates. A still-resting item becomes a single tuple with a large
// hold_frames; a shoved or re-dropped item keeps each new position. Positions are
// compared exactly: a static entity reports byte-identical floats every sample.
func collapseGroundItemTrack(track []model.GroundItemFrame) []model.GroundItemFrame {
	if len(track) <= 1 {
		return track
	}
	out := make([]model.GroundItemFrame, 0, len(track))
	kept := track[0]
	for i := 1; i < len(track); i++ {
		if track[i].Position == kept.Position {
			kept.HoldFrames++
			continue
		}
		out = append(out, kept)
		kept = track[i]
	}
	out = append(out, kept)
	return out
}

// sortStreams sorts the map-derived stream slices for deterministic output. shots
// are appended in event order already; positions, grenade_paths, inventory and
// ground-items are built by ranging maps and need a stable order. No-op when nil.
func sortStreams(s *model.RoundStreams, period time.Duration) {
	if s == nil {
		return
	}
	sort.SliceStable(s.Positions, func(i, j int) bool {
		a, b := s.Positions[i], s.Positions[j]
		if a.TMs != b.TMs {
			return a.TMs < b.TMs
		}
		return a.SteamID < b.SteamID
	})
	s.Positions = compressPositions(s.Positions, period)
	sort.SliceStable(s.GrenadePaths, func(i, j int) bool {
		return s.GrenadePaths[i].GrenadeID < s.GrenadePaths[j].GrenadeID
	})
	sort.SliceStable(s.Inventory, func(i, j int) bool {
		a, b := s.Inventory[i], s.Inventory[j]
		if a.SteamID != b.SteamID {
			return a.SteamID < b.SteamID
		}
		if a.TMs != b.TMs {
			return a.TMs < b.TMs
		}
		return a.Phase < b.Phase
	})
	sort.SliceStable(s.GroundItems, func(i, j int) bool {
		a, b := s.GroundItems[i], s.GroundItems[j]
		if a.DroppedAtMs != b.DroppedAtMs {
			return a.DroppedAtMs < b.DroppedAtMs
		}
		if a.Item != b.Item {
			return a.Item < b.Item
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
func compressPositions(frames []model.PlayerFrame, period time.Duration) []model.PlayerFrame {
	if len(frames) == 0 {
		return frames
	}

	// max gap that still counts as the immediate next sample of a run. ms, matching
	// the frame timestamps and the configured sample period.
	maxGap := int64(period/time.Millisecond) * 3 / 2

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
		lastSeen := kept.TMs
		for i := 1; i < len(group); i++ {
			f := group[i]
			gap := f.TMs - lastSeen
			if gap > 0 && gap <= maxGap && sameFrameState(&f, &kept) {
				kept.HoldFrames++
				lastSeen = f.TMs
				continue
			}
			out = append(out, kept)
			kept = f
			lastSeen = f.TMs
		}
		out = append(out, kept)
	}

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].TMs != out[j].TMs {
			return out[i].TMs < out[j].TMs
		}
		return out[i].SteamID < out[j].SteamID
	})
	return out
}

// sameFrameState reports whether two player frames carry an identical state,
// ignoring T and HoldFrames (the only fields RLE is allowed to vary within a run).
// Velocity is a pointer so the struct can't be compared with ==.
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
		if seen[k][d.TMs] {
			continue
		}
		seen[k][d.TMs] = true
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
