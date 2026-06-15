package parser

import (
	"github.com/f-gillmann/demolens/internal/csdata"
	"github.com/f-gillmann/demolens/model"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/common"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/events"
)

func (st *parseState) grenadeByEntity(entityID int) *model.Grenade {
	if st.pendingGrenades == nil {
		return nil
	}
	uid, ok := st.entityToUnique[entityID]
	if !ok {
		return nil
	}
	return st.pendingGrenades[uid]
}

// detonate marks when a grenade lands, pops or explodes.
func (st *parseState) detonate(entityID int, pos model.Position, instant bool) {
	g := st.grenadeByEntity(entityID)
	if g == nil || g.DetonateTimeMicroseconds != 0 {
		return
	}
	t := (st.parsed.CurrentTime() - st.roundStart).Microseconds()
	g.DetonateTimeMicroseconds = t
	g.DetonatePosition = pos
	g.FlightMicroseconds = t - g.ThrowTimeMicroseconds
	if instant { // flash/he detonate and expire at the same instant
		g.ExpireTimeMicroseconds = t
	}
}

// expire marks when a timed grenade (smoke/fire/decoy) fades out.
func (st *parseState) expire(entityID int) {
	g := st.grenadeByEntity(entityID)
	if g == nil || g.ExpireTimeMicroseconds != 0 {
		return
	}
	g.ExpireTimeMicroseconds = (st.parsed.CurrentTime() - st.roundStart).Microseconds()
}

func (st *parseState) grenadeEventPos(e events.GrenadeEvent) model.Position {
	return toPosition(e.Position)
}

func (st *parseState) onGrenadeThrow(thrown events.GrenadeProjectileThrow) {
	if st.parsed.GameState().IsWarmupPeriod() || thrown.Projectile == nil {
		return
	}
	nade := thrown.Projectile.WeaponInstance
	thrower := thrown.Projectile.Thrower
	if nade == nil || thrower == nil {
		return
	}
	roundPlayer := st.pendingPlayers[thrower.SteamID64]
	if roundPlayer == nil {
		return
	}

	switch nade.Type {
	case common.EqFlash:
		roundPlayer.Utility.FlashesThrown++
	case common.EqSmoke:
		roundPlayer.Utility.SmokesThrown++
	case common.EqHE:
		roundPlayer.Utility.HEsThrown++
	case common.EqMolotov, common.EqIncendiary:
		roundPlayer.Utility.MolotovsThrown++
	case common.EqDecoy:
		roundPlayer.Utility.DecoysThrown++
	}
	roundPlayer.Utility.UsedUtilityValue += csdata.UtilityPrice[nade.Type]

	if st.pendingGrenades != nil {
		throwTime := (st.parsed.CurrentTime() - st.roundStart).Microseconds()
		st.pendingGrenades[thrown.Projectile.UniqueID()] = &model.Grenade{
			Thrower:               thrower.SteamID64,
			Side:                  roundPlayer.Side,
			Type:                  grenadeTypeString(nade.Type),
			ThrowTimeMicroseconds: throwTime,
			ThrowPosition:         grenadePosition(thrown.Projectile),
		}
		if e := thrown.Projectile.Entity; e != nil {
			st.entityToUnique[e.ID()] = thrown.Projectile.UniqueID()
		}
	}
}

// onInfernoStart links a new fire to its grenade. Fire grenades don't carry a
// projectile link; the inferno is its own entity. FireGrenadeStart/Expired are
// flaky in CS2 too, so we match on thrower instead.
func (st *parseState) onInfernoStart(e events.InfernoStart) {
	if e.Inferno == nil || st.pendingGrenades == nil {
		return
	}
	thrower := e.Inferno.Thrower()
	if thrower == nil {
		return
	}
	g := newestFireGrenade(st.pendingGrenades, thrower.SteamID64)
	if g == nil {
		return
	}
	g.DetonateTimeMicroseconds = (st.parsed.CurrentTime() - st.roundStart).Microseconds()
	g.DetonatePosition = toPosition(e.Inferno.Entity.Position())
	g.FlightMicroseconds = g.DetonateTimeMicroseconds - g.ThrowTimeMicroseconds
	st.liveInfernos[e.Inferno.UniqueID()] = &liveInferno{inferno: e.Inferno, grenade: g}
}

