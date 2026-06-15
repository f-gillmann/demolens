package parser

import (
	"runtime"
	"sync"
	"time"

	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/events"
)

// onPlayerFrames samples player pos + state each frame (opt-in), throttled so
// output stays bounded.
func (st *parseState) onPlayerFrames(events.FrameDone) {
	if !st.opts.PlayerFrames || !st.roundLive || st.pending == nil {
		return
	}
	cur := st.parsed.CurrentTime()
	if cur-st.lastFrameSample < frameSamplePeriod {
		return
	}
	st.lastFrameSample = cur
	into := (cur - st.roundStart).Microseconds()
	for _, pl := range st.parsed.GameState().Participants().Playing() {
		if side := sideString(pl.Team); side != "" {
			st.pending.PlayerFrames = append(st.pending.PlayerFrames, playerFrame(pl, side, into))
		}
	}
}

// onSpeedSample remembers every player's position each frame. CS2 doesn't network
// velocity, so kill speed comes from this frame-to-frame delta.
func (st *parseState) onSpeedSample(events.FrameDone) {
	cur := st.parsed.CurrentTime()
	for _, pl := range st.parsed.GameState().Participants().Playing() {
		pos := toPosition(pl.Position())
		if prev, ok := st.lastPos[pl.SteamID64]; ok {
			if dt := (cur - st.lastPosTime[pl.SteamID64]).Seconds(); dt > 0 {
				st.playerSpeed[pl.SteamID64] = horizontalSpeed(pos, prev, dt)
			}
		}
		st.lastPos[pl.SteamID64] = pos
		st.lastPosTime[pl.SteamID64] = cur
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
	st.live = st.live[:0]
	for _, pl := range st.parsed.GameState().Participants().Playing() {
		if !pl.IsAlive() {
			continue
		}
		if eye, ok := pl.PositionEyes(); ok {
			st.live = append(st.live, pv{pl.SteamID64, pl.Team, eye, viewVector(pl)})
		}
	}

	st.cands = st.cands[:0]
	for i := range st.live {
		s := st.live[i]
		for j := range st.live {
			e := st.live[j]
			if e.team == s.team {
				continue
			}
			dir := e.eye.Sub(s.eye)
			key := [2]uint64{s.id, e.id}
			eng := st.engagements[key]
			if eng == nil {
				eng = &engagement{}
				st.engagements[key] = eng
			}

			// crosshair placement: snapshot the view the moment the enemy hits the
			// appearance frustum. the move from there to the hit is the placement.
			// frustum only, no los gate: appearance fires earlier than a strict wall
			// raycast, and gating on los here undershoots.
			xIn := enemyInFrustum(s.view, dir, st.cal.CrosshairConeDeg)
			if xIn && !eng.crosshairInFrustum && !eng.crosshairPending {
				eng.crosshairPending, eng.appearView = true, s.view
			} else if !xIn && eng.crosshairInFrustum && eng.crosshairPending {
				eng.crosshairPending = false
			}
			eng.crosshairInFrustum = xIn

			// TTD clock starts when the enemy is first seen: inside the frustum,
			// clear los, not through smoke. it must stay visible for TTDDebounceMs
			// before the clock commits, which kills 1-tick grazes. then it's back-dated
			// to first-visible. brief look-aways don't break it. once the enemy is
			// unseen for TTDGapMs the sighting resets, so re-peeking someone you just
			// saw isn't counted as a fresh duel.
			// the cheap frustum/mesh gate stays here; the expensive los+smoke check
			// is deferred to pass 2 so it can run concurrently.
			ttdCand := enemyInFrustum(s.view, dir, st.cal.TTDFovDeg) && st.mesh != nil
			if ttdCand {
				st.cands = append(st.cands, cand{en: eng, sEye: s.eye, eEye: e.eye})
			} else {
				st.notVisible(eng, now)
			}
		}
	}

	// pass 2 (parallel): the los raycast + smoke test for each cand. mesh and
	// activeSmokes are read-only for the whole handler, so concurrent reads are
	// safe; each goroutine only writes its own disjoint cand.vis indices.
	if len(st.cands) >= 16 {
		workers := runtime.GOMAXPROCS(0)
		if workers > len(st.cands) {
			workers = len(st.cands)
		}
		chunk := (len(st.cands) + workers - 1) / workers
		var wg sync.WaitGroup
		for w := 0; w < workers; w++ {
			lo := w * chunk
			if lo >= len(st.cands) {
				break
			}
			hi := lo + chunk
			if hi > len(st.cands) {
				hi = len(st.cands)
			}
			wg.Add(1)
			go func(lo, hi int) {
				defer wg.Done()
				for k := lo; k < hi; k++ {
					c := &st.cands[k]
					c.vis = losClear(st.mesh, c.sEye, c.eEye) &&
						!smokeBlocked(c.sEye, c.eEye, st.activeSmokes)
				}
			}(lo, hi)
		}
		wg.Wait()
	} else {
		for k := range st.cands {
			c := &st.cands[k]
			c.vis = losClear(st.mesh, c.sEye, c.eEye) &&
				!smokeBlocked(c.sEye, c.eEye, st.activeSmokes)
		}
	}

	// pass 3 (sequential): the TTD state machine over each cand. each en is
	// touched once for TTD this frame (a pair is a cand or a non-cand, never
	// both), so pair order doesn't change the final state.
	for k := range st.cands {
		c := &st.cands[k]
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
