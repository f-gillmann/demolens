package parser

import (
	"runtime"
	"sort"
	"sync"
	"time"

	"github.com/f-gillmann/demolens/v2/model"
	"github.com/golang/geo/r3"
	dem "github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/common"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/events"
)

// below this many candidate sightlines the goroutine dispatch costs more than the
// parallel los work saves, so run them inline
const losParallelThreshold = 2

// playingStable returns the playing participants deduplicated by SteamID64 in a
// deterministic order, so per-frame accumulators keyed by SteamID64 resolve the same
// way on every run. Playing() ranges the library's userID-keyed player map, whose
// iteration order is randomized per process. On a mid-match reconnect the same
// SteamID64 briefly lives under two userIDs at once: the reconnecting player gets a
// fresh controller entity while the stale entry lingers until its controller entity
// is destroyed. Both pass the connected+entity filter, so a SteamID-keyed last write
// would otherwise land on a random one of the two. The tiebreak uses EntityID, not
// UserID: the library masks UserID to a single byte (userID &= 0xff in its
// getOrCreatePlayer), so it wraps on any process with 256+ total connect events
// (routine on a long-running/persistent server) and "higher UserID" can then pick the
// stale entity instead of the reconnected one. EntityID is unmasked and strictly
// increasing per new controller entity, so the reconnected player's entity always
// has the higher value. SteamID64 0 is bots, which share the id legitimately, so
// those entries are never collapsed.
func playingStable(gs dem.GameState) []*common.Player {
	src := gs.Participants().Playing()
	out := make([]*common.Player, 0, len(src))
	idx := make(map[uint64]int, len(src))
	for _, pl := range src {
		if pl.SteamID64 == 0 {
			out = append(out, pl)
			continue
		}
		if i, ok := idx[pl.SteamID64]; ok {
			if pl.EntityID > out[i].EntityID {
				out[i] = pl
			}
			continue
		}
		idx[pl.SteamID64] = len(out)
		out = append(out, pl)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].SteamID64 != out[j].SteamID64 {
			return out[i].SteamID64 < out[j].SteamID64
		}
		return out[i].EntityID < out[j].EntityID
	})
	return out
}

// onPlayerFrames samples player pos + state each frame into the round's positions
// stream (opt-in), throttled so output stays bounded. The capture spans the last
// prerollWindow of freezetime (buffered, negative time), the live round and the
// post-round (into > round_end). 0 stays go-live so kill/bomb times are unchanged.
func (st *parseState) onPlayerFrames(_ events.FrameDone) {
	if !st.opts.PlayerFrames {
		return
	}
	gs := st.parsed.GameState()
	if gs.IsWarmupPeriod() {
		return
	}
	cur := st.parsed.CurrentTime()
	if cur-st.frames.lastFrameSample < st.framePeriod {
		return
	}
	st.frames.lastFrameSample = cur

	if st.framePhase == phaseFreeze {
		// buffer the upcoming round's freeze; pending here is the previous round, so
		// do not touch it. into is a placeholder, rebased negative at the flush.
		for _, pl := range playingStable(gs) {
			if side := sideString(pl.Team); side != "" {
				st.prerollBuf = append(st.prerollBuf, bufferedFrame{abs: cur, frame: st.playerFrame(pl, side, 0)})
			}
		}
		return
	}

	// phaseLive or phasePost: attach to the current round.
	if st.pending == nil {
		return
	}
	streams := st.ensureStreams()
	if streams == nil {
		return
	}
	into := (cur - st.roundStart).Milliseconds()
	for _, pl := range playingStable(gs) {
		if side := sideString(pl.Team); side != "" {
			streams.Positions = append(streams.Positions, st.playerFrame(pl, side, into))
		}
	}
}

