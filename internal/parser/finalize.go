package parser

import (
	"math"
	"sort"
	"strconv"
	"time"

	"github.com/f-gillmann/demolens/v2/internal/csdata"
	"github.com/f-gillmann/demolens/v2/model"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/common"
)

// how often we poll a live inferno for burn-out
const fireCheckPeriod = 200 * time.Millisecond

// c4 shockwave constants. the 2026-07-08 update (first demo build c4WaveBuild) made
// the planted bomb hurt via an expanding euclidean sphere at c4WaveSpeed game units
// per second: the func_bomb_target bomb_damage_power default, validated on demo
// arrival ticks. the speed is NOT networked, so we surface the constant on qualifying
// demos. pre-rework demos hurt every victim on the same tick and carry no wave.
const (
	c4WaveSpeed = 3000
	c4WaveBuild = 14168
)

// liveInferno is an active fire we poll to catch its burn-out (all flames gone,
// whether it burned out on its own or got smoked off).
type liveInferno struct {
	inferno       *common.Inferno
	grenade       *parseGrenade
	hadFire       bool // saw at least one flame. guards the pre-ignition frame.
	lastChecked   time.Duration
	peakFireCount int // widest active-flame count seen, gates the fire_cells snapshot
}

// finalizeMatch runs the post-parse aggregation: match meta, duel matrix, and the
// per-player counter-strafe/spotted/spray/TTD/crosshair stats.
func (st *parseState) finalizeMatch() {
	st.match.SchemaVersion = 6
	st.match.Meta.TickRate = st.parsed.TickRate()
	st.match.Meta.DurationMs = st.parsed.CurrentTime().Milliseconds()
	st.match.Meta.TotalRounds = len(st.match.Rounds)
	st.match.Meta.ServerPlatform = guessSource(st.match.Meta.ServerName)
	st.match.Meta.DemoType = demoType(st.match.Meta.IsHltv, st.match.Meta.ClientName)
	// surface the c4 shockwave speed on post-rework demos only. build_num is a string;
	// a non-numeric value leaves the field absent.
	if bn, err := strconv.Atoi(st.match.Meta.BuildNum); err == nil && bn >= c4WaveBuild {
		st.match.Meta.C4WaveSpeed = c4WaveSpeed
	}

	st.finalizeOutputMeta()
	st.finalizeScore()
	st.finalizeClanNames()

	// duel matrix: for each enemy killer/victim pair, kills, damage, per-weapon
	// kills and average TTD. lives here rather than in metrics because it needs the
	// TTD samples; the kill/damage half is duplicated in the rounds.
	st.match.Stats.DuelPairs = buildDuelMatrix(st.match.Rounds, st.players, st.aim.ttdByVictim, st.cal)

	st.finalizeCounterStrafe()
	st.finalizeSpottedAccuracy()
	st.finalizeSprayStats()
	st.finalizeSprayDeviation()
	st.finalizeTTD()
	st.finalizeCrosshair()
	st.finalizePlayers()

	if st.aimDump != nil {
		_ = st.aimDump.close()
	}
}

// finalizeOutputMeta records how this document was produced: which tier/streams
// are on, the positions sample rate (only when positions are on), and whether the
// collision mesh loaded (the gate for the geometric LOS stats).
func (st *parseState) finalizeOutputMeta() {
	st.match.Meta.Output = model.OutputMeta{
		Tier:          st.opts.tierName(),
		Streams:       st.opts.enabledStreamNames(),
		MapMeshLoaded: st.vision.mesh != nil,
	}
	if st.opts.PlayerFrames {
		st.match.Meta.Output.PositionsSampleHz = float64(time.Second) / float64(st.framePeriod)
		st.match.Meta.Output.PositionsFields = model.PositionFields
	}
	if st.opts.GroundItems {
		st.match.Meta.Output.GroundItemPositionsFields = model.GroundItemPositionFields
	}
}

// finalizeScore tallies each team's round wins. counted from our own A/B winner so
// the side swap doesn't matter (the engine's per-side score follows the slot, not
// the team).
func (st *parseState) finalizeScore() {
	for _, r := range st.match.Rounds {
		switch r.WinnerTeam {
		case "A":
			st.match.Meta.Score.TeamA++
		case "B":
			st.match.Meta.Score.TeamB++
		}
	}
}

