package parser

import (
	"github.com/f-gillmann/demolens/internal/csdata"
	"github.com/f-gillmann/demolens/model"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/common"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/events"
)

// sprayDevKey buckets a player's scoped/no-silencer/base sprays of one weapon
// separately so they don't average. Internal key, not a display name.
func sprayDevKey(run *sprayRun) string {
	if run.scoped && csdata.SprayPatternsScoped[run.weapon] != nil {
		return run.label + "|scoped"
	}
	if !run.silenced && csdata.SprayPatternsNoSilencer[run.weapon] != nil {
		return run.label + "|nosil"
	}
	return run.label
}

// finalizeSpray closes a finished spray run. Needs 3+ consecutive shots of the
// same auto weapon. Records recoil deviation for every such spray, plus the hit
// ratio for rifles.
func (st *parseState) finalizeSpray(id uint64) {
	run := st.aim.curSpray[id]
	delete(st.aim.curSpray, id)
	if run == nil || len(run.shotTimes) < 3 {
		return
	}

	// recoil deviation: player's per-shot aim trajectory vs the weapon pattern.
	// no visibility gate, this measures spray control not accuracy.
	if pat := csdata.SprayPattern(run.weapon, run.scoped, run.silenced); pat != nil && len(run.views) > 0 {
		if st.aim.sprayDev[id] == nil {
			st.aim.sprayDev[id] = map[string]*sprayDevAgg{}
		}
		key := sprayDevKey(run)
		da := st.aim.sprayDev[id][key]
		if da == nil {
			da = &sprayDevAgg{weapon: run.weapon, label: run.label, scoped: run.scoped, silenced: run.silenced}
			st.aim.sprayDev[id][key] = da
		}

		lim := len(pat)
		if len(run.views) < lim {
			lim = len(run.views)
		}
		for len(da.sumX) < lim {
			da.sumX = append(da.sumX, 0)
			da.sumY = append(da.sumY, 0)
			da.n = append(da.n, 0)
		}

		y0, p0 := run.views[0][0], run.views[0][1]
		for i := 0; i < lim; i++ {
			da.sumX[i] += wrapDeg(run.views[i][0] - y0)
			da.sumY[i] += run.views[i][1] - p0
			da.n[i]++
		}
		da.sprays++
	}
	// spray accuracy is rifles only. non-rifles never populate visTimes.
	if len(run.visTimes) == 0 {
		return
	}

	shots := len(run.visTimes)
	hits := 0
	win := int64(st.cal.SprayHitWindowMs * 1000)
	for _, t := range run.visTimes {
		if hitNear(st.aim.hitTimes[id], t, win) { // shot landed a bullet on an enemy
			hits++
		}
	}

	st.aim.sprayShots[id] += shots
	st.aim.sprayHits[id] += hits
	if st.aim.sprayByWeapon[id] == nil {
		st.aim.sprayByWeapon[id] = map[string]*sprayAgg{}
	}
	a := st.aim.sprayByWeapon[id][run.label]
	if a == nil {
		a = &sprayAgg{}
		st.aim.sprayByWeapon[id][run.label] = a
	}
	a.hits += hits
	a.shots += shots
	a.sprays++
}

// killWeaponPickedUp reports whether the kill gun was picked up: original owner set
// and not the killer. Knife/zeus/world kills are false.
func killWeaponPickedUp(kill events.Kill) bool {
	if kill.Killer == nil || kill.Killer.SteamID64 == 0 {
		return false
	}
	w := kill.Weapon
	if w == nil || w.Entity == nil {
		w = kill.Killer.ActiveWeapon()
	}
	if w == nil || !csdata.IsGun(w) {
		return false
	}
	orig := weaponOriginalOwner(w)
	return orig != 0 && orig != kill.Killer.SteamID64
}

