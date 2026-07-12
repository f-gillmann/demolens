package parser

import (
	"math"
	"strings"

	"github.com/f-gillmann/demolens/v2/internal/csdata"
	"github.com/f-gillmann/demolens/v2/model"
	"github.com/golang/geo/r3"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/common"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/events"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/sendtables"
)

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
// keep their gun class (pistol/smg/heavy/rifle/sniper), the loose c4 is "c4", a knife is
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
		return csdata.EquipmentClassName(w.Type) // gun classes
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
	for _, pl := range playingStable(st.parsed.GameState()) {
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
	for _, pl := range playingStable(st.parsed.GameState()) {
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
