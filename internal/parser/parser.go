package parser

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"math"
	"runtime"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/f-gillmann/demolens/internal/geom"
	"github.com/f-gillmann/demolens/model"
	"github.com/golang/geo/r3"
	dem "github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/common"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/events"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/msg"
)

// Options turns on the expensive per-frame captures. Zero value is the cheap path.
type Options struct {
	PlayerFrames bool        // sample player pos + state each frame
	Shots        bool        // per-shot shooter geometry
	GrenadePaths bool        // grenade trajectories + bounces
	MapsDir      string      // .tri mesh dir for TTD los. empty disables TTD
	Calibration  Calibration // aim-stat thresholds, zero fields fall back to defaults
}

const frameSamplePeriod = 250 * time.Millisecond

// longest pause between two shots that still counts as one spray
const sprayGap = 300 * time.Millisecond

// blind has to last at least this long to count as "fully flashed"
const flashFullyBlind = 1100 * time.Millisecond

// who fully blinded a player, so we can credit the flasher if the victim dies blind
type pendingFlash struct {
	flasher uint64
	team    common.Team
	expire  time.Duration
}

func Parse(r io.Reader, opts Options) (_ *model.Match, err error) {
	hash := sha256.New()
	parsed := dem.NewParser(io.TeeReader(r, hash))
	defer func() {
		if closeErr := parsed.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}()

	match := &model.Match{}
	players := map[uint64]*model.Player{}
	cal := opts.Calibration.withDefaults()

	var roundStart time.Duration
	var roundEndTime time.Duration // when the round ended, for the exit-window duration
	var pending *model.Round
	var pendingPlayers map[uint64]*model.RoundPlayer
	var pendingGrenades map[int64]*model.Grenade // keyed by projectile UniqueID
	var entityToUnique map[int]int64             // grenade entity id to UniqueID, needed for landing events
	var liveInfernos map[int64]*liveInferno      // polled until they burn out
	var roundLive bool                           // true between freezetime end and round end
	var lastFrameSample time.Duration
	lastPos := map[uint64]model.Position{}
	lastPosTime := map[uint64]time.Duration{}
	// CS2 doesn't network velocity, so we derive speed from the frame-to-frame delta
	playerSpeed := map[uint64]float64{}
	counterStrafes := map[uint64]*counterStrafeAcc{}
	shotsAtEnemy := map[uint64]int{}
	hitsOnEnemy := map[uint64]int{}
	lastShotTime := map[uint64]time.Duration{}
	curSpray := map[uint64]*sprayRun{}
	sprayHits := map[uint64]int{}
	sprayShots := map[uint64]int{}
	sprayByWeapon := map[uint64]map[string]*sprayAgg{}
	sprayDev := map[uint64]map[string]*sprayDevAgg{} // per-weapon recoil deviation
	hitTimes := map[uint64][]int64{}                 // per shooter, demo times (us) a bullet hit an enemy, chronological

	// TTD los needs the map collision mesh. load it lazily once we know the map.
	var mesh *geom.Mesh
	meshTried := false
	activeSmokes := map[int]r3.Vector{}        // entityID to cloud pos while blooming
	engagements := map[[2]uint64]*engagement{} // (shooter,enemy) sighting state, wiped each round
	flashLead := map[uint64]pendingFlash{}     // wiped each round
	ttdSamples := map[uint64][]float64{}       // ms samples per shooter, averaged at the end
	ttdByVictim := map[[2]uint64][]float64{}   // same samples split by victim for the duel matrix
	crosshair := map[uint64][]float64{}        // crosshair-move samples in deg

	// reused across frames by the sighting handler so the los raycasts can run in
	// parallel without a per-frame alloc. live: alive players' eye+view. cands: the
	// frustum-passing pairs whose expensive los/smoke check is deferred to pass 2.
	type pv struct {
		id        uint64
		team      common.Team
		eye, view r3.Vector
	}
	type cand struct {
		en         *engagement
		sEye, eEye r3.Vector
		vis        bool
	}
	live := make([]pv, 0, 10)
	cands := make([]cand, 0, 32)

	// finalizeSpray closes a finished spray run. Needs 3+ consecutive shots of the
	// same auto weapon. Records recoil deviation for every such spray, plus the hit
	// ratio for rifles.
	finalizeSpray := func(id uint64) {
		run := curSpray[id]
		delete(curSpray, id)
		if run == nil || len(run.shotTimes) < 3 {
			return
		}
		// recoil deviation: player's per-shot aim trajectory vs the weapon pattern.
		// no visibility gate, this measures spray control not accuracy.
		if pat := sprayPatterns[run.weapon]; pat != nil && len(run.views) > 0 {
			if sprayDev[id] == nil {
				sprayDev[id] = map[string]*sprayDevAgg{}
			}
			da := sprayDev[id][run.label]
			if da == nil {
				da = &sprayDevAgg{weapon: run.weapon}
				sprayDev[id][run.label] = da
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
		win := int64(cal.SprayHitWindowMs * 1000)
		for _, t := range run.visTimes {
			if hitNear(hitTimes[id], t, win) { // shot landed a bullet on an enemy
				hits++
			}
		}
		sprayShots[id] += shots
		sprayHits[id] += hits
		if sprayByWeapon[id] == nil {
			sprayByWeapon[id] = map[string]*sprayAgg{}
		}
		a := sprayByWeapon[id][run.label]
		if a == nil {
			a = &sprayAgg{}
			sprayByWeapon[id][run.label] = a
		}
		a.hits += hits
		a.shots += shots
		a.sprays++
	}

	// A starts CT, B starts T. We flip this on every side switch so a player's
	// A/B identity stays put when the sides swap.
	sideToTeam := map[common.Team]string{
		common.TeamCounterTerrorists: "A",
		common.TeamTerrorists:        "B",
	}

	// upsert a player name into the match-level map
	track := func(id uint64, name string) {
		pl, ok := players[id]
		if !ok {
			pl = &model.Player{SteamID: id, Name: name}
			players[id] = pl
		}
		if name != "" {
			pl.Name = name
		}
	}

	// flush the current round (including post-round damage/shots) into the match
	finalize := func() {
		if pending == nil {
			return
		}
		pending.Players = finalizeRoundPlayers(pendingPlayers)
		pending.Grenades = finalizeGrenades(pendingGrenades)
		match.Rounds = append(match.Rounds, *pending)
		pending = nil
		pendingPlayers = nil
		pendingGrenades = nil
		entityToUnique = nil
		liveInfernos = nil
	}

	grenadeByEntity := func(entityID int) *model.Grenade {
		if pendingGrenades == nil {
			return nil
		}
		uid, ok := entityToUnique[entityID]
		if !ok {
			return nil
		}
		return pendingGrenades[uid]
	}

	parsed.RegisterNetMessageHandler(func(info *msg.CSVCMsg_ServerInfo) {
		match.Meta.MapName = info.GetMapName()
		match.Meta.IsHltv = info.GetIsHltv()
		match.Meta.IsDedicatedServer = info.GetIsDedicated()
		if match.Meta.ServerName == "" {
			match.Meta.ServerName = info.GetHostName()
		}
		if match.Meta.WorkshopID == "" {
			match.Meta.WorkshopID = info.GetAddonName()
		}
	})

	parsed.RegisterNetMessageHandler(func(header *msg.CDemoFileHeader) {
		match.Meta.BuildNum = strconv.Itoa(int(header.GetPatchVersion()))
		if sn := header.GetServerName(); sn != "" {
			match.Meta.ServerName = sn
		}
		match.Meta.ClientName = header.GetClientName()
		if a := header.GetAddons(); a != "" {
			match.Meta.WorkshopID = a
		}
	})

	// sides swap at halftime and at each OT half. flip the mapping so identity
	// stays anchored to the first-half side.
	parsed.RegisterEventHandler(func(events.TeamSideSwitch) {
		if parsed.GameState().IsWarmupPeriod() {
			return
		}
		ct, t := common.TeamCounterTerrorists, common.TeamTerrorists
		sideToTeam[ct], sideToTeam[t] = sideToTeam[t], sideToTeam[ct]
	})

	// freezetime end means buys are done and the round goes live. finalize the
	// previous round first (so its post-round events make it in), then open a new one.
	parsed.RegisterEventHandler(func(events.RoundFreezetimeEnd) {
		gs := parsed.GameState()
		if gs.IsWarmupPeriod() {
			return
		}
		finalize()

		if match.Meta.GameMode == "" {
			match.Meta.GameMode = gameMode(gs.Rules().ConVars())
		}
		if opts.MapsDir != "" && !meshTried {
			meshTried = true
			mesh, _ = geom.Load(geom.MapFile(opts.MapsDir, match.Meta.WorkshopID, match.Meta.MapName))
		}
		engagements = map[[2]uint64]*engagement{} // none of these carry across rounds
		flashLead = map[uint64]pendingFlash{}
		activeSmokes = map[int]r3.Vector{}
		roundStart = parsed.CurrentTime()
		pending = &model.Round{
			Number:  len(match.Rounds) + 1,
			Economy: roundEconomy(gs),
		}
		captureTeams(gs, players, sideToTeam)
		pendingPlayers = roundRoster(gs)
		pendingGrenades = map[int64]*model.Grenade{}
		entityToUnique = map[int]int64{}
		liveInfernos = map[int64]*liveInferno{}
		roundLive = true
	})

	// the next round's freezetime starting means the exit window just closed.
	// record how long it was open.
	parsed.RegisterEventHandler(func(events.RoundStart) {
		if parsed.GameState().IsWarmupPeriod() || pending == nil || roundLive {
			return
		}
		pending.PostRoundMicroseconds = (parsed.CurrentTime() - roundEndTime).Microseconds()
	})

	parsed.RegisterEventHandler(func(kill events.Kill) {
		if parsed.GameState().IsWarmupPeriod() {
			return
		}

		// exit kills (round already over) get their own bucket. they never touch
		// K/D and stay out of the kill timeline that clutch/opening/trade reads.
		if !roundLive {
			if pendingPlayers == nil {
				return
			}
			if kill.Killer != nil && kill.Killer.SteamID64 != 0 {
				track(kill.Killer.SteamID64, kill.Killer.Name)
				if rp := pendingPlayers[kill.Killer.SteamID64]; rp != nil {
					rp.ExitKills++
				}
			}
			if kill.Victim != nil && kill.Victim.SteamID64 != 0 {
				track(kill.Victim.SteamID64, kill.Victim.Name)
				if rp := pendingPlayers[kill.Victim.SteamID64]; rp != nil {
					rp.ExitDeaths++
				}
			}
			pending.ExitKills = append(pending.ExitKills, roundKill(kill, parsed.CurrentTime()-roundStart))
			return
		}

		if kill.Killer != nil && kill.Killer.SteamID64 != 0 {
			track(kill.Killer.SteamID64, kill.Killer.Name)
			if rp := pendingPlayers[kill.Killer.SteamID64]; rp != nil {
				rp.Kills++
				if kill.IsHeadshot {
					rp.Headshots++
				}
			}
		}

		if kill.Victim != nil && kill.Victim.SteamID64 != 0 {
			track(kill.Victim.SteamID64, kill.Victim.Name)
			if rp := pendingPlayers[kill.Victim.SteamID64]; rp != nil {
				rp.Deaths++
				// value of nades still in hand when they died
				rp.Utility.UnusedUtilityValue += grenadeInventoryValue(kill.Victim)
			}
			// flash-to-kill: victim died still fully blind, and the killer is on
			// the flasher's team. credit the flasher.
			if fl, ok := flashLead[kill.Victim.SteamID64]; ok {
				if parsed.CurrentTime() < fl.expire && kill.Killer != nil && kill.Killer.Team == fl.team {
					if rp := pendingPlayers[fl.flasher]; rp != nil {
						rp.Utility.FlashesLeadingToKill++
					}
				}
				delete(flashLead, kill.Victim.SteamID64)
			}
		}

		if kill.Assister != nil && kill.Assister.SteamID64 != 0 {
			track(kill.Assister.SteamID64, kill.Assister.Name)
			if rp := pendingPlayers[kill.Assister.SteamID64]; rp != nil {
				if kill.AssistedFlash {
					rp.FlashAssists++
				} else {
					rp.Assists++
				}
			}
		}

		rk := roundKill(kill, parsed.CurrentTime()-roundStart)
		rk.AlivePlayers = aliveSnapshot(parsed.GameState())
		if kill.Killer != nil {
			rk.KillerSpeed = playerSpeed[kill.Killer.SteamID64]
			rk.KillerSpeedRatio = speedRatio(rk.KillerSpeed, kill.Weapon)
		}
		pending.Kills = append(pending.Kills, rk)
	})

	parsed.RegisterEventHandler(func(hurt events.PlayerHurt) {
		if parsed.GameState().IsWarmupPeriod() {
			return
		}
		if hurt.Attacker == nil || hurt.Attacker.SteamID64 == 0 {
			return
		}

		// spotted-accuracy numerator: hits on an enemy actually in fov. same gate
		// as the denominator, which drops wallbangs and through-smoke hits where the
		// enemy wasn't really visible.
		if roundLive && isGun(hurt.Weapon) && hurt.Player != nil && hurt.Player.Team != hurt.Attacker.Team &&
			seesTarget(hurt.Attacker, hurt.Player, mesh, activeSmokes, engagements, cal.CSConeDeg, parsed.CurrentTime(), 0) {
			hitsOnEnemy[hurt.Attacker.SteamID64]++
		}
		// note the tick a rifle bullet hit an enemy, so a spray can tally its hits later.
		// one tick is one hit even on penetration.
		if roundLive && isRifle(hurt.Weapon) && hurt.Player != nil && hurt.Player.Team != hurt.Attacker.Team {
			id := hurt.Attacker.SteamID64
			hitTimes[id] = append(hitTimes[id], parsed.CurrentTime().Microseconds())
		}
		// first gun damage closes the sighting and yields the crosshair + TTD samples.
		// nade/molotov/zeus damage isn't an aim duel, so it must not log or consume here.
		if roundLive && isGun(hurt.Weapon) && hurt.Player != nil && hurt.Player.Team != hurt.Attacker.Team {
			key := [2]uint64{hurt.Attacker.SteamID64, hurt.Player.SteamID64}
			if en := engagements[key]; en != nil {
				id := hurt.Attacker.SteamID64
				if en.xPending { // crosshair = view move from appearance to this hit
					en.xPending = false
					crosshair[id] = append(crosshair[id], crosshairDelta(en.appearView, hurt.Attacker))
				}
				if en.tPending { // TTD = first-saw to this hit
					en.tPending, en.consumed = false, true
					ttd := float64((parsed.CurrentTime() - en.seeTime).Microseconds()) / 1000
					// keep the raw value. long trigger-discipline samples and other
					// outliers get clamped/trimmed at finalize, see adaptiveTTD.
					if ttd >= cal.TTDFloorMs {
						ttdSamples[id] = append(ttdSamples[id], ttd)
						vk := [2]uint64{id, hurt.Player.SteamID64}
						ttdByVictim[vk] = append(ttdByVictim[vk], ttd)
					}
				}
			}
		}

		// running totals, incl. post-round damage so they line up with HLTV.
		if rp := pendingPlayers[hurt.Attacker.SteamID64]; rp != nil {
			teamHit := hurt.Player != nil && hurt.Player.Team == hurt.Attacker.Team
			dmg := hurt.HealthDamageTaken
			if teamHit {
				rp.TeamDamage += dmg
			} else {
				rp.Damage += dmg
			}
			if hurt.Weapon != nil {
				switch hurt.Weapon.Type {
				case common.EqHE:
					if teamHit {
						rp.Utility.HETeamDamage += dmg
					} else {
						rp.Utility.HEDamage += dmg
					}
				case common.EqMolotov, common.EqIncendiary:
					if teamHit {
						rp.Utility.MolotovTeamDamage += dmg
					} else {
						rp.Utility.MolotovDamage += dmg
					}
				}
			}
		}

		// timeline, live round only. trade/clutch analysis reads this.
		if roundLive {
			pending.Damages = append(pending.Damages, damageEvent(hurt, parsed.CurrentTime()-roundStart))
		}
	})

	parsed.RegisterEventHandler(func(fire events.WeaponFire) {
		if parsed.GameState().IsWarmupPeriod() || fire.Shooter == nil || !isGun(fire.Weapon) {
			return
		}
		// count every gun shot, exit shots included, so the accuracy denominator
		// spans the same scope as hits (which include post-round damage).
		if rp := pendingPlayers[fire.Shooter.SteamID64]; rp != nil {
			rp.ShotsFired++
		}
		// spotted accuracy, counter-strafe and spray all gate on "enemy in vision":
		// inside the view frustum, clear los, not behind smoke. We rebuild vision
		// geometrically because the engine spotted flag lags 0-500ms and undercounts.
		// accuracy + counter-strafe use the wide gate plus a recently-seen window.
		// spray uses a tighter cone, only during the burst.
		inVision := roundLive &&
			shooterHasVision(parsed.GameState(), fire.Shooter, mesh, activeSmokes, engagements, cal.CSConeDeg, parsed.CurrentTime(), cal.CSRecentMs)
		csVisible := inVision && isRifle(fire.Weapon)
		// denominator: shots at an enemy in vision, recently-seen window included
		// (firing at someone you just watched duck still counts). numerator needs
		// the enemy actually visible at impact.
		if inVision {
			shotsAtEnemy[fire.Shooter.SteamID64]++
		}
		sprayVisible := roundLive && isRifle(fire.Weapon) &&
			shooterHasVision(parsed.GameState(), fire.Shooter, mesh, activeSmokes, engagements, cal.SprayConeDeg, parsed.CurrentTime(), 0)
		// group consecutive full-auto shots into a spray run. finalize uses it twice:
		// hit ratio (rifles, shots at a visible enemy) and recoil deviation (any auto
		// weapon, view trajectory vs pattern).
		if roundLive && isSprayWeapon(fire.Weapon) {
			id := fire.Shooter.SteamID64
			now := parsed.CurrentTime()
			run := curSpray[id]
			if last, ok := lastShotTime[id]; ok && (now-last > sprayGap || (run != nil && run.weapon != fire.Weapon.Type)) {
				finalizeSpray(id)
				run = nil
			}
			if run == nil {
				run = &sprayRun{weapon: fire.Weapon.Type, label: fire.Weapon.String()}
				curSpray[id] = run
			}
			run.shotTimes = append(run.shotTimes, now.Microseconds())
			run.views = append(run.views, [2]float64{float64(fire.Shooter.ViewDirectionX()), float64(fire.Shooter.ViewDirectionY())})
			if sprayVisible { // only shots at a visible enemy feed the ratio
				run.visTimes = append(run.visTimes, now.Microseconds())
			}
			lastShotTime[id] = now
		}
		// counter-strafe: rifle shots with an enemy in vision, skipping fully
		// crouched ones. counts as "stopped" when speed is under CSRatio of the
		// weapon's max.
		if csVisible && !fire.Shooter.IsDucking() {
			speed := engineSpeed(fire.Shooter) // engine's exact 2D speed
			if speed < 0 {
				speed = playerSpeed[fire.Shooter.SteamID64] // fall back to the position delta
			}
			acc := counterStrafes[fire.Shooter.SteamID64]
			if acc == nil {
				acc = &counterStrafeAcc{}
				counterStrafes[fire.Shooter.SteamID64] = acc
			}
			acc.shots++
			acc.speedSum += speed
			if speedRatio(speed, fire.Weapon) < cal.CSRatio {
				acc.stopped++
			}
		}
		if opts.Shots && roundLive && pending != nil {
			pending.Shots = append(pending.Shots, model.Shot{
				TimeMicroseconds: (parsed.CurrentTime() - roundStart).Microseconds(),
				Shooter:          fire.Shooter.SteamID64,
				Weapon:           fire.Weapon.String(),
				Position:         toPosition(fire.Shooter.Position()),
				Yaw:              float64(fire.Shooter.ViewDirectionX()),
				Pitch:            float64(fire.Shooter.ViewDirectionY()),
				RecoilIndex:      float64(fire.Weapon.RecoilIndex()),
			})
		}
	})

	parsed.RegisterEventHandler(func(thrown events.GrenadeProjectileThrow) {
		if parsed.GameState().IsWarmupPeriod() || thrown.Projectile == nil {
			return
		}
		nade := thrown.Projectile.WeaponInstance
		thrower := thrown.Projectile.Thrower
		if nade == nil || thrower == nil {
			return
		}
		rp := pendingPlayers[thrower.SteamID64]
		if rp == nil {
			return
		}
		switch nade.Type {
		case common.EqFlash:
			rp.Utility.FlashesThrown++
		case common.EqSmoke:
			rp.Utility.SmokesThrown++
		case common.EqHE:
			rp.Utility.HEsThrown++
		case common.EqMolotov, common.EqIncendiary:
			rp.Utility.MolotovsThrown++
		case common.EqDecoy:
			rp.Utility.DecoysThrown++
		}
		rp.Utility.UsedUtilityValue += utilityPrice[nade.Type]

		if pendingGrenades != nil {
			throwTime := (parsed.CurrentTime() - roundStart).Microseconds()
			pendingGrenades[thrown.Projectile.UniqueID()] = &model.Grenade{
				Thrower:               thrower.SteamID64,
				Side:                  rp.Side,
				Type:                  grenadeTypeString(nade.Type),
				ThrowTimeMicroseconds: throwTime,
				ThrowPosition:         grenadePosition(thrown.Projectile),
			}
			if e := thrown.Projectile.Entity; e != nil {
				entityToUnique[e.ID()] = thrown.Projectile.UniqueID()
			}
		}
	})

	// detonate marks when a grenade lands, pops or explodes.
	detonate := func(entityID int, pos model.Position, instant bool) {
		g := grenadeByEntity(entityID)
		if g == nil || g.DetonateTimeMicroseconds != 0 {
			return
		}
		t := (parsed.CurrentTime() - roundStart).Microseconds()
		g.DetonateTimeMicroseconds = t
		g.DetonatePosition = pos
		g.FlightMicroseconds = t - g.ThrowTimeMicroseconds
		if instant { // flash/he detonate and expire at the same instant
			g.ExpireTimeMicroseconds = t
		}
	}
	// expire marks when a timed grenade (smoke/fire/decoy) fades out.
	expire := func(entityID int) {
		g := grenadeByEntity(entityID)
		if g == nil || g.ExpireTimeMicroseconds != 0 {
			return
		}
		g.ExpireTimeMicroseconds = (parsed.CurrentTime() - roundStart).Microseconds()
	}
	grenadeEventPos := func(e events.GrenadeEvent) model.Position {
		return toPosition(e.Position)
	}

	parsed.RegisterEventHandler(func(e events.FlashExplode) { detonate(e.GrenadeEntityID, grenadeEventPos(e.GrenadeEvent), true) })
	parsed.RegisterEventHandler(func(e events.HeExplode) { detonate(e.GrenadeEntityID, grenadeEventPos(e.GrenadeEvent), true) })
	parsed.RegisterEventHandler(func(e events.SmokeStart) { detonate(e.GrenadeEntityID, grenadeEventPos(e.GrenadeEvent), false) })
	parsed.RegisterEventHandler(func(e events.DecoyStart) { detonate(e.GrenadeEntityID, grenadeEventPos(e.GrenadeEvent), false) })
	parsed.RegisterEventHandler(func(e events.SmokeExpired) { expire(e.GrenadeEntityID) })
	parsed.RegisterEventHandler(func(e events.DecoyExpired) { expire(e.GrenadeEntityID) })

	// fire grenades don't carry a projectile link; the inferno is its own entity.
	// FireGrenadeStart/Expired are flaky in CS2 too, so we match on thrower instead.
	parsed.RegisterEventHandler(func(e events.InfernoStart) {
		if e.Inferno == nil || pendingGrenades == nil {
			return
		}
		thrower := e.Inferno.Thrower()
		if thrower == nil {
			return
		}
		g := newestFireGrenade(pendingGrenades, thrower.SteamID64)
		if g == nil {
			return
		}
		g.DetonateTimeMicroseconds = (parsed.CurrentTime() - roundStart).Microseconds()
		g.DetonatePosition = toPosition(e.Inferno.Entity.Position())
		g.FlightMicroseconds = g.DetonateTimeMicroseconds - g.ThrowTimeMicroseconds
		liveInfernos[e.Inferno.UniqueID()] = &liveInferno{inferno: e.Inferno, grenade: g}
	})

	// flames go out well before the inferno entity is removed (InfernoExpired fires
	// ~20s later). there's no event for the actual burn-out, so we poll. guard it
	// hard: only when a fire is live, only after it's burned a bit, and only every
	// so often, so the no-fire case is a single map check per frame.
	parsed.RegisterEventHandler(func(events.FrameDone) {
		if len(liveInfernos) == 0 {
			return
		}
		cur := parsed.CurrentTime()
		for uid, li := range liveInfernos {
			if cur-li.lastChecked < fireCheckPeriod {
				continue
			}
			li.lastChecked = cur
			if len(li.inferno.Fires().Active().List()) > 0 {
				li.hadFire = true
				continue
			}
			if li.hadFire { // was burning, now all out (burned out or smoked off)
				li.grenade.ExpireTimeMicroseconds = (cur - roundStart).Microseconds()
				delete(liveInfernos, uid)
			}
		}
	})

	// entity removed. fallback expiry for the case the poll above never caught it.
	parsed.RegisterEventHandler(func(e events.InfernoExpired) {
		if e.Inferno == nil {
			return
		}
		uid := e.Inferno.UniqueID()
		if li := liveInfernos[uid]; li != nil {
			if li.grenade.ExpireTimeMicroseconds == 0 {
				li.grenade.ExpireTimeMicroseconds = (parsed.CurrentTime() - roundStart).Microseconds()
			}
			delete(liveInfernos, uid)
		}
	})

	parsed.RegisterEventHandler(func(e events.SmokeStart) {
		activeSmokes[e.GrenadeEntityID] = e.Position
	})
	parsed.RegisterEventHandler(func(e events.SmokeExpired) {
		delete(activeSmokes, e.GrenadeEntityID)
	})

	parsed.RegisterEventHandler(func(flash events.PlayerFlashed) {
		if parsed.GameState().IsWarmupPeriod() || flash.Attacker == nil || flash.Player == nil {
			return
		}
		rp := pendingPlayers[flash.Attacker.SteamID64]
		if rp == nil {
			return
		}
		self := flash.Attacker.SteamID64 == flash.Player.SteamID64
		sameTeam := self || flash.Player.Team == flash.Attacker.Team
		blind := int64(float64(flash.FlashDuration().Microseconds()) * cal.FlashBlindScale)
		// only "fully flashed" players count, i.e. blinded >= 1.1s. friendlies here
		// means the thrower's own team plus themselves.
		if blind >= flashFullyBlind.Microseconds() {
			if sameTeam {
				rp.Utility.TeammatesFlashed++
			} else {
				rp.Utility.EnemiesFlashed++
				rp.Utility.EnemyBlindMicroseconds += blind
				// arm the flash-to-kill credit in case this enemy dies still blind
				flashLead[flash.Player.SteamID64] = pendingFlash{
					flasher: flash.Attacker.SteamID64,
					team:    flash.Attacker.Team,
					expire:  parsed.CurrentTime() + flash.FlashDuration(),
				}
			}
		}

		// attach the blind to the grenade that caused it, for per-flash durations
		// and the flash matrix. self-flashes don't belong in who-blinded-whom.
		if !self && flash.Projectile != nil && pendingGrenades != nil {
			if g := pendingGrenades[flash.Projectile.UniqueID()]; g != nil {
				fp := model.FlashedPlayer{SteamID: flash.Player.SteamID64, BlindMicroseconds: blind}
				if vrp := pendingPlayers[flash.Player.SteamID64]; vrp != nil {
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
	})

	parsed.RegisterEventHandler(func(e events.BombPlanted) {
		if pending == nil {
			return
		}
		if pending.Bomb == nil {
			pending.Bomb = &model.Bomb{}
		}
		pending.Bomb.Site = bombSite(e.Site)
		pending.Bomb.PlantTimeMicroseconds = (parsed.CurrentTime() - roundStart).Microseconds()
		if e.Player != nil {
			track(e.Player.SteamID64, e.Player.Name)
			pending.Bomb.Planter = e.Player.SteamID64
			pending.Bomb.PlantPosition = positionOf(e.Player)
		}
	})

	parsed.RegisterEventHandler(func(e events.BombDefused) {
		if pending == nil || pending.Bomb == nil {
			return
		}
		pending.Bomb.Defused = true
		pending.Bomb.DefuseTimeMicroseconds = (parsed.CurrentTime() - roundStart).Microseconds()
		if e.Player != nil {
			track(e.Player.SteamID64, e.Player.Name)
			pending.Bomb.Defuser = e.Player.SteamID64
			pending.Bomb.DefusePosition = positionOf(e.Player)
		}
	})

	parsed.RegisterEventHandler(func(events.BombExplode) {
		if pending != nil && pending.Bomb != nil {
			pending.Bomb.Exploded = true
		}
	})

	// grenade trajectory (opt-in). demoinfocs keeps the whole flight path on the
	// projectile already, so just copy it out when the grenade is destroyed.
	parsed.RegisterEventHandler(func(e events.GrenadeProjectileDestroy) {
		if !opts.GrenadePaths || e.Projectile == nil || pendingGrenades == nil {
			return
		}
		if g := pendingGrenades[e.Projectile.UniqueID()]; g != nil {
			for _, t := range e.Projectile.Trajectory {
				g.Path = append(g.Path, toPosition(t.Position))
			}
		}
	})

	parsed.RegisterEventHandler(func(e events.GrenadeProjectileBounce) {
		if !opts.GrenadePaths || e.Projectile == nil || pendingGrenades == nil {
			return
		}
		if g := pendingGrenades[e.Projectile.UniqueID()]; g != nil {
			g.Bounces = append(g.Bounces, toPosition(e.Projectile.Position()))
		}
	})

	// per-frame player sampling (opt-in), throttled so output stays bounded.
	parsed.RegisterEventHandler(func(events.FrameDone) {
		if !opts.PlayerFrames || !roundLive || pending == nil {
			return
		}
		cur := parsed.CurrentTime()
		if cur-lastFrameSample < frameSamplePeriod {
			return
		}
		lastFrameSample = cur
		into := (cur - roundStart).Microseconds()
		for _, pl := range parsed.GameState().Participants().Playing() {
			if side := sideString(pl.Team); side != "" {
				pending.PlayerFrames = append(pending.PlayerFrames, playerFrame(pl, side, into))
			}
		}
	})

	// remember every player's position each frame. CS2 doesn't network velocity,
	// so kill speed comes from this frame-to-frame delta.
	parsed.RegisterEventHandler(func(events.FrameDone) {
		cur := parsed.CurrentTime()
		for _, pl := range parsed.GameState().Participants().Playing() {
			pos := toPosition(pl.Position())
			if prev, ok := lastPos[pl.SteamID64]; ok {
				if dt := (cur - lastPosTime[pl.SteamID64]).Seconds(); dt > 0 {
					playerSpeed[pl.SteamID64] = horizontalSpeed(pos, prev, dt)
				}
			}
			lastPos[pl.SteamID64] = pos
			lastPosTime[pl.SteamID64] = cur
		}
	})

	// engagement detection for the los metrics. an enemy entering the view cone
	// with clear los starts a sighting; the first hit closes it. the cheap angle
	// check gates the raycast so only a handful of rays fire per frame.
	parsed.RegisterEventHandler(func(events.FrameDone) {
		if !roundLive {
			return
		}
		now := parsed.CurrentTime()
		// notVisible applies the not-seen TTD branch: drop the visibility window and,
		// if the sighting has gone quiet past TTDGapMs, reset the clock.
		notVisible := func(en *engagement) {
			en.visSince = 0
			if (en.tPending || en.consumed) &&
				float64((now-en.lastSeen).Microseconds())/1000 > cal.TTDGapMs {
				en.tPending, en.consumed = false, false
			}
		}

		// pass 1 (sequential): pull each alive player's eye pos + view dir once up
		// front, so the pair loop is O(n) reads instead of O(n^2). reuse the buffer.
		live = live[:0]
		for _, pl := range parsed.GameState().Participants().Playing() {
			if !pl.IsAlive() {
				continue
			}
			if eye, ok := pl.PositionEyes(); ok {
				live = append(live, pv{pl.SteamID64, pl.Team, eye, viewVector(pl)})
			}
		}
		cands = cands[:0]
		for i := range live {
			s := live[i]
			for j := range live {
				e := live[j]
				if e.team == s.team {
					continue
				}
				dir := e.eye.Sub(s.eye)
				key := [2]uint64{s.id, e.id}
				en := engagements[key]
				if en == nil {
					en = &engagement{}
					engagements[key] = en
				}
				// crosshair placement: snapshot the view the moment the enemy hits the
				// appearance frustum. the move from there to the hit is the placement.
				// frustum only, no los gate: appearance fires earlier than a strict wall
				// raycast, and gating on los here undershoots.
				xIn := enemyInFrustum(s.view, dir, cal.CrosshairConeDeg)
				if xIn && !en.xIn && !en.xPending {
					en.xPending, en.appearView = true, s.view
				} else if !xIn && en.xIn && en.xPending {
					en.xPending = false
				}
				en.xIn = xIn
				// TTD clock starts when the enemy is first seen: inside the frustum,
				// clear los, not through smoke. it must stay visible for TTDDebounceMs
				// before the clock commits, which kills 1-tick grazes. then it's back-dated
				// to first-visible. brief look-aways don't break it. once the enemy is
				// unseen for TTDGapMs the sighting resets, so re-peeking someone you just
				// saw isn't counted as a fresh duel.
				// the cheap frustum/mesh gate stays here; the expensive los+smoke check
				// is deferred to pass 2 so it can run concurrently.
				ttdCand := enemyInFrustum(s.view, dir, cal.TTDFovDeg) && mesh != nil
				if ttdCand {
					cands = append(cands, cand{en: en, sEye: s.eye, eEye: e.eye})
				} else {
					notVisible(en)
				}
			}
		}

		// pass 2 (parallel): the los raycast + smoke test for each cand. mesh and
		// activeSmokes are read-only for the whole handler, so concurrent reads are
		// safe; each goroutine only writes its own disjoint cand.vis indices.
		if len(cands) >= 16 {
			workers := runtime.GOMAXPROCS(0)
			if workers > len(cands) {
				workers = len(cands)
			}
			chunk := (len(cands) + workers - 1) / workers
			var wg sync.WaitGroup
			for w := 0; w < workers; w++ {
				lo := w * chunk
				if lo >= len(cands) {
					break
				}
				hi := lo + chunk
				if hi > len(cands) {
					hi = len(cands)
				}
				wg.Add(1)
				go func(lo, hi int) {
					defer wg.Done()
					for k := lo; k < hi; k++ {
						c := &cands[k]
						c.vis = losClear(mesh, c.sEye, c.eEye) &&
							!smokeBlocked(c.sEye, c.eEye, activeSmokes)
					}
				}(lo, hi)
			}
			wg.Wait()
		} else {
			for k := range cands {
				c := &cands[k]
				c.vis = losClear(mesh, c.sEye, c.eEye) &&
					!smokeBlocked(c.sEye, c.eEye, activeSmokes)
			}
		}

		// pass 3 (sequential): the TTD state machine over each cand. each en is
		// touched once for TTD this frame (a pair is a cand or a non-cand, never
		// both), so pair order doesn't change the final state.
		for k := range cands {
			c := &cands[k]
			en := c.en
			if c.vis {
				if en.visSince == 0 {
					en.visSince = now
				}
				if !en.tPending && !en.consumed &&
					float64((now-en.visSince).Microseconds())/1000 >= cal.TTDDebounceMs {
					en.tPending, en.seeTime = true, en.visSince
				}
				en.lastSeen = now
			} else {
				notVisible(en)
			}
		}
	})

	parsed.RegisterEventHandler(func(end events.RoundEnd) {
		if parsed.GameState().IsWarmupPeriod() || pending == nil {
			return
		}
		pending.WinnerSide = sideString(end.Winner)
		pending.WinnerTeam = sideToTeam[end.Winner]
		pending.Reason = reasonString(end.Reason)
		roundEndTime = parsed.CurrentTime()
		roundLive = false // post-round: damage/shots/exit-kills still count, K/D doesn't
	})

	// CS2 GOTV demos tend to end on a truncated final fragment. gameplay is all
	// there by then, so swallow ErrUnexpectedEndOfDemo.
	if err := parsed.ParseToEnd(); err != nil && !errors.Is(err, dem.ErrUnexpectedEndOfDemo) {
		return nil, err
	}

	// append the last round only if it ended cleanly. a round still live at demo
	// end got cut off, so drop it (same as the old behaviour).
	if !roundLive {
		finalize()
	}

	match.Meta.TickRate = parsed.TickRate()
	match.Meta.DurationMicroseconds = parsed.CurrentTime().Microseconds()
	match.Meta.TotalRounds = len(match.Rounds)
	match.Meta.ServerPlatform = guessSource(match.Meta.ServerName)
	match.Meta.DemoType = demoType(match.Meta.IsHltv, match.Meta.ClientName)

	// score = each team's round wins. counted from our own A/B winner so the side
	// swap doesn't matter (the engine's per-side score follows the slot, not the team).
	for _, r := range match.Rounds {
		switch r.WinnerTeam {
		case "A":
			match.Meta.Score.TeamA++
		case "B":
			match.Meta.Score.TeamB++
		}
	}

	// clan names, pulled off the players on each side
	for _, pl := range players {
		if pl.TeamName == "" {
			continue
		}
		if pl.Team == "A" && match.Meta.Score.TeamAName == "" {
			match.Meta.Score.TeamAName = pl.TeamName
		} else if pl.Team == "B" && match.Meta.Score.TeamBName == "" {
			match.Meta.Score.TeamBName = pl.TeamName
		}
	}

	// duel matrix: for each enemy killer/victim pair, kills, damage, per-weapon
	// kills and average TTD. lives here rather than in metrics because it needs the
	// TTD samples; the kill/damage half is duplicated in the rounds.
	match.DuelMatrix = buildDuelMatrix(match.Rounds, players, ttdByVictim, cal)

	for id, acc := range counterStrafes {
		if pl := players[id]; pl != nil && acc.shots > 0 {
			pl.CounterStrafe = &model.CounterStrafe{
				Shots:       acc.shots,
				Stopped:     acc.stopped,
				StoppedRate: float64(acc.stopped) / float64(acc.shots) * 100,
				AvgSpeed:    acc.speedSum / float64(acc.shots),
			}
		}
	}

	for id, shots := range shotsAtEnemy {
		if pl := players[id]; pl != nil && shots > 0 {
			pl.SpottedShots = shots
			pl.SpottedHits = hitsOnEnemy[id]
			pl.SpottedAccuracy = float64(hitsOnEnemy[id]) / float64(shots) * 100
		}
	}

	var sprayIDs []uint64
	for id := range curSpray {
		sprayIDs = append(sprayIDs, id)
	}
	for _, id := range sprayIDs {
		finalizeSpray(id) // flush any spray still open at demo end
	}
	for id, shots := range sprayShots {
		if pl := players[id]; pl != nil && shots > 0 {
			pl.SprayAccuracy = float64(sprayHits[id]) / float64(shots) * 100
		}
	}
	for id, byw := range sprayByWeapon {
		pl := players[id]
		if pl == nil {
			continue
		}
		pl.SprayWeapons = map[string]model.WeaponSpray{}
		for label, a := range byw {
			if a.shots > 0 {
				pl.SprayWeapons[label] = model.WeaponSpray{Sprays: a.sprays, Accuracy: float64(a.hits) / float64(a.shots) * 100}
			}
		}
	}

	// recoil deviation per player per weapon: average aim trajectory vs the ideal.
	// the ideal is the pattern negated, i.e. the move that would cancel it out.
	for id, byw := range sprayDev {
		pl := players[id]
		if pl == nil {
			continue
		}
		pl.SprayPatterns = map[string]model.SprayDeviation{}
		for label, da := range byw {
			pat := sprayPatterns[da.weapon]
			dev := model.SprayDeviation{Sprays: da.sprays}
			var devSum float64
			var devN int
			for i := range da.sumX {
				if da.n[i] == 0 {
					continue
				}
				px, py := da.sumX[i]/float64(da.n[i]), da.sumY[i]/float64(da.n[i])
				sx, sy := -pat[i].X, -pat[i].Y
				dev.Bullets = append(dev.Bullets, model.SprayBullet{
					Index: i, ShouldX: sx, ShouldY: sy, PlayerX: px, PlayerY: py,
				})
				dx, dy := px-sx, py-sy
				devSum += math.Sqrt(dx*dx + dy*dy)
				devN++
			}
			if devN > 0 {
				dev.AvgDeviation = devSum / float64(devN)
			}
			pl.SprayPatterns[label] = dev
		}
	}

	for id, samples := range ttdSamples {
		if pl := players[id]; pl != nil && len(samples) > 0 {
			pl.TimeToDamage = adaptiveTTD(samples, cal.TTDOutlierFactor, cal.TTDClampMs)
			pl.TimeToDamageSamples = len(samples)
		}
	}
	for id, samples := range crosshair {
		if pl := players[id]; pl != nil && len(samples) > 0 {
			sort.Float64s(samples)
			pl.CrosshairPlacement = median(samples)
			pl.CrosshairSamples = len(samples)
		}
	}

	for _, pl := range players {
		match.Players = append(match.Players, *pl)
	}
	sort.Slice(match.Players, func(i, j int) bool {
		return match.Players[i].SteamID < match.Players[j].SteamID
	})

	match.FileHash = hex.EncodeToString(hash.Sum(nil))
	return match, nil
}

// buildDuelMatrix builds the head-to-head record for every enemy killer/victim
// pair: kills, damage, per-weapon kills, and average TTD.
func buildDuelMatrix(rounds []model.Round, players map[uint64]*model.Player, ttd map[[2]uint64][]float64, cal Calibration) []model.Duel {
	type agg struct {
		kills, damage int
		weapons       map[string]int
	}
	pairs := map[[2]uint64]*agg{}
	enemies := func(a, b uint64) bool {
		pa, pb := players[a], players[b]
		return pa != nil && pb != nil && pa.Team != "" && pa.Team != pb.Team
	}
	get := func(k [2]uint64) *agg {
		a := pairs[k]
		if a == nil {
			a = &agg{weapons: map[string]int{}}
			pairs[k] = a
		}
		return a
	}
	for _, r := range rounds {
		for _, k := range r.Kills {
			if k.Killer == 0 || k.Victim == 0 || !enemies(k.Killer, k.Victim) {
				continue
			}
			a := get([2]uint64{k.Killer, k.Victim})
			a.kills++
			if k.Weapon != "" {
				a.weapons[k.Weapon]++
			}
		}
		for _, d := range r.Damages {
			if d.Attacker == 0 || d.Victim == 0 || !enemies(d.Attacker, d.Victim) {
				continue
			}
			get([2]uint64{d.Attacker, d.Victim}).damage += d.HealthDamage
		}
	}
	duels := make([]model.Duel, 0, len(pairs))
	for k, a := range pairs {
		duel := model.Duel{Killer: k[0], Victim: k[1], Kills: a.kills, Damage: a.damage}
		if len(a.weapons) > 0 {
			duel.Weapons = a.weapons
		}
		if s := ttd[k]; len(s) > 0 {
			duel.TimeToDamage = adaptiveTTD(s, cal.TTDOutlierFactor, cal.TTDClampMs)
		}
		duels = append(duels, duel)
	}
	sort.Slice(duels, func(i, j int) bool {
		if duels[i].Killer != duels[j].Killer {
			return duels[i].Killer < duels[j].Killer
		}
		return duels[i].Victim < duels[j].Victim
	})
	return duels
}

// finalizeRoundPlayers flattens the roster map into a SteamID-sorted slice so
// output is deterministic.
func finalizeRoundPlayers(roster map[uint64]*model.RoundPlayer) []model.RoundPlayer {
	ids := make([]uint64, 0, len(roster))
	for id := range roster {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })

	out := make([]model.RoundPlayer, 0, len(ids))
	for _, id := range ids {
		out = append(out, *roster[id])
	}
	return out
}

// how often we poll a live inferno for burn-out
const fireCheckPeriod = 200 * time.Millisecond

// liveInferno is an active fire we poll to catch its burn-out (all flames gone,
// whether it burned out on its own or got smoked off).
type liveInferno struct {
	inferno     *common.Inferno
	grenade     *model.Grenade
	hadFire     bool // saw at least one flame. guards the pre-ignition frame.
	lastChecked time.Duration
}

// newestFireGrenade finds the thrower's most recent molotov/incendiary that hasn't
// landed yet, so we can link an inferno back to its grenade.
func newestFireGrenade(grenades map[int64]*model.Grenade, throwerID uint64) *model.Grenade {
	var best *model.Grenade
	for _, g := range grenades {
		if g.Thrower != throwerID || g.DetonateTimeMicroseconds != 0 {
			continue
		}
		if g.Type != "molotov" && g.Type != "incendiary" {
			continue
		}
		if best == nil || g.ThrowTimeMicroseconds > best.ThrowTimeMicroseconds {
			best = g
		}
	}
	return best
}

// finalizeGrenades flattens the grenade map into a slice ordered by throw time.
func finalizeGrenades(grenades map[int64]*model.Grenade) []model.Grenade {
	out := make([]model.Grenade, 0, len(grenades))
	for _, g := range grenades {
		out = append(out, *g)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ThrowTimeMicroseconds != out[j].ThrowTimeMicroseconds {
			return out[i].ThrowTimeMicroseconds < out[j].ThrowTimeMicroseconds
		}
		return out[i].Thrower < out[j].Thrower
	})
	return out
}