func (st *parseState) onKill(kill events.Kill) {
	if st.parsed.GameState().IsWarmupPeriod() {
		return
	}

	// exit kills (round already over) get their own bucket. they never touch
	// K/D and stay out of the kill timeline that clutch/opening/trade reads.
	if !st.roundLive {
		st.recordExitKill(kill)
		return
	}

	st.tallyKillerStats(kill)
	st.tallyVictimStats(kill)
	st.tallyAssistStats(kill)

	rk := roundKill(kill, st.parsed.CurrentTime()-st.roundStart)
	rk.AlivePlayers = aliveSnapshot(st.parsed.GameState())
	if kill.Killer != nil {
		rk.KillerSpeed = st.frames.playerSpeed[kill.Killer.SteamID64]
		rk.KillerSpeedRatio = csdata.SpeedRatio(rk.KillerSpeed, kill.Weapon)
		rk.KillerScoped = kill.Killer.IsScoped()
	}
	rk.PickedUp = killWeaponPickedUp(kill)
	rk.KillerSide = st.roundSide(rk.Killer)
	rk.VictimSide = st.roundSide(rk.Victim)
	st.markFirstContact()
	st.pending.Kills = append(st.pending.Kills, rk)
}

// recordExitKill buckets a kill that landed after the round ended: it bumps the
// killer/victim exit counters and appends to the round's exit-kill timeline. Never
// touches K/D or the live kill timeline.
func (st *parseState) recordExitKill(kill events.Kill) {
	if st.pendingPlayers == nil {
		return
	}
	if kill.Killer != nil && kill.Killer.SteamID64 != 0 {
		st.track(kill.Killer.SteamID64, kill.Killer.Name)
		if roundPlayer := st.pendingPlayers[kill.Killer.SteamID64]; roundPlayer != nil {
			roundPlayer.ExitKills++
		}
	}
	if kill.Victim != nil && kill.Victim.SteamID64 != 0 {
		st.track(kill.Victim.SteamID64, kill.Victim.Name)
		if roundPlayer := st.pendingPlayers[kill.Victim.SteamID64]; roundPlayer != nil {
			roundPlayer.ExitDeaths++
		}
	}
	st.pending.ExitKills = append(st.pending.ExitKills, roundKill(kill, st.parsed.CurrentTime()-st.roundStart))
}

// tallyKillerStats credits the killer's round kill plus the per-round kill-type
// counters (headshot, knife/zeus, airborne, blind, scoped, picked-up).
func (st *parseState) tallyKillerStats(kill events.Kill) {
	if kill.Killer != nil && kill.Killer.SteamID64 != 0 {
		st.track(kill.Killer.SteamID64, kill.Killer.Name)
		if roundPlayer := st.pendingPlayers[kill.Killer.SteamID64]; roundPlayer != nil {
			roundPlayer.Kills++
			if kill.IsHeadshot {
				roundPlayer.Headshots++
			}
			// per-round kill-type counters; roll up to the match Player in metrics.
			if kill.Weapon != nil {
				switch kill.Weapon.Type {
				case common.EqKnife:
					roundPlayer.KnifeKills++
				case common.EqZeus:
					roundPlayer.ZeusKills++
				}
			}
			if kill.Killer.IsAirborne() {
				roundPlayer.AirborneKills++
			}
			if kill.AttackerBlind {
				roundPlayer.BlindKills++
			}
			if kill.Killer.IsScoped() {
				roundPlayer.ScopedKills++
			}
			if killWeaponPickedUp(kill) {
				roundPlayer.PickedUpKills++
			}
		}
	}
}

