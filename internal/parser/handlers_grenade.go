package parser

import (
	"strconv"
	"time"

	"github.com/f-gillmann/demolens/internal/csdata"
	"github.com/f-gillmann/demolens/model"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/common"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/events"
)

// heDamageWindow is how long after an HE detonation a PlayerHurt is still
// attributed to that grenade. The blast is instant; the few extra ticks cover
// the engine's damage-tick lag without pulling in a later, unrelated nade.
const heDamageWindow = 250 * time.Millisecond

func (st *parseState) grenadeByEntity(entityID int) *parseGrenade {
	if st.grenades.pendingGrenades == nil {
		return nil
	}
	uid, ok := st.grenades.entityToUnique[entityID]
	if !ok {
		return nil
	}
	return st.grenades.pendingGrenades[uid]
}

// detonate marks when a grenade lands, pops or explodes.
func (st *parseState) detonate(entityID int, pos model.Position, instant bool) {
	g := st.grenadeByEntity(entityID)
	if g == nil || g.detonateTimeMicroseconds != 0 {
		return
	}
	t := st.roundMicros()
	g.detonateTimeMicroseconds = t
	g.detonatePosition = pos
	g.flightMicroseconds = t - g.throwTimeMicroseconds
	g.detonated = true
	if instant { // flash/he detonate and expire at the same instant
		g.expireTimeMicroseconds = t
	}
}

// expire marks when a timed grenade (smoke/fire/decoy) fades out.
func (st *parseState) expire(entityID int) {
	g := st.grenadeByEntity(entityID)
	if g == nil || g.expireTimeMicroseconds != 0 {
		return
	}
	g.expireTimeMicroseconds = st.roundMicros()
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

	if st.grenades.pendingGrenades != nil {
		throwTime := st.roundMicros()
		st.grenades.grenadeSeq++
		gtype := grenadeTypeString(nade.Type)
		st.grenades.pendingGrenades[thrown.Projectile.UniqueID()] = &parseGrenade{
			grenadeID:             grenadeID(gtype, st.grenades.grenadeSeq),
			thrower:               thrower.SteamID64,
			side:                  roundPlayer.Side,
			gtype:                 gtype,
			throwTimeMicroseconds: throwTime,
			throwPosition:         grenadePosition(thrown.Projectile),
		}
		if e := thrown.Projectile.Entity; e != nil {
			st.grenades.entityToUnique[e.ID()] = thrown.Projectile.UniqueID()
		}
	}
}

// grenadeID is the per-round-stable join key between a grenade record and its
// grenade_paths stream entry. The per-round monotonic seq keeps it deterministic;
// the type prefix keeps it readable. Both records read it off the same parseGrenade.
func grenadeID(gtype string, seq int) string {
	return gtype + "-" + strconv.Itoa(seq)
}

// onInfernoStart links a new fire to its grenade. Fire grenades don't carry a
// projectile link; the inferno is its own entity. FireGrenadeStart/Expired are
// flaky in CS2 too, so we match on thrower instead.
func (st *parseState) onInfernoStart(e events.InfernoStart) {
	if e.Inferno == nil || st.grenades.pendingGrenades == nil {
		return
	}
	thrower := e.Inferno.Thrower()
	if thrower == nil {
		return
	}
	g := newestFireGrenade(st.grenades.pendingGrenades, thrower.SteamID64)
	if g == nil {
		return
	}
	g.detonateTimeMicroseconds = st.roundMicros()
	g.detonatePosition = toPosition(e.Inferno.Entity.Position())
	g.flightMicroseconds = g.detonateTimeMicroseconds - g.throwTimeMicroseconds
	g.detonated = true
	st.grenades.liveInfernos[e.Inferno.UniqueID()] = &liveInferno{inferno: e.Inferno, grenade: g}
}

