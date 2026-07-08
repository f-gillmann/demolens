package parser

import (
	"runtime"
	"sync"
	"time"

	"github.com/f-gillmann/demolens/v2/model"
	"github.com/golang/geo/r3"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/events"
)

// below this many candidate sightlines the goroutine dispatch costs more than the
// parallel los work saves, so run them inline
const losParallelThreshold = 16

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
		for _, pl := range gs.Participants().Playing() {
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
	for _, pl := range gs.Participants().Playing() {
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

	for _, pl := range st.parsed.GameState().Participants().Playing() {
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
	for _, pl := range st.parsed.GameState().Participants().Playing() {
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
	for _, pl := range st.parsed.GameState().Participants().Playing() {
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

	st.vision.cands = st.vision.cands[:0]
	for i := range st.vision.live {
		s := st.vision.live[i]
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

			// cheap frustum/mesh gate so only a handful of los rays fire per frame; the
			// los+smoke raycast is pass 2 and the sighting machines are pass 3.
			if enemyInFrustum(s.view, dir, maxFov) && st.vision.mesh != nil {
				st.vision.cands = append(st.vision.cands, cand{en: eng, sEye: s.eye, eEye: e.eye, eFeet: e.feet, sView: s.view, sID: s.id, vID: e.id, sBlind: s.blindRemain, sBlindFrac: s.blindFrac, angOff: s.view.Angle(dir).Degrees()})
			} else {
				st.notVisible(eng, now)
			}
		}
	}

	st.runLOSPass()
	st.runSightingMachines(now)

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
		for k := range st.vision.cands {
			c := &st.vision.cands[k]
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

// runLOSPass is pass 2: it fills both visibilities per cand. vis is the dense
// any-part silhouette (TTD / crosshair), gated by smoke, active inferno fire and
// third-party player bodies; visN is the narrow 9-ray silhouette (counter-strafe /
// spotted marker), gated by smoke only so it stays byte-identical to its calibration.
// The active fire cells are gathered once up front; mesh/activeSmokes/live and that
// slice are read-only and each goroutine writes disjoint cand fields, so chunking is
// safe above losParallelThreshold, else inline.
func (st *parseState) runLOSPass() {
	// gather every live inferno's active fire-cell positions once this frame, so the
	// fire-occlusion probe runs off a single read-only slice shared by the workers
	// below (no per-cand rebuild, no data race). empty when no inferno is live, in
	// which case fireBlocked returns false cheaply.
	var fireCells []r3.Vector
	for _, li := range st.grenades.liveInfernos {
		for _, fire := range li.inferno.Fires().Active().List() {
			fireCells = append(fireCells, fire.Vector)
		}
	}
	now := st.parsed.CurrentTime()

	if len(st.vision.cands) >= losParallelThreshold {
		workers := runtime.GOMAXPROCS(0)
		if workers > len(st.vision.cands) {
			workers = len(st.vision.cands)
		}
		chunk := (len(st.vision.cands) + workers - 1) / workers
		var wg sync.WaitGroup
		for w := 0; w < workers; w++ {
			lo := w * chunk
			if lo >= len(st.vision.cands) {
				break
			}
			hi := lo + chunk
			if hi > len(st.vision.cands) {
				hi = len(st.vision.cands)
			}
			wg.Add(1)
			go func(lo, hi int) {
				defer wg.Done()
				for k := lo; k < hi; k++ {
					c := &st.vision.cands[k]
					noSmoke := !smokeBlocked(c.sEye, c.eEye, st.vision.activeSmokes, now)
					c.vis = losAnyPartFeet(st.vision.mesh, c.sEye, c.eFeet) && noSmoke &&
						!fireBlocked(c.sEye, c.eEye, fireCells) &&
						!playerBlocked(c.sEye, c.eEye, st.vision.live, c.sID, c.vID)
					c.visN = losClear(st.vision.mesh, c.sEye, c.eEye) && noSmoke
					if st.opts.AimDebugPath != "" {
						c.visTorso = losTorso(st.vision.mesh, c.sEye, c.eEye) && noSmoke
					}
				}
			}(lo, hi)
		}
		wg.Wait()
	} else {
		for k := range st.vision.cands {
			c := &st.vision.cands[k]
			noSmoke := !smokeBlocked(c.sEye, c.eEye, st.vision.activeSmokes, now)
			c.vis = losAnyPartFeet(st.vision.mesh, c.sEye, c.eFeet) && noSmoke &&
				!fireBlocked(c.sEye, c.eEye, fireCells) &&
				!playerBlocked(c.sEye, c.eEye, st.vision.live, c.sID, c.vID)
			c.visN = losClear(st.vision.mesh, c.sEye, c.eEye) && noSmoke
			if st.opts.AimDebugPath != "" {
				c.visTorso = losTorso(st.vision.mesh, c.sEye, c.eEye) && noSmoke
			}
		}
	}
}

// runSightingMachines is pass 3 (sequential): the two decoupled sighting state
// machines over each cand. TTD uses the narrow los + its fov/gap/debounce; crosshair
// uses the dense los + its fov/gap/debounce and re-anchors its appearance view at the
// start of each fresh visible window (peek). Each en is touched once per frame (a pair
// is a cand or a non-cand, never both), so pair order doesn't change the final state.
func (st *parseState) runSightingMachines(now time.Duration) {
	for k := range st.vision.cands {
		c := &st.vision.cands[k]
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