// tallyVictimStats credits the victim's death, locks their buy-window equipment
// value (a dead player can't buy), and resolves flash-to-kill credit for the
// flasher when the victim died fully blind.
func (st *parseState) tallyVictimStats(kill events.Kill) {
	if kill.Victim != nil && kill.Victim.SteamID64 != 0 {
		st.track(kill.Victim.SteamID64, kill.Victim.Name)
		if roundPlayer := st.pendingPlayers[kill.Victim.SteamID64]; roundPlayer != nil {
			roundPlayer.Deaths++
			// value of nades still in hand when they died
			roundPlayer.Utility.UnusedUtilityValue += grenadeInventoryValue(kill.Victim)
			// death-cap: a dead player can't buy, so lock their buy-window value
			// NOW. Fires only on a real Kill event, never on a transient disconnect.
			if !st.econ.buyCaptured[kill.Victim.SteamID64] {
				roundPlayer.EquipmentValue = kill.Victim.EquipmentValueCurrent()
				st.econ.buyCaptured[kill.Victim.SteamID64] = true
			}
		}
		// flash-to-kill: victim died still fully blind, and the killer is on
		// the flasher's team. credit the flasher.
		if fl, ok := st.grenades.flashLead[kill.Victim.SteamID64]; ok {
			if st.parsed.CurrentTime() < fl.expire && kill.Killer != nil && kill.Killer.Team == fl.team {
				if roundPlayer := st.pendingPlayers[fl.flasher]; roundPlayer != nil {
					roundPlayer.Utility.FlashesLeadingToKill++
				}
			}
			delete(st.grenades.flashLead, kill.Victim.SteamID64)
		}
	}
}

// tallyAssistStats credits the assister with a flash assist or a regular assist.
func (st *parseState) tallyAssistStats(kill events.Kill) {
	if kill.Assister != nil && kill.Assister.SteamID64 != 0 {
		st.track(kill.Assister.SteamID64, kill.Assister.Name)
		if roundPlayer := st.pendingPlayers[kill.Assister.SteamID64]; roundPlayer != nil {
			if kill.AssistedFlash {
				roundPlayer.FlashAssists++
			} else {
				roundPlayer.Assists++
			}
		}
	}
}

// onOtherDeath credits chicken kills. chicken_kills is a player-match total only
// (chickens are not a round-level victim), so we bump the match Player directly.
// Warmup and exit-window chickens are ignored to match the kill timeline.
func (st *parseState) onOtherDeath(e events.OtherDeath) {
	if st.parsed.GameState().IsWarmupPeriod() || !st.roundLive {
		return
	}
	if e.OtherType != "chicken" {
		return
	}
	if e.Killer == nil || e.Killer.SteamID64 == 0 {
		return
	}
	st.track(e.Killer.SteamID64, e.Killer.Name)
	st.players[e.Killer.SteamID64].ChickenKills++
}

// roundSide is a player's CT/T side this round, read off their RoundPlayer. Empty
// when the id is unknown (world/non-player).
func (st *parseState) roundSide(id uint64) string {
	if rp := st.pendingPlayers[id]; rp != nil {
		return rp.Side
	}
	return ""
}

func (st *parseState) onPlayerHurt(hurt events.PlayerHurt) {
	if st.parsed.GameState().IsWarmupPeriod() {
		return
	}
	if hurt.Attacker == nil || hurt.Attacker.SteamID64 == 0 {
		return
	}

	st.sampleAimOnHit(hurt)

	// cap cumulative health damage against one (round, victim) life at 100.
	// demoinfocs reports full pre-tick HP on a killing shotgun pellet instead of
	// the remaining HP, overcounting a multi-pellet kill; a no-op for correct hits.
	dmg := hurt.HealthDamageTaken
	if hurt.Player != nil {
		vid := hurt.Player.SteamID64
		remaining := 100 - st.dmgToVictim[vid]
		if remaining < 0 {
			remaining = 0
		}
		if dmg > remaining {
			dmg = remaining
		}
		st.dmgToVictim[vid] += dmg
	}

	st.attributeDamage(hurt, dmg)

	// timeline, live round only. trade/clutch analysis reads this.
	if st.roundLive {
		st.markFirstContact()
		st.pending.Damages = append(st.pending.Damages, damageEvent(hurt, st.parsed.CurrentTime()-st.roundStart, dmg, st.opts.PlayerFrames))
	}
}