// onInfernoPoll catches fire burn-out: flames go out well before InfernoExpired
// (~20s later) and there's no burn-out event. Empty-map return keeps the no-fire
// case at one check per frame.
func (st *parseState) onInfernoPoll(_ events.FrameDone) {
	if len(st.grenades.liveInfernos) == 0 {
		return
	}
	cur := st.parsed.CurrentTime()
	for uid, li := range st.grenades.liveInfernos {
		if cur-li.lastChecked < fireCheckPeriod {
			continue
		}
		li.lastChecked = cur
		if active := li.inferno.Fires().Active().List(); len(active) > 0 {
			li.hadFire = true
			st.snapshotPeakFire(li, active)
			continue
		}
		if li.hadFire { // was burning, now all out (burned out or smoked off)
			li.grenade.expireTimeMicroseconds = (cur - st.roundStart).Microseconds()
			delete(st.grenades.liveInfernos, uid)
		}
	}
}

// snapshotPeakFire records the inferno's widest active-flame footprint as the
// candidate fire_cells. Only a strictly larger active set replaces the prior peak,
// so fire_cells ends up the real multi-cell footprint, sorted for determinism.
func (st *parseState) snapshotPeakFire(li *liveInferno, active []common.Fire) {
	if len(active) <= li.peakFireCount {
		return
	}
	li.peakFireCount = len(active)
	cells := make([]model.Position, 0, len(active))
	for _, fire := range active {
		cells = append(cells, toPosition(fire.Vector))
	}
	sortPositions(cells)
	li.grenade.fireCells = cells
}

// onInfernoExpired is the fallback expiry for the case the poll above never caught it.
func (st *parseState) onInfernoExpired(e events.InfernoExpired) {
	if e.Inferno == nil {
		return
	}
	uid := e.Inferno.UniqueID()
	if li := st.grenades.liveInfernos[uid]; li != nil {
		if li.grenade.expireTimeMicroseconds == 0 {
			li.grenade.expireTimeMicroseconds = st.roundMicros()
		}
		delete(st.grenades.liveInfernos, uid)
	}
}

func (st *parseState) onSmokeStartTrack(e events.SmokeStart) {
	st.vision.activeSmokes[e.GrenadeEntityID] = e.Position
}

func (st *parseState) onSmokeExpiredTrack(e events.SmokeExpired) {
	delete(st.vision.activeSmokes, e.GrenadeEntityID)
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
			st.grenades.flashLead[flash.Player.SteamID64] = pendingFlash{
				flasher: flash.Attacker.SteamID64,
				team:    flash.Attacker.Team,
				expire:  st.parsed.CurrentTime() + flash.FlashDuration(),
			}
		}
	}

	// attach the blind to the grenade that caused it, for per-flash durations
	// and the flash matrix. self-flashes don't belong in who-blinded-whom.
	if !self && flash.Projectile != nil && st.grenades.pendingGrenades != nil {
		if g := st.grenades.pendingGrenades[flash.Projectile.UniqueID()]; g != nil {
			fp := model.FlashedPlayer{SteamID: flash.Player.SteamID64, BlindMicroseconds: blind}
			if vrp := st.pendingPlayers[flash.Player.SteamID64]; vrp != nil {
				fp.Side = vrp.Side
			}
			// peak whiteout 0..255, read off the victim pawn. Stash the max per
			// (grenade, victim) and apply at finalize so a re-flash keeps the peak.
			if a, ok := propF64(flash.Player.PlayerPawnEntity(), "m_flFlashMaxAlpha"); ok && a > 0 {
				k := flashAlphaKey{grenade: flash.Projectile.UniqueID(), victim: flash.Player.SteamID64}
				if a > st.grenades.flashAlpha[k] {
					st.grenades.flashAlpha[k] = a
				}
			}
			g.flashed = append(g.flashed, fp)
			if sameTeam {
				g.teammatesFlashed++
			} else {
				g.enemiesFlashed++
			}
		}
	}
}

// attributeHEDamage links one HE hit to the thrower's nearest-in-time detonated HE
// inside heDamageWindow (~250ms). dmg is pre-clamped, so per-grenade victim sums
// equal the per-player he_damage totals. teamHit routes to team_damage.
func (st *parseState) attributeHEDamage(attacker, victim uint64, victimSide string, dmg int, teamHit bool) {
	if st.grenades.pendingGrenades == nil || dmg <= 0 {
		return
	}
	now := st.roundMicros()
	window := heDamageWindow.Microseconds()
	var best *parseGrenade
	for _, g := range st.grenades.pendingGrenades {
		if g.gtype != "he" || !g.detonated || g.thrower != attacker {
			continue
		}
		delta := now - g.detonateTimeMicroseconds
		if delta < 0 || delta > window {
			continue
		}
		if best == nil || g.detonateTimeMicroseconds > best.detonateTimeMicroseconds {
			best = g
		}
	}
	if best == nil {
		return
	}
	addGrenadeVictim(best, victim, victimSide, dmg, teamHit)
}