// finalizeClanNames pulls each side's clan name off the players on that side.
func (st *parseState) finalizeClanNames() {
	for _, pl := range st.players {
		if pl.TeamName == "" {
			continue
		}
		if pl.Team == "A" && st.match.Meta.Score.TeamAName == "" {
			st.match.Meta.Score.TeamAName = pl.TeamName
		} else if pl.Team == "B" && st.match.Meta.Score.TeamBName == "" {
			st.match.Meta.Score.TeamBName = pl.TeamName
		}
	}
}

// finalizeCounterStrafe folds each player's counter-strafe tally into their
// stopped rate and average speed.
func (st *parseState) finalizeCounterStrafe() {
	for id, acc := range st.aim.counterStrafes {
		if pl := st.players[id]; pl != nil && acc.shots > 0 {
			pl.CounterStrafe = &model.CounterStrafe{
				Shots:       acc.shots,
				Stopped:     acc.stopped,
				StoppedRate: float64(acc.stopped) / float64(acc.shots) * 100,
				AvgSpeed:    acc.speedSum / float64(acc.shots),
			}
		}
	}
}

// finalizeSpottedAccuracy folds the spotted shots/hits tallies into each player's
// spotted accuracy.
func (st *parseState) finalizeSpottedAccuracy() {
	for id, shots := range st.aim.shotsAtEnemy {
		if pl := st.players[id]; pl != nil && shots > 0 {
			pl.SpottedShots = shots
			pl.SpottedHits = st.aim.hitsOnEnemy[id]
			pl.Stats.SpottedAccuracy = float64(st.aim.hitsOnEnemy[id]) / float64(shots) * 100
		}
	}
}

// finalizeSprayStats flushes any spray still open at demo end, then folds the
// per-player and per-weapon spray tallies into accuracy figures.
func (st *parseState) finalizeSprayStats() {
	var sprayIDs []uint64
	for id := range st.aim.curSpray {
		sprayIDs = append(sprayIDs, id)
	}
	for _, id := range sprayIDs {
		st.finalizeSpray(id) // flush any spray still open at demo end
	}

	for id, shots := range st.aim.sprayShots {
		if pl := st.players[id]; pl != nil && shots > 0 {
			pl.Stats.SprayAccuracy = float64(st.aim.sprayHits[id]) / float64(shots) * 100
		}
	}

	for id, byWeapon := range st.aim.sprayByWeapon {
		pl := st.players[id]
		if pl == nil {
			continue
		}
		pl.SprayWeapons = map[string]model.WeaponSpray{}
		for label, acc := range byWeapon {
			if acc.shots > 0 {
				pl.SprayWeapons[label] = model.WeaponSpray{Sprays: acc.sprays, Accuracy: float64(acc.hits) / float64(acc.shots) * 100}
			}
		}
	}
}

// finalizeSprayDeviation computes recoil deviation per player per weapon: average
// aim trajectory vs the ideal. the ideal is the pattern negated, i.e. the move
// that would cancel it out.
func (st *parseState) finalizeSprayDeviation() {
	for id, byWeapon := range st.aim.sprayDev {
		pl := st.players[id]
		if pl == nil {
			continue
		}
		pl.SprayPatterns = nil
		for _, acc := range byWeapon {
			pattern := csdata.SprayPattern(acc.weapon, acc.scoped, acc.silenced)
			dev := model.SprayDeviation{Weapon: acc.label, Scoped: acc.scoped, SilencerOn: acc.silenced, Sprays: acc.sprays}
			var devSum float64
			var devN int
			for i := range acc.sumX {
				if acc.n[i] == 0 {
					continue
				}
				px, py := acc.sumX[i]/float64(acc.n[i]), acc.sumY[i]/float64(acc.n[i])
				sx, sy := -pattern[i].X, -pattern[i].Y
				// store rounded display values; avg_deviation below uses the
				// full-precision px,py,sx,sy locals, so it is unaffected.
				dev.Bullets = append(dev.Bullets, model.SprayBullet{
					IdealX: round3(sx), IdealY: round3(sy), ActualX: round3(px), ActualY: round3(py),
				})
				dx, dy := px-sx, py-sy
				devSum += math.Sqrt(dx*dx + dy*dy)
				devN++
			}
			if devN > 0 {
				dev.AvgDeviation = devSum / float64(devN)
			}
			pl.SprayPatterns = append(pl.SprayPatterns, dev)
		}
		// deterministic order: weapon, then base before scoped, unsilenced before silenced.
		sort.Slice(pl.SprayPatterns, func(i, j int) bool {
			a, b := pl.SprayPatterns[i], pl.SprayPatterns[j]
			if a.Weapon != b.Weapon {
				return a.Weapon < b.Weapon
			}
			if a.Scoped != b.Scoped {
				return !a.Scoped
			}
			return !a.SilencerOn && b.SilencerOn
		})
	}
}