// sampleAimOnHit feeds the aim-duel calibration off a gun hit on an enemy
// (spotted-accuracy numerator, rifle hit ticks, crosshair + TTD samples). Gun-on-
// enemy only; nade/molotov/zeus damage isn't an aim duel and must not consume here.
func (st *parseState) sampleAimOnHit(hurt events.PlayerHurt) {
	if !st.roundLive || !csdata.IsGun(hurt.Weapon) || hurt.Player == nil || hurt.Player.Team == hurt.Attacker.Team {
		return
	}
	id := hurt.Attacker.SteamID64

	// spotted-accuracy numerator: same gate as the denominator (drops wallbangs
	// and through-smoke hits).
	if seesTarget(hurt.Attacker, hurt.Player, st.vision.mesh, st.vision.activeSmokes, st.vision.engagements, st.cal.CSConeDeg, st.parsed.CurrentTime(), 0) {
		st.aim.hitsOnEnemy[id]++
	}

	// note the tick a rifle bullet hit an enemy, so a spray can tally its hits
	// later. one tick is one hit even on penetration.
	if csdata.IsRifle(hurt.Weapon) {
		st.aim.hitTimes[id] = append(st.aim.hitTimes[id], st.parsed.CurrentTime().Microseconds())
	}

	// first gun damage closes the sighting: emit the crosshair + TTD samples.
	eng := st.vision.engagements[[2]uint64{id, hurt.Player.SteamID64}]
	if eng == nil {
		return
	}
	if eng.crosshairPending { // crosshair = view move from appearance to this hit
		eng.crosshairPending = false
		st.aim.crosshair[id] = append(st.aim.crosshair[id], crosshairDelta(eng.appearView, hurt.Attacker))
	}
	if eng.ttdRunning { // TTD = first-saw to this hit
		eng.ttdRunning, eng.consumed = false, true
		ttd := float64((st.parsed.CurrentTime() - eng.seeTime).Microseconds()) / 1000
		// keep the raw value; outliers get clamped/trimmed at finalize (adaptiveTTD).
		if ttd >= st.cal.TTDFloorMs {
			st.aim.ttdSamples[id] = append(st.aim.ttdSamples[id], ttd)
			vk := [2]uint64{id, hurt.Player.SteamID64}
			st.aim.ttdByVictim[vk] = append(st.aim.ttdByVictim[vk], ttd)
		}
	}
}

// attributeDamage folds the clamped hit into the attacker's damage rollups (incl.
// post-round, to match HLTV) and, for he/molotov, the per-grenade victim breakdown.
// Same dmg in both, so per-grenade sums equal the he_damage/molotov_damage totals.
func (st *parseState) attributeDamage(hurt events.PlayerHurt, dmg int) {
	roundPlayer := st.pendingPlayers[hurt.Attacker.SteamID64]
	if roundPlayer == nil {
		return
	}
	teamHit := hurt.Player != nil && hurt.Player.Team == hurt.Attacker.Team
	if teamHit {
		roundPlayer.TeamDamage += dmg
	} else {
		roundPlayer.Damage += dmg
	}
	if hurt.Weapon == nil {
		return
	}

	var victimSide string
	if hurt.Player != nil {
		if vrp := st.pendingPlayers[hurt.Player.SteamID64]; vrp != nil {
			victimSide = vrp.Side
		}
	}
	switch hurt.Weapon.Type {
	case common.EqHE:
		if teamHit {
			roundPlayer.Utility.HETeamDamage += dmg
		} else {
			roundPlayer.Utility.HEDamage += dmg
		}
		if hurt.Player != nil {
			st.attributeHEDamage(hurt.Attacker.SteamID64, hurt.Player.SteamID64, victimSide, dmg, teamHit)
		}
	case common.EqMolotov, common.EqIncendiary:
		if teamHit {
			roundPlayer.Utility.MolotovTeamDamage += dmg
		} else {
			roundPlayer.Utility.MolotovDamage += dmg
		}
		if hurt.Player != nil {
			st.attributeFireDamage(hurt.Attacker.SteamID64, hurt.Player.SteamID64, victimSide, dmg, teamHit)
		}
	}
}

