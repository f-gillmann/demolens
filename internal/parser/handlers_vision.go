package parser

import (
	"runtime"
	"sync"
	"time"

	"github.com/f-gillmann/demolens/model"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/events"
)

// below this many candidate sightlines the goroutine dispatch costs more than the
// parallel los work saves, so run them inline
const losParallelThreshold = 16

// onPlayerFrames samples player pos + state each frame into the round's positions
// stream (opt-in), throttled so output stays bounded. The capture spans the last
// prerollWindow of freezetime (buffered, negative time), the live round and the
// post-round (into > round_end). 0 stays go-live so kill/bomb times are unchanged.
func (st *parseState) onPlayerFrames(events.FrameDone) {
	if !st.opts.PlayerFrames {
		return
	}
	gs := st.parsed.GameState()
	if gs.IsWarmupPeriod() {
		return
	}
	cur := st.parsed.CurrentTime()
	if cur-st.frames.lastFrameSample < frameSamplePeriod {
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
	into := (cur - st.roundStart).Microseconds()
	for _, pl := range gs.Participants().Playing() {
		if side := sideString(pl.Team); side != "" {
			streams.Positions = append(streams.Positions, st.playerFrame(pl, side, into))
		}
	}
}

// onBuyWindowClose locks each survivor's equipment value at the buy deadline, once
// per round. Dead players were capped in onKill; disconnects keep their freeze seed.
func (st *parseState) onBuyWindowClose(events.FrameDone) {
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
func (st *parseState) onSpeedSample(events.FrameDone) {
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

// notVisible applies the not-seen TTD branch: drop the visibility window and, if
// the sighting has gone quiet past TTDGapMs, reset the clock.
func (st *parseState) notVisible(eng *engagement, now time.Duration) {
	eng.visSince = 0
	if (eng.ttdRunning || eng.consumed) &&
		float64((now-eng.lastSeen).Microseconds())/1000 > st.cal.TTDGapMs {
		eng.ttdRunning, eng.consumed = false, false
	}
}

// onSighting drives engagement detection for the los metrics. an enemy entering
// the view cone with clear los starts a sighting; the first hit closes it. the
// cheap angle check gates the raycast so only a handful of rays fire per frame.
func (st *parseState) onSighting(events.FrameDone) {
	if !st.roundLive {
		return
	}
	now := st.parsed.CurrentTime()

	// pass 1 (sequential): pull each alive player's eye pos + view dir once up
	// front, so the pair loop is O(n) reads instead of O(n^2). reuse the buffer.
	st.vision.live = st.vision.live[:0]
	for _, pl := range st.parsed.GameState().Participants().Playing() {
		if !pl.IsAlive() {
			continue
		}
		if eye, ok := pl.PositionEyes(); ok {
			st.vision.live = append(st.vision.live, pv{pl.SteamID64, pl.Team, eye, viewVector(pl)})
		}
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

			// crosshair placement: snapshot the view when the enemy hits the
			// appearance frustum; move from there to the hit is the placement. No los
			// gate here, it fires earlier than a wall raycast and undershoots.
			xIn := enemyInFrustum(s.view, dir, st.cal.CrosshairConeDeg)
			if xIn && !eng.crosshairInFrustum && !eng.crosshairPending {
				eng.crosshairPending, eng.appearView = true, s.view
			} else if !xIn && eng.crosshairInFrustum && eng.crosshairPending {
				eng.crosshairPending = false
			}
			eng.crosshairInFrustum = xIn

			// TTD clock: starts on first sight, commits after TTDDebounceMs continuous
			// visibility (kills 1-tick grazes, back-dated to first-visible), resets
			// after TTDGapMs unseen. Cheap frustum/mesh gate; los+smoke is pass 2.
			ttdCand := enemyInFrustum(s.view, dir, st.cal.TTDFovDeg) && st.vision.mesh != nil
			if ttdCand {
				st.vision.cands = append(st.vision.cands, cand{en: eng, sEye: s.eye, eEye: e.eye})
			} else {
				st.notVisible(eng, now)
			}
		}
	}

	st.runLOSPass()
	st.runTTDStateMachine(now)
}

// runLOSPass is pass 2: los raycast + smoke test per cand. mesh/activeSmokes are
// read-only and each goroutine writes disjoint cand.vis, so chunking is safe above
// losParallelThreshold, else inline.
func (st *parseState) runLOSPass() {
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
					c.vis = losClear(st.vision.mesh, c.sEye, c.eEye) &&
						!smokeBlocked(c.sEye, c.eEye, st.vision.activeSmokes)
				}
			}(lo, hi)
		}
		wg.Wait()
	} else {
		for k := range st.vision.cands {
			c := &st.vision.cands[k]
			c.vis = losClear(st.vision.mesh, c.sEye, c.eEye) &&
				!smokeBlocked(c.sEye, c.eEye, st.vision.activeSmokes)
		}
	}
}

// runTTDStateMachine is pass 3 (sequential): the TTD state machine over each cand.
// each en is touched once for TTD this frame (a pair is a cand or a non-cand,
// never both), so pair order doesn't change the final state.
func (st *parseState) runTTDStateMachine(now time.Duration) {
	for k := range st.vision.cands {
		c := &st.vision.cands[k]
		eng := c.en
		if c.vis {
			if eng.visSince == 0 {
				eng.visSince = now
			}
			if !eng.ttdRunning && !eng.consumed &&
				float64((now-eng.visSince).Microseconds())/1000 >= st.cal.TTDDebounceMs {
				eng.ttdRunning, eng.seeTime = true, eng.visSince
			}
			eng.lastSeen = now
		} else {
			st.notVisible(eng, now)
		}
	}
}