// onBuyWindowClose locks each survivor's equipment value at the buy deadline, once
// per round. Dead players were capped in onKill; disconnects keep their freeze seed.
func (st *parseState) onBuyWindowClose(_ events.FrameDone) {
	if !st.roundLive || st.pendingPlayers == nil || st.econ.buyWindowClosed {
		return
	}
	if st.parsed.CurrentTime() < st.econ.buyDeadline {
		return
	}
	st.econ.buyWindowClosed = true

	for _, pl := range playingStable(st.parsed.GameState()) {
		if sideString(pl.Team) == "" || st.econ.buyCaptured[pl.SteamID64] {
			continue
		}
		rp := st.pendingPlayers[pl.SteamID64]
		if rp == nil {
			continue
		}
		if v := pl.EquipmentValueCurrent(); v > 0 { // never clobber the seed with 0
			rp.EquipmentValue = v
		}
		st.econ.buyCaptured[pl.SteamID64] = true
	}
}

// onSpeedSample remembers every player's position each frame. CS2 doesn't network
// velocity, so kill speed comes from this frame-to-frame delta.
func (st *parseState) onSpeedSample(_ events.FrameDone) {
	cur := st.parsed.CurrentTime()
	for _, pl := range playingStable(st.parsed.GameState()) {
		pos := toPosition(pl.Position())
		if prev, ok := st.frames.lastPos[pl.SteamID64]; ok {
			if dt := (cur - st.frames.lastPosTime[pl.SteamID64]).Seconds(); dt > 0 {
				st.frames.playerSpeed[pl.SteamID64] = horizontalSpeed(pos, prev, dt)
				st.frames.playerVelocity[pl.SteamID64] = model.Position{X: (pos.X - prev.X) / dt, Y: (pos.Y - prev.Y) / dt, Z: (pos.Z - prev.Z) / dt}
			}
		}
		st.frames.lastPos[pl.SteamID64] = pos
		st.frames.lastPosTime[pl.SteamID64] = cur
	}
}

// notVisible applies the not-seen branch to a pair outside both fovs (or with no
// mesh): it drops both visibility windows and, if either sighting has gone quiet past
// its own gap, resets that machine. A pair this frame is a cand or a non-cand, never
// both, so this and runSightingMachines never touch the same en twice.
func (st *parseState) notVisible(eng *engagement, now time.Duration) {
	eng.ttdVisSince = 0
	if (eng.ttdRunning || eng.ttdConsumed) &&
		float64((now-eng.ttdLastSeen).Microseconds())/1000 > st.cal.TTDGapMs {
		eng.ttdRunning, eng.ttdConsumed = false, false
	}
	eng.chVisSince = 0
	if (eng.chRunning || eng.chConsumed) &&
		float64((now-eng.chLastSeen).Microseconds())/1000 > st.cal.CrosshairGapMs {
		eng.chRunning, eng.chConsumed, eng.chArmed = false, false, false
		eng.chAppearView = r3.Vector{}
	}
}