// markFirstContact latches the round's first kill/damage. The first call of a live
// round snapshots every playing player's inventory under the first_contact phase;
// later calls are a single bool check.
func (st *parseState) markFirstContact() {
	if st.firstContact || !st.roundLive {
		return
	}
	st.firstContact = true
	st.snapshotInventories("first_contact")
}

func (st *parseState) onWeaponFire(fire events.WeaponFire) {
	if st.parsed.GameState().IsWarmupPeriod() || fire.Shooter == nil || !csdata.IsGun(fire.Weapon) {
		return
	}

	// count every gun shot, exit shots included, so the accuracy denominator
	// spans the same scope as hits (which include post-round damage).
	if roundPlayer := st.pendingPlayers[fire.Shooter.SteamID64]; roundPlayer != nil {
		roundPlayer.ShotsFired++
	}

	inVision, csVisible := st.shotVisionGate(fire)

	// denominator: shots at an enemy in vision, recently-seen window included
	// (firing at someone you just watched duck still counts). numerator needs
	// the enemy actually visible at impact.
	if inVision {
		st.aim.shotsAtEnemy[fire.Shooter.SteamID64]++
	}

	// per-(shooter,weapon) tally for round.shot_stats. always on, even when the
	// per-shot stream is off. shots match ShotsFired's scope; spotted reuses inVision.
	if roundPlayer := st.pendingPlayers[fire.Shooter.SteamID64]; roundPlayer != nil {
		acc := st.shotStat(fire.Shooter.SteamID64, fire.Weapon.String())
		acc.shots++
		if inVision {
			acc.spotted++
		}
	}

	st.accumulateSpray(fire)
	st.recordCounterStrafe(fire, csVisible)

	// per-shot stream (opt-in). full rate, never downsampled, so shot geometry
	// stays intact. shot_stats above is the always-on aggregate when this is off.
	// post-round included so exit-frag shots land in the same pending(N).
	if st.opts.Shots && (st.roundLive || st.framePhase == phasePost) {
		if streams := st.ensureStreams(); streams != nil {
			streams.Shots = append(streams.Shots, model.Shot{
				TimeMicroseconds: st.roundMicros(),
				Shooter:          fire.Shooter.SteamID64,
				Weapon:           fire.Weapon.String(),
				Position:         toPosition(fire.Shooter.Position()),
				Yaw:              round2(float64(fire.Shooter.ViewDirectionX())),
				Pitch:            round2(float64(fire.Shooter.ViewDirectionY())),
				RecoilIndex:      float64(fire.Weapon.RecoilIndex()),
			})
		}
	}
}

// shotVisionGate rebuilds "enemy in vision" geometrically (frustum, los, no smoke)
// because the engine spotted flag lags 0-500ms. inVision is the wide accuracy/
// counter-strafe gate plus a recently-seen window; csVisible narrows it to rifles.
func (st *parseState) shotVisionGate(fire events.WeaponFire) (inVision, csVisible bool) {
	inVision = st.roundLive &&
		shooterHasVision(st.parsed.GameState(), fire.Shooter, st.vision.mesh, st.vision.activeSmokes, st.vision.engagements, st.cal.CSConeDeg, st.parsed.CurrentTime(), st.cal.CSRecentMs)
	csVisible = inVision && csdata.IsRifle(fire.Weapon)
	return inVision, csVisible
}