// onInfernoPoll polls live infernos to catch burn-out. flames go out well before
// the inferno entity is removed (InfernoExpired fires ~20s later). there's no
// event for the actual burn-out, so we poll. guard it hard: only when a fire is
// live, only after it's burned a bit, and only every so often, so the no-fire
// case is a single map check per frame.
func (st *parseState) onInfernoPoll(events.FrameDone) {
	if len(st.liveInfernos) == 0 {
		return
	}
	cur := st.parsed.CurrentTime()
	for uid, li := range st.liveInfernos {
		if cur-li.lastChecked < fireCheckPeriod {
			continue
		}
		li.lastChecked = cur
		if len(li.inferno.Fires().Active().List()) > 0 {
			li.hadFire = true
			continue
		}
		if li.hadFire { // was burning, now all out (burned out or smoked off)
			li.grenade.ExpireTimeMicroseconds = (cur - st.roundStart).Microseconds()
			delete(st.liveInfernos, uid)
		}
	}
}

// onInfernoExpired is the fallback expiry for the case the poll above never caught it.
func (st *parseState) onInfernoExpired(e events.InfernoExpired) {
	if e.Inferno == nil {
		return
	}
	uid := e.Inferno.UniqueID()
	if li := st.liveInfernos[uid]; li != nil {
		if li.grenade.ExpireTimeMicroseconds == 0 {
			li.grenade.ExpireTimeMicroseconds = (st.parsed.CurrentTime() - st.roundStart).Microseconds()
		}
		delete(st.liveInfernos, uid)
	}
}

func (st *parseState) onSmokeStartTrack(e events.SmokeStart) {
	st.activeSmokes[e.GrenadeEntityID] = e.Position
}

func (st *parseState) onSmokeExpiredTrack(e events.SmokeExpired) {
	delete(st.activeSmokes, e.GrenadeEntityID)
}

func (st *parseState) onPlayerFlashed(flash events.PlayerFlashed) {
	if st.parsed.GameState().IsWarmupPeriod() || flash.Attacker == nil || flash.Player == nil {
		return
	}
	roundPlayer := st.pendingPlayers[flash.Attacker.SteamID64]
	if roundPlayer == nil {
		return
	}

	self := flash.Attacker.SteamID64 == flash.Player.SteamID64
	sameTeam := self || flash.Player.Team == flash.Attacker.Team
	blind := int64(float64(flash.FlashDuration().Microseconds()) * st.cal.FlashBlindScale)

	// only "fully flashed" players count, i.e. blinded >= 1.1s. friendlies here
	// means the thrower's own team plus themselves.
	if blind >= flashFullyBlind.Microseconds() {
		if sameTeam {
			roundPlayer.Utility.TeammatesFlashed++
		} else {
			roundPlayer.Utility.EnemiesFlashed++
			roundPlayer.Utility.EnemyBlindMicroseconds += blind
			// arm the flash-to-kill credit in case this enemy dies still blind
			st.flashLead[flash.Player.SteamID64] = pendingFlash{
				flasher: flash.Attacker.SteamID64,
				team:    flash.Attacker.Team,
				expire:  st.parsed.CurrentTime() + flash.FlashDuration(),
			}
		}
	}

	// attach the blind to the grenade that caused it, for per-flash durations
	// and the flash matrix. self-flashes don't belong in who-blinded-whom.
	if !self && flash.Projectile != nil && st.pendingGrenades != nil {
		if g := st.pendingGrenades[flash.Projectile.UniqueID()]; g != nil {
			fp := model.FlashedPlayer{SteamID: flash.Player.SteamID64, BlindMicroseconds: blind}
			if vrp := st.pendingPlayers[flash.Player.SteamID64]; vrp != nil {
				fp.Side = vrp.Side
			}
			g.Flashed = append(g.Flashed, fp)
			if sameTeam {
				g.TeammatesFlashed++
			} else {
				g.EnemiesFlashed++
			}
		}
	}
}

// onGrenadeDestroy copies a grenade trajectory out (opt-in). demoinfocs keeps the
// whole flight path on the projectile already, so just copy it out on destroy.
func (st *parseState) onGrenadeDestroy(e events.GrenadeProjectileDestroy) {
	if !st.opts.GrenadePaths || e.Projectile == nil || st.pendingGrenades == nil {
		return
	}
	if g := st.pendingGrenades[e.Projectile.UniqueID()]; g != nil {
		for _, t := range e.Projectile.Trajectory {
			g.Path = append(g.Path, toPosition(t.Position))
		}
	}
}

func (st *parseState) onGrenadeBounce(e events.GrenadeProjectileBounce) {
	if !st.opts.GrenadePaths || e.Projectile == nil || st.pendingGrenades == nil {
		return
	}
	if g := st.pendingGrenades[e.Projectile.UniqueID()]; g != nil {
		g.Bounces = append(g.Bounces, toPosition(e.Projectile.Position()))
	}
}