// onSighting drives engagement detection for the los metrics. an enemy entering
// the view cone with clear los starts a sighting; the first hit closes it. the
// cheap angle check gates the raycast so only a handful of rays fire per frame.
func (st *parseState) onSighting(_ events.FrameDone) {
	if !st.roundLive {
		return
	}
	now := st.parsed.CurrentTime()

	// pass 1 (sequential): pull each alive player's eye pos + view dir once up
	// front, so the pair loop is O(n) reads instead of O(n^2). reuse the buffer.
	st.vision.live = st.vision.live[:0]
	if st.aimDump != nil {
		for k := range st.vision.byID {
			delete(st.vision.byID, k)
		}
	}
	for _, pl := range playingStable(st.parsed.GameState()) {
		if !pl.IsAlive() {
			continue
		}
		if eye, ok := pl.PositionEyes(); ok {
			// FlashDurationTimeRemaining is the seconds of blindness still left; FlashDuration
			// is the total seconds of the current flash. The fraction still remaining gates the
			// sighting clocks proportionally; guard the no-active-flash case (total <= 0).
			rem := pl.FlashDurationTimeRemaining().Seconds()
			frac := 0.0
			if dur := float64(pl.FlashDuration); dur > 0 {
				frac = rem / dur
			}
			st.vision.live = append(st.vision.live, pv{pl.SteamID64, pl.Team, eye, viewVector(pl), pl.Position(), rem, frac})
			if st.aimDump != nil {
				st.vision.byID[pl.SteamID64] = pl
			}
		}
	}

	// the cheap frustum gate must admit any pair either metric could use, so it spans
	// the wider of the two fovs; the precise per-metric angOff <= fov cut is applied in
	// runSightingMachines. compute the wider fov once.
	maxFov := st.cal.TTDFovDeg
	if st.cal.CrosshairFovDeg > maxFov {
		maxFov = st.cal.CrosshairFovDeg
	}
	// keep the prefilter at least as wide as CSConeDeg: it also feeds the shared
	// lastSeen marker seesTarget reads for the counter-strafe / spotted recent window,
	// which must stay unchanged.
	if st.cal.CSConeDeg > maxFov {
		maxFov = st.cal.CSConeDeg
	}

	// vis's cheap gates (smoke, fire, third-party bodies) read per-frame mutable
	// state, so they run eagerly here at capture time; only the raycasts against
	// the static mesh are deferred into the cross-frame batch. metricFov bounds
	// where vis is ever consumed (the wider CSConeDeg band feeds visN alone).
	metricFov := st.cal.TTDFovDeg
	if st.cal.CrosshairFovDeg > metricFov {
		metricFov = st.cal.CrosshairFovDeg
	}
	var fireCells []r3.Vector
	for _, li := range st.grenades.liveInfernos {
		for _, fire := range li.inferno.Fires().Active().List() {
			fireCells = append(fireCells, fire.Vector)
		}
	}

	b := &st.vision.batch
	frame := batchFrame{now: now, candLo: len(b.cands), notVisLo: len(b.notVis)}

	// the vertical half-angle and the shooter basis depend only on maxFov and each
	// shooter's view, so derive them once per frame / per shooter instead of per pair.
	vHalf := frustumVHalfDeg(maxFov)
	for i := range st.vision.live {
		s := st.vision.live[i]
		sBasis := makeFrustumBasis(s.view)
		for j := range st.vision.live {
			e := st.vision.live[j]
			if e.team == s.team {
				continue
			}
			dir := e.eye.Sub(s.eye)
			key := [2]uint64{s.id, e.id}
			eng := st.vision.engagements[key]
			if eng == nil {
				eng = &engagement{}
				st.vision.engagements[key] = eng
			}

			// cheap frustum/mesh gate so only frustum-passing pairs cost los rays; the
			// mesh raycasts are the deferred batch, the machines replay at flush. the
			// not-visible reset reads machine state, so it defers with the batch too.
			if sBasis.contains(dir, maxFov, vHalf) && st.vision.mesh != nil {
				c := cand{en: eng, sEye: s.eye, eEye: e.eye, eFeet: e.feet, sView: s.view, sID: s.id, vID: e.id, sBlind: s.blindRemain, sBlindFrac: s.blindFrac, angOff: s.view.Angle(dir).Degrees()}
				c.noSmoke = !smokeBlocked(c.sEye, c.eEye, st.vision.activeSmokes, now)
				c.needVis = c.angOff <= metricFov || st.aimDump != nil
				if c.needVis && c.noSmoke {
					c.visGate = !fireBlocked(c.sEye, c.eEye, fireCells) &&
						!playerBlocked(c.sEye, c.eEye, st.vision.live, c.sID, c.vID)
				}
				b.cands = append(b.cands, c)
			} else {
				b.notVis = append(b.notVis, eng)
			}
		}
	}

	frame.candHi, frame.notVisHi = len(b.cands), len(b.notVis)
	if frame.candHi > frame.candLo || frame.notVisHi > frame.notVisLo {
		b.frames = append(b.frames, frame)
	}

	if st.aimDump == nil {
		// bound the buffer; consumers of engagement state drain it themselves.
		if len(b.cands) >= losBatchFlushCands || len(b.frames) >= losBatchFlushFrames {
			st.drainLOS()
		}
		return
	}

	// aim-debug wants one row per candidate per frame, off the flushed values, so
	// batching is a per-frame drain here: flush, emit this frame's rows, reset.
	st.flushLOS()
	defer st.resetLOSBatch()

	// aim-debug dump: one row per candidate this frame (free when the option is off).
	if st.aimDump != nil && st.roundLive && st.pending != nil {
		// gather every live inferno's active fire-cell positions once this frame, for
		// the fire-occlusion probe. measurement-only: not wired into the gate yet.
		var fireCells []r3.Vector
		for _, li := range st.grenades.liveInfernos {
			for _, fire := range li.inferno.Fires().Active().List() {
				fireCells = append(fireCells, fire.Vector)
			}
		}
		for k := frame.candLo; k < frame.candHi; k++ {
			c := &b.cands[k]
			v, s := st.vision.byID[c.vID], st.vision.byID[c.sID]
			spotted := false
			if v != nil && s != nil {
				spotted = v.IsSpottedBy(s)
			}
			vDuck := v != nil && v.IsDucking()
			sScoped := s != nil && s.IsScoped()
			sWeap := ""
			if s != nil {
				if wpn := s.ActiveWeapon(); wpn != nil {
					sWeap = wpn.String()
				}
			}
			dist := c.eEye.Sub(c.sEye).Norm()
			dz := c.eEye.Z - c.sEye.Z
			fireBlk := fireBlocked(c.sEye, c.eEye, fireCells)
			bodyBlk := playerBlocked(c.sEye, c.eEye, st.vision.live, c.sID, c.vID)
			visGeo := losAnyPartFeet(st.vision.mesh, c.sEye, c.eFeet)
			st.aimDump.cand(st.pending.Number, st.roundMs(), st.parsed.GameState().IngameTick(), c.sID, c.vID, c.sView,
				c.sView.Angle(c.eEye.Sub(c.sEye)).Degrees(), c.visN, c.vis, c.visTorso, spotted,
				dist, dz, vDuck, sScoped, c.sBlind, c.sBlindFrac, sWeap, fireBlk, bodyBlk, visGeo)
		}
	}
}