// accumulateSpray groups consecutive full-auto shots into a spray run. finalize
// uses it for hit ratio (rifles, visible enemy) and recoil deviation (any auto
// weapon). The spray-visible cone is tighter than the accuracy gate.
func (st *parseState) accumulateSpray(fire events.WeaponFire) {
	if !(st.roundLive && csdata.IsSprayWeapon(fire.Weapon)) {
		return
	}

	sprayVisible := st.roundLive && csdata.IsRifle(fire.Weapon) &&
		shooterHasVision(st.parsed.GameState(), fire.Shooter, st.vision.mesh, st.vision.activeSmokes, st.vision.engagements, st.cal.SprayConeDeg, st.parsed.CurrentTime(), 0)

	id := fire.Shooter.SteamID64
	now := st.parsed.CurrentTime()
	run := st.aim.curSpray[id]
	if last, ok := st.aim.lastShotTime[id]; ok && (now-last > sprayGap || (run != nil && run.weapon != fire.Weapon.Type)) {
		st.finalizeSpray(id)
		run = nil
	}
	if run == nil {
		run = &sprayRun{
			weapon:   fire.Weapon.Type,
			label:    fire.Weapon.String(),
			scoped:   fire.Shooter.IsScoped(),
			silenced: fire.Weapon.Silenced(),
		}
		st.aim.curSpray[id] = run
	}

	run.shotTimes = append(run.shotTimes, now.Microseconds())
	run.views = append(run.views, [2]float64{float64(fire.Shooter.ViewDirectionX()), float64(fire.Shooter.ViewDirectionY())})
	if sprayVisible { // only shots at a visible enemy feed the ratio
		run.visTimes = append(run.visTimes, now.Microseconds())
	}
	st.aim.lastShotTime[id] = now
}

// recordCounterStrafe tallies the counter-strafe stat off rifle shots with an
// enemy in vision, skipping fully crouched ones. counts as "stopped" when speed is
// under CSRatio of the weapon's max.
func (st *parseState) recordCounterStrafe(fire events.WeaponFire, csVisible bool) {
	if csVisible && !fire.Shooter.IsDucking() {
		speed := csdata.EngineSpeed(fire.Shooter) // engine's exact 2D speed
		if speed < 0 {
			speed = st.frames.playerSpeed[fire.Shooter.SteamID64] // fall back to the position delta
		}
		acc := st.aim.counterStrafes[fire.Shooter.SteamID64]
		if acc == nil {
			acc = &counterStrafeAcc{}
			st.aim.counterStrafes[fire.Shooter.SteamID64] = acc
		}

		acc.shots++
		acc.speedSum += speed
		if csdata.SpeedRatio(speed, fire.Weapon) < st.cal.CSRatio {
			acc.stopped++
		}
	}
}

// onItemPickup records a true pickup: a weapon (gun or grenade) whose original
// owner isn't the holder (buys/own re-grabs have owner 0 or self, and stay
// filtered out). FromEnemy means the original owner was on the opposing side this
// round. Live round only.
func (st *parseState) onItemPickup(e events.ItemPickup) {
	if st.parsed.GameState().IsWarmupPeriod() || !st.roundLive {
		return
	}
	if e.Player == nil || e.Player.SteamID64 == 0 || e.Weapon == nil {
		return
	}

	orig := weaponOriginalOwner(e.Weapon)
	if orig == 0 || orig == e.Player.SteamID64 {
		return // a buy or the holder's own gun, not a pickup
	}

	// original owner on the opposing side this round means an enemy gun.
	holderSide := st.roundSide(e.Player.SteamID64)
	origSide := st.roundSide(orig)
	fromEnemy := holderSide != "" && origSide != "" && holderSide != origSide

	st.track(e.Player.SteamID64, e.Player.Name)
	st.pending.Pickups = append(st.pending.Pickups, model.WeaponPickup{
		SteamID:          e.Player.SteamID64,
		Weapon:           e.Weapon.String(),
		OriginalOwner:    orig,
		FromEnemy:        fromEnemy,
		TimeMicroseconds: st.roundMicros(),
	})
}

// shotStat returns the per-round shot tally for a (shooter, weapon) pair,
// allocating the nested maps on first use.
func (st *parseState) shotStat(shooter uint64, weapon string) *shotStatAcc {
	byWeapon := st.shotStats[shooter]
	if byWeapon == nil {
		byWeapon = map[string]*shotStatAcc{}
		st.shotStats[shooter] = byWeapon
	}
	acc := byWeapon[weapon]
	if acc == nil {
		acc = &shotStatAcc{}
		byWeapon[weapon] = acc
	}
	return acc
}
