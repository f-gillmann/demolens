package parser

import (
	"github.com/f-gillmann/demolens/v2/model"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/events"
)

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
