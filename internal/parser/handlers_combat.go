package parser

import (
	"github.com/f-gillmann/demolens/internal/csdata"
	"github.com/f-gillmann/demolens/model"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/common"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/events"
)

// finalizeSpray closes a finished spray run. Needs 3+ consecutive shots of the
// same auto weapon. Records recoil deviation for every such spray, plus the hit
// ratio for rifles.
func (st *parseState) finalizeSpray(id uint64) {
	run := st.curSpray[id]
	delete(st.curSpray, id)
	if run == nil || len(run.shotTimes) < 3 {
		return
	}

	// recoil deviation: player's per-shot aim trajectory vs the weapon pattern.
	// no visibility gate, this measures spray control not accuracy.
	if pat := sprayPatterns[run.weapon]; pat != nil && len(run.views) > 0 {
		if st.sprayDev[id] == nil {
			st.sprayDev[id] = map[string]*sprayDevAgg{}
		}
		da := st.sprayDev[id][run.label]
		if da == nil {
			da = &sprayDevAgg{weapon: run.weapon}
			st.sprayDev[id][run.label] = da
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
		if hitNear(st.hitTimes[id], t, win) { // shot landed a bullet on an enemy
			hits++
		}
	}

	st.sprayShots[id] += shots
	st.sprayHits[id] += hits
	if st.sprayByWeapon[id] == nil {
		st.sprayByWeapon[id] = map[string]*sprayAgg{}
	}
	a := st.sprayByWeapon[id][run.label]
	if a == nil {
		a = &sprayAgg{}
		st.sprayByWeapon[id][run.label] = a
	}
	a.hits += hits
	a.shots += shots
	a.sprays++
}

func (st *parseState) onKill(kill events.Kill) {
	if st.parsed.GameState().IsWarmupPeriod() {
		return
	}

	// exit kills (round already over) get their own bucket. they never touch
	// K/D and stay out of the kill timeline that clutch/opening/trade reads.
	if !st.roundLive {
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
		return
	}

	if kill.Killer != nil && kill.Killer.SteamID64 != 0 {
		st.track(kill.Killer.SteamID64, kill.Killer.Name)
		if roundPlayer := st.pendingPlayers[kill.Killer.SteamID64]; roundPlayer != nil {
			roundPlayer.Kills++
			if kill.IsHeadshot {
				roundPlayer.Headshots++
			}
		}
	}

	if kill.Victim != nil && kill.Victim.SteamID64 != 0 {
		st.track(kill.Victim.SteamID64, kill.Victim.Name)
		if roundPlayer := st.pendingPlayers[kill.Victim.SteamID64]; roundPlayer != nil {
			roundPlayer.Deaths++
			// value of nades still in hand when they died
			roundPlayer.Utility.UnusedUtilityValue += grenadeInventoryValue(kill.Victim)
		}
		// flash-to-kill: victim died still fully blind, and the killer is on
		// the flasher's team. credit the flasher.
		if fl, ok := st.flashLead[kill.Victim.SteamID64]; ok {
			if st.parsed.CurrentTime() < fl.expire && kill.Killer != nil && kill.Killer.Team == fl.team {
				if roundPlayer := st.pendingPlayers[fl.flasher]; roundPlayer != nil {
					roundPlayer.Utility.FlashesLeadingToKill++
				}
			}
			delete(st.flashLead, kill.Victim.SteamID64)
		}
	}

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

	rk := roundKill(kill, st.parsed.CurrentTime()-st.roundStart)
	rk.AlivePlayers = aliveSnapshot(st.parsed.GameState())
	if kill.Killer != nil {
		rk.KillerSpeed = st.playerSpeed[kill.Killer.SteamID64]
		rk.KillerSpeedRatio = csdata.SpeedRatio(rk.KillerSpeed, kill.Weapon)
	}
	st.pending.Kills = append(st.pending.Kills, rk)
}

func (st *parseState) onPlayerHurt(hurt events.PlayerHurt) {
	if st.parsed.GameState().IsWarmupPeriod() {
		return
	}
	if hurt.Attacker == nil || hurt.Attacker.SteamID64 == 0 {
		return
	}

	// spotted-accuracy numerator: hits on an enemy actually in fov, same gate as the denominator (drops wallbangs and through-smoke hits).
	if st.roundLive && csdata.IsGun(hurt.Weapon) && hurt.Player != nil && hurt.Player.Team != hurt.Attacker.Team &&
		seesTarget(hurt.Attacker, hurt.Player, st.mesh, st.activeSmokes, st.engagements, st.cal.CSConeDeg, st.parsed.CurrentTime(), 0) {
		st.hitsOnEnemy[hurt.Attacker.SteamID64]++
	}

	// note the tick a rifle bullet hit an enemy, so a spray can tally its hits later.
	// one tick is one hit even on penetration.
	if st.roundLive && csdata.IsRifle(hurt.Weapon) && hurt.Player != nil && hurt.Player.Team != hurt.Attacker.Team {
		id := hurt.Attacker.SteamID64
		st.hitTimes[id] = append(st.hitTimes[id], st.parsed.CurrentTime().Microseconds())
	}

	// first gun damage closes the sighting and yields the crosshair + TTD samples.
	// nade/molotov/zeus damage isn't an aim duel, so it must not log or consume here.
	if st.roundLive && csdata.IsGun(hurt.Weapon) && hurt.Player != nil && hurt.Player.Team != hurt.Attacker.Team {
		key := [2]uint64{hurt.Attacker.SteamID64, hurt.Player.SteamID64}
		if eng := st.engagements[key]; eng != nil {
			id := hurt.Attacker.SteamID64
			if eng.crosshairPending { // crosshair = view move from appearance to this hit
				eng.crosshairPending = false
				st.crosshair[id] = append(st.crosshair[id], crosshairDelta(eng.appearView, hurt.Attacker))
			}

			if eng.ttdRunning { // TTD = first-saw to this hit
				eng.ttdRunning, eng.consumed = false, true
				ttd := float64((st.parsed.CurrentTime() - eng.seeTime).Microseconds()) / 1000
				// keep the raw value. long trigger-discipline samples and other
				// outliers get clamped/trimmed at finalize, see adaptiveTTD.
				if ttd >= st.cal.TTDFloorMs {
					st.ttdSamples[id] = append(st.ttdSamples[id], ttd)
					vk := [2]uint64{id, hurt.Player.SteamID64}
					st.ttdByVictim[vk] = append(st.ttdByVictim[vk], ttd)
				}
			}
		}
	}

	// running totals, incl. post-round damage so they line up with HLTV.
	if roundPlayer := st.pendingPlayers[hurt.Attacker.SteamID64]; roundPlayer != nil {
		teamHit := hurt.Player != nil && hurt.Player.Team == hurt.Attacker.Team
		dmg := hurt.HealthDamageTaken
		if teamHit {
			roundPlayer.TeamDamage += dmg
		} else {
			roundPlayer.Damage += dmg
		}
		if hurt.Weapon != nil {
			switch hurt.Weapon.Type {
			case common.EqHE:
				if teamHit {
					roundPlayer.Utility.HETeamDamage += dmg
				} else {
					roundPlayer.Utility.HEDamage += dmg
				}
			case common.EqMolotov, common.EqIncendiary:
				if teamHit {
					roundPlayer.Utility.MolotovTeamDamage += dmg
				} else {
					roundPlayer.Utility.MolotovDamage += dmg
				}
			}
		}
	}

	// timeline, live round only. trade/clutch analysis reads this.
	if st.roundLive {
		st.pending.Damages = append(st.pending.Damages, damageEvent(hurt, st.parsed.CurrentTime()-st.roundStart))
	}
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

	// spotted accuracy, counter-strafe and spray all gate on "enemy in vision":
	// inside the view frustum, clear los, not behind smoke. We rebuild vision
	// geometrically because the engine spotted flag lags 0-500ms and undercounts.
	// accuracy + counter-strafe use the wide gate plus a recently-seen window.
	// spray uses a tighter cone, only during the burst.
	inVision := st.roundLive &&
		shooterHasVision(st.parsed.GameState(), fire.Shooter, st.mesh, st.activeSmokes, st.engagements, st.cal.CSConeDeg, st.parsed.CurrentTime(), st.cal.CSRecentMs)
	csVisible := inVision && csdata.IsRifle(fire.Weapon)

	// denominator: shots at an enemy in vision, recently-seen window included
	// (firing at someone you just watched duck still counts). numerator needs
	// the enemy actually visible at impact.
	if inVision {
		st.shotsAtEnemy[fire.Shooter.SteamID64]++
	}

	sprayVisible := st.roundLive && csdata.IsRifle(fire.Weapon) &&
		shooterHasVision(st.parsed.GameState(), fire.Shooter, st.mesh, st.activeSmokes, st.engagements, st.cal.SprayConeDeg, st.parsed.CurrentTime(), 0)

	// group consecutive full-auto shots into a spray run. finalize uses it twice:
	// hit ratio (rifles, shots at a visible enemy) and recoil deviation (any auto
	// weapon, view trajectory vs pattern).
	if st.roundLive && csdata.IsSprayWeapon(fire.Weapon) {
		id := fire.Shooter.SteamID64
		now := st.parsed.CurrentTime()
		run := st.curSpray[id]
		if last, ok := st.lastShotTime[id]; ok && (now-last > sprayGap || (run != nil && run.weapon != fire.Weapon.Type)) {
			st.finalizeSpray(id)
			run = nil
		}
		if run == nil {
			run = &sprayRun{weapon: fire.Weapon.Type, label: fire.Weapon.String()}
			st.curSpray[id] = run
		}

		run.shotTimes = append(run.shotTimes, now.Microseconds())
		run.views = append(run.views, [2]float64{float64(fire.Shooter.ViewDirectionX()), float64(fire.Shooter.ViewDirectionY())})
		if sprayVisible { // only shots at a visible enemy feed the ratio
			run.visTimes = append(run.visTimes, now.Microseconds())
		}
		st.lastShotTime[id] = now
	}

	// counter-strafe: rifle shots with an enemy in vision, skipping fully
	// crouched ones. counts as "stopped" when speed is under CSRatio of the
	// weapon's max.
	if csVisible && !fire.Shooter.IsDucking() {
		speed := csdata.EngineSpeed(fire.Shooter) // engine's exact 2D speed
		if speed < 0 {
			speed = st.playerSpeed[fire.Shooter.SteamID64] // fall back to the position delta
		}
		acc := st.counterStrafes[fire.Shooter.SteamID64]
		if acc == nil {
			acc = &counterStrafeAcc{}
			st.counterStrafes[fire.Shooter.SteamID64] = acc
		}

		acc.shots++
		acc.speedSum += speed
		if csdata.SpeedRatio(speed, fire.Weapon) < st.cal.CSRatio {
			acc.stopped++
		}
	}

	if st.opts.Shots && st.roundLive && st.pending != nil {
		st.pending.Shots = append(st.pending.Shots, model.Shot{
			TimeMicroseconds: (st.parsed.CurrentTime() - st.roundStart).Microseconds(),
			Shooter:          fire.Shooter.SteamID64,
			Weapon:           fire.Weapon.String(),
			Position:         toPosition(fire.Shooter.Position()),
			Yaw:              float64(fire.Shooter.ViewDirectionX()),
			Pitch:            float64(fire.Shooter.ViewDirectionY()),
			RecoilIndex:      float64(fire.Weapon.RecoilIndex()),
		})
	}
}