// attributeFireDamage links one molotov/incendiary PlayerHurt to the live inferno
// whose thrower is the attacker and accumulates per-grenade fire damage. dmg is the
// already-clamped per-hit value from onPlayerHurt.
func (st *parseState) attributeFireDamage(attacker, victim uint64, victimSide string, dmg int, teamHit bool) {
	if dmg <= 0 {
		return
	}
	var g *parseGrenade
	for _, li := range st.grenades.liveInfernos {
		if t := li.inferno.Thrower(); t != nil && t.SteamID64 == attacker {
			g = li.grenade
			break
		}
	}
	if g == nil {
		g = newestDetonatedFireGrenade(st.grenades.pendingGrenades, attacker)
	}
	if g == nil {
		return
	}
	addGrenadeVictim(g, victim, victimSide, dmg, teamHit)
}

// addGrenadeVictim folds one clamped hit into a grenade's running damage totals
// and its per-victim breakdown, accumulating into the existing victim entry when
// the same victim is hit more than once by the same grenade.
func addGrenadeVictim(g *parseGrenade, victim uint64, victimSide string, dmg int, teamHit bool) {
	if teamHit {
		g.teamDamage += dmg
	} else {
		g.damageDealt += dmg
	}
	for i := range g.victims {
		if g.victims[i].SteamID == victim {
			if teamHit {
				g.victims[i].TeamDamage += dmg
			} else {
				g.victims[i].Damage += dmg
			}
			return
		}
	}
	v := model.GrenadeVictim{SteamID: victim, Side: victimSide}
	if teamHit {
		v.TeamDamage = dmg
	} else {
		v.Damage = dmg
	}
	g.victims = append(g.victims, v)
}

// onGrenadeDestroy copies a grenade trajectory out (opt-in) into the grenade_paths
// stream, joined to the grenade by grenade_id. demoinfocs keeps the whole flight
// path on the projectile already, so we just copy it on destroy.
func (st *parseState) onGrenadeDestroy(e events.GrenadeProjectileDestroy) {
	if !st.opts.GrenadePaths || e.Projectile == nil || st.grenades.pendingGrenades == nil {
		return
	}
	g := st.grenades.pendingGrenades[e.Projectile.UniqueID()]
	if g == nil {
		return
	}
	path := make([]model.Position, 0, len(e.Projectile.Trajectory))
	for _, t := range e.Projectile.Trajectory {
		path = append(path, toPosition(t.Position))
	}
	st.grenadePath(g.grenadeID).Path = path
}

func (st *parseState) onGrenadeBounce(e events.GrenadeProjectileBounce) {
	if !st.opts.GrenadePaths || e.Projectile == nil || st.grenades.pendingGrenades == nil {
		return
	}
	g := st.grenades.pendingGrenades[e.Projectile.UniqueID()]
	if g == nil {
		return
	}
	gp := st.grenadePath(g.grenadeID)
	gp.Bounces = append(gp.Bounces, toPosition(e.Projectile.Position()))
}

// grenadePath returns the round's grenade_paths entry for an id, allocating it on
// first use so destroy (path) and bounce (bounces) write into the same record.
func (st *parseState) grenadePath(id string) *model.GrenadePath {
	streams := st.ensureStreams()
	if streams == nil {
		return &model.GrenadePath{} // pending nil: scratch record, never emitted
	}
	for i := range streams.GrenadePaths {
		if streams.GrenadePaths[i].GrenadeID == id {
			return &streams.GrenadePaths[i]
		}
	}
	streams.GrenadePaths = append(streams.GrenadePaths, model.GrenadePath{GrenadeID: id})
	return &streams.GrenadePaths[len(streams.GrenadePaths)-1]
}