// batch flush bounds: cands cap the raycast buffer's memory, frames cap how
// long a quiet stretch (few cands, but per-frame notVisible resets) can defer.
const (
	losBatchFlushCands  = 4096
	losBatchFlushFrames = 2048
)

// drainLOS flushes and resets the deferred los batch. Every reader of
// engagement state outside the frame pass (player-hurt, weapon-fire, the
// round reset) must call this first so it observes exactly the state the
// unbatched per-frame run would have produced.
func (st *parseState) drainLOS() {
	st.flushLOS()
	st.resetLOSBatch()
}

// flushLOS runs the batch: first every buffered cand's mesh raycasts, fanned
// out across all cores (the mesh is immutable and each cand's rays depend only
// on its own captured snapshot, so chunking is safe); then the sighting
// machines and not-visible resets replay sequentially in frame order, which
// keeps every engagement's state transitions identical to the per-frame run.
func (st *parseState) flushLOS() {
	b := &st.vision.batch
	if len(b.frames) == 0 {
		return
	}

	if len(b.cands) >= losParallelThreshold {
		workers := runtime.GOMAXPROCS(0)
		if workers > len(b.cands) {
			workers = len(b.cands)
		}
		chunk := (len(b.cands) + workers - 1) / workers
		var wg sync.WaitGroup
		for w := 0; w < workers; w++ {
			lo := w * chunk
			if lo >= len(b.cands) {
				break
			}
			hi := lo + chunk
			if hi > len(b.cands) {
				hi = len(b.cands)
			}
			wg.Add(1)
			go func(lo, hi int) {
				defer wg.Done()
				for k := lo; k < hi; k++ {
					st.losRays(&b.cands[k])
				}
			}(lo, hi)
		}
		wg.Wait()
	} else {
		for k := range b.cands {
			st.losRays(&b.cands[k])
		}
	}

	for i := range b.frames {
		f := &b.frames[i]
		st.runSightingMachines(b.cands[f.candLo:f.candHi], f.now)
		for _, eng := range b.notVis[f.notVisLo:f.notVisHi] {
			st.notVisible(eng, f.now)
		}
	}
}

func (st *parseState) resetLOSBatch() {
	b := &st.vision.batch
	b.cands = b.cands[:0]
	b.frames = b.frames[:0]
	b.notVis = b.notVis[:0]
}