// finalizeTTD reduces each player's raw time-to-damage samples into the reported
// value: drop the trigger-discipline cutoff, then take the calibrated percentile.
func (st *parseState) finalizeTTD() {
	for id, samples := range st.aim.ttdSamples {
		if pl := st.players[id]; pl != nil && len(samples) > 0 {
			pl.Stats.TimeToDamage = ttdPercentile(samples, st.cal.TTDExcludeMs, st.cal.TTDPercentile)
			pl.TimeToDamageSamples = len(samples)
			if st.opts.AimDebugPath != "" {
				pl.TimeToDamageRaw = append([]float64(nil), samples...)
			}
		}
	}
}

// finalizeCrosshair reduces each player's crosshair-move samples into their reported
// placement via the low-winsor mean.
func (st *parseState) finalizeCrosshair() {
	for id, samples := range st.aim.crosshair {
		if pl := st.players[id]; pl != nil && len(samples) > 0 {
			pl.Stats.CrosshairPlacement = lowinsorMean(samples, st.cal.CrosshairWinsorPct)
			pl.CrosshairSamples = len(samples)
			if st.opts.AimDebugPath != "" {
				pl.CrosshairRaw = append([]float64(nil), samples...)
			}
		}
	}
}

// finalizePlayers flattens the roster into the match's SteamID-sorted player slice
// and stabilizes the lifecycle log for deterministic output.
func (st *parseState) finalizePlayers() {
	for _, pl := range st.players {
		st.match.Players = append(st.match.Players, *pl)
	}
	sort.Slice(st.match.Players, func(i, j int) bool {
		return st.match.Players[i].SteamID < st.match.Players[j].SteamID
	})

	// lifecycle log: append-ordered already, but stabilize by time then steam_id
	// then type for determinism across runs.
	sort.SliceStable(st.match.Stats.MatchLifecycle, func(i, j int) bool {
		a, b := st.match.Stats.MatchLifecycle[i], st.match.Stats.MatchLifecycle[j]
		if a.TMs != b.TMs {
			return a.TMs < b.TMs
		}
		if a.SteamID != b.SteamID {
			return a.SteamID < b.SteamID
		}
		return a.Type < b.Type
	})
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
		pair := pairs[k]
		if pair == nil {
			pair = &agg{weapons: map[string]int{}}
			pairs[k] = pair
		}
		return pair
	}

	for _, r := range rounds {
		for _, k := range r.Kills {
			if k.KillerID() == 0 || k.Victim == 0 || !enemies(k.KillerID(), k.Victim) {
				continue
			}
			pair := get([2]uint64{k.KillerID(), k.Victim})
			pair.kills++
			if k.Weapon != "" {
				pair.weapons[k.Weapon]++
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
	for k, pair := range pairs {
		duel := model.Duel{Killer: k[0], Victim: k[1], Kills: pair.kills, Damage: pair.damage}
		if len(pair.weapons) > 0 {
			duel.Weapons = pair.weapons
		}
		if s := ttd[k]; len(s) > 0 {
			duel.TimeToDamage = ttdPercentile(s, cal.TTDExcludeMs, cal.TTDPercentile)
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

// newestFireGrenade finds the thrower's most recent molotov/incendiary that hasn't
// landed yet, so we can link an inferno back to its grenade.
func newestFireGrenade(grenades map[int64]*parseGrenade, throwerID uint64) *parseGrenade {
	var best *parseGrenade
	for _, g := range grenades {
		if g.thrower != throwerID || g.detonateT != 0 {
			continue
		}
		if g.gtype != "molotov" && g.gtype != "incendiary" {
			continue
		}
		if best == nil || g.throwT > best.throwT {
			best = g
		}
	}
	return best
}

// newestDetonatedFireGrenade is the fallback for fire damage that lands after the
// inferno has already burned out (the live-inferno link is gone, but a late fire
// tick still credits the most recent fire grenade of that thrower).
func newestDetonatedFireGrenade(grenades map[int64]*parseGrenade, throwerID uint64) *parseGrenade {
	if grenades == nil {
		return nil
	}
	var best *parseGrenade
	for _, g := range grenades {
		if g.thrower != throwerID || !g.detonated {
			continue
		}
		if g.gtype != "molotov" && g.gtype != "incendiary" {
			continue
		}
		if best == nil || g.detonateT > best.detonateT {
			best = g
		}
	}
	return best
}

// applyFlashAlpha folds the per-(grenade, victim) peak whiteout alpha collected
// during onPlayerFlashed onto each flash grenade's flashed[] entries. The grenade
// map key is the projectile UniqueID, the same key flashAlpha was stored under.
func (st *parseState) applyFlashAlpha() {
	if len(st.grenades.flashAlpha) == 0 || st.grenades.pendingGrenades == nil {
		return
	}
	for uid, g := range st.grenades.pendingGrenades {
		if g.gtype != "flash" {
			continue
		}
		for i := range g.flashed {
			if a, ok := st.grenades.flashAlpha[flashAlphaKey{grenade: uid, victim: g.flashed[i].SteamID}]; ok {
				g.flashed[i].MaxAlpha = a
			}
		}
	}
}

// finalizeGrenades fans the working grenades into typed buckets by type, sorting
// each by throw time then thrower (victims/flashed by steam_id) for determinism.
func finalizeGrenades(grenades map[int64]*parseGrenade) model.Grenades {
	var out model.Grenades
	for _, g := range grenades {
		switch g.gtype {
		case "flash":
			out.Flashes = append(out.Flashes, g.toFlash())
		case "he":
			out.HEs = append(out.HEs, g.toHE())
		case "molotov", "incendiary":
			out.Molotovs = append(out.Molotovs, g.toMolotov())
		case "smoke":
			out.Smokes = append(out.Smokes, g.toBasic())
		case "decoy":
			out.Decoys = append(out.Decoys, g.toBasic())
		}
	}

	sort.Slice(out.Flashes, func(i, j int) bool {
		return grenadeLess(out.Flashes[i].ThrowMs, out.Flashes[j].ThrowMs, out.Flashes[i].Thrower, out.Flashes[j].Thrower)
	})
	sort.Slice(out.HEs, func(i, j int) bool {
		return grenadeLess(out.HEs[i].ThrowMs, out.HEs[j].ThrowMs, out.HEs[i].Thrower, out.HEs[j].Thrower)
	})
	sort.Slice(out.Molotovs, func(i, j int) bool {
		return grenadeLess(out.Molotovs[i].ThrowMs, out.Molotovs[j].ThrowMs, out.Molotovs[i].Thrower, out.Molotovs[j].Thrower)
	})
	sort.Slice(out.Smokes, func(i, j int) bool {
		return grenadeLess(out.Smokes[i].ThrowMs, out.Smokes[j].ThrowMs, out.Smokes[i].Thrower, out.Smokes[j].Thrower)
	})
	sort.Slice(out.Decoys, func(i, j int) bool {
		return grenadeLess(out.Decoys[i].ThrowMs, out.Decoys[j].ThrowMs, out.Decoys[i].Thrower, out.Decoys[j].Thrower)
	})
	return out
}

func sortVictims(v []model.GrenadeVictim) {
	sort.Slice(v, func(i, j int) bool { return v[i].SteamID < v[j].SteamID })
}

// sortPositions orders positions by x then y then z for deterministic output.
func sortPositions(p []model.Position) {
	sort.Slice(p, func(i, j int) bool {
		if p[i].X != p[j].X {
			return p[i].X < p[j].X
		}
		if p[i].Y != p[j].Y {
			return p[i].Y < p[j].Y
		}
		return p[i].Z < p[j].Z
	})
}

func sortFlashed(f []model.FlashedPlayer) {
	sort.Slice(f, func(i, j int) bool { return f[i].SteamID < f[j].SteamID })
}

// grenadeLess orders grenades by throw time, tie-breaking on thrower.
func grenadeLess(aTime, bTime int64, aThrower, bThrower uint64) bool {
	if aTime != bTime {
		return aTime < bTime
	}
	return aThrower < bThrower
}