// losRays fills one cand's visibilities from the static mesh. The cheap gates
// were captured at frame time (noSmoke/visGate/needVis), so a blocked or
// never-consumed sightline costs no rays here; each gate is pure, so the
// short-circuit cannot change the resulting booleans.
func (st *parseState) losRays(c *cand) {
	if c.needVis {
		c.vis = c.visGate && losAnyPartFeet(st.vision.mesh, c.sEye, c.eFeet)
	} else {
		c.vis = false
	}
	c.visN = c.noSmoke && losClear(st.vision.mesh, c.sEye, c.eEye)
	if st.opts.AimDebugPath != "" {
		c.visTorso = c.noSmoke && losTorso(st.vision.mesh, c.sEye, c.eEye)
	}
}

// runSightingMachines is pass 3 (sequential): the two decoupled sighting state
// machines over one frame's cands. TTD uses the narrow los + its fov/gap/debounce;
// crosshair uses the dense los + its fov/gap/debounce and re-anchors its appearance
// view at the start of each fresh visible window (peek). Each en is touched once per
// frame (a pair is a cand or a non-cand, never both), so pair order doesn't change
// the final state.
func (st *parseState) runSightingMachines(cands []cand, now time.Duration) {
	for k := range cands {
		c := &cands[k]
		eng := c.en

		// shared recently-seen marker for seesTarget (counter-strafe / spotted): the
		// clear losClear sightline, no fov cone, as before the metrics split. left
		// ungated by blindness on purpose, so spotted / counter-strafe stay unchanged.
		if c.visN {
			eng.lastSeen = now
		}

		// a shooter blinded by a flash cannot see, so both sighting clocks must stay
		// paused until less than FlashBlindFraction of this flash's duration remains. only
		// the metric gates use this, never the shared lastSeen marker above.
		sighted := c.sBlindFrac <= st.cal.FlashBlindFraction

		// TTD sighting: dense los inside the TTD fov. clock commits after
		// TTDDebounceMs continuous visibility, back-dated to first-visible.
		ttdVis := c.vis && c.angOff <= st.cal.TTDFovDeg && sighted
		if ttdVis {
			if eng.ttdVisSince == 0 {
				eng.ttdVisSince = now
			}
			if !eng.ttdRunning && !eng.ttdConsumed &&
				float64((now-eng.ttdVisSince).Microseconds())/1000 >= st.cal.TTDDebounceMs {
				eng.ttdRunning, eng.ttdSeeTime = true, eng.ttdVisSince
			}
			eng.ttdLastSeen = now
		} else {
			eng.ttdVisSince = 0
			if (eng.ttdRunning || eng.ttdConsumed) &&
				float64((now-eng.ttdLastSeen).Microseconds())/1000 > st.cal.TTDGapMs {
				eng.ttdRunning, eng.ttdConsumed = false, false
			}
		}

		// crosshair sighting: dense los inside the crosshair fov. the appearance view
		// re-anchors (peek) at the start of a fresh window opened after CrosshairPeekGapMs
		// of not being seen; the sighting commits after CrosshairDebounceMs.
		chVis := c.vis && c.angOff <= st.cal.CrosshairFovDeg && sighted
		if chVis {
			if eng.chVisSince == 0 {
				eng.chVisSince = now
				if !eng.chConsumed && (eng.chLastSeen == 0 ||
					float64((now-eng.chLastSeen).Microseconds())/1000 > st.cal.CrosshairPeekGapMs) {
					eng.chAppearView, eng.chArmed = c.sView, true
				}
			}
			if !eng.chRunning && !eng.chConsumed &&
				float64((now-eng.chVisSince).Microseconds())/1000 >= st.cal.CrosshairDebounceMs {
				eng.chRunning = true
			}
			eng.chLastSeen = now
		} else {
			eng.chVisSince = 0
			if (eng.chRunning || eng.chConsumed) &&
				float64((now-eng.chLastSeen).Microseconds())/1000 > st.cal.CrosshairGapMs {
				eng.chRunning, eng.chConsumed, eng.chArmed = false, false, false
				eng.chAppearView = r3.Vector{}
			}
		}
	}
}
