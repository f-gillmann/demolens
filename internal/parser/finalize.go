package parser

import (
	"math"
	"sort"
	"time"

	"github.com/f-gillmann/demolens/internal/demosource"
	"github.com/f-gillmann/demolens/model"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/common"
)

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

// finalizeMatch runs the post-parse aggregation: match meta, duel matrix, and the
// per-player counter-strafe/spotted/spray/TTD/crosshair stats.
func (st *parseState) finalizeMatch() {
	st.match.Meta.TickRate = st.parsed.TickRate()
	st.match.Meta.DurationMicroseconds = st.parsed.CurrentTime().Microseconds()
	st.match.Meta.TotalRounds = len(st.match.Rounds)
	st.match.Meta.ServerPlatform = demosource.GuessSource(st.match.Meta.ServerName)
	st.match.Meta.DemoType = demosource.DemoType(st.match.Meta.IsHltv, st.match.Meta.ClientName)

	// score = each team's round wins. counted from our own A/B winner so the side
	// swap doesn't matter (the engine's per-side score follows the slot, not the team).
	for _, r := range st.match.Rounds {
		switch r.WinnerTeam {
		case "A":
			st.match.Meta.Score.TeamA++
		case "B":
			st.match.Meta.Score.TeamB++
		}
	}

	// clan names, pulled off the players on each side
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

	// duel matrix: for each enemy killer/victim pair, kills, damage, per-weapon
	// kills and average TTD. lives here rather than in metrics because it needs the
	// TTD samples; the kill/damage half is duplicated in the rounds.
	st.match.DuelMatrix = buildDuelMatrix(st.match.Rounds, st.players, st.ttdByVictim, st.cal)

	for id, acc := range st.counterStrafes {
		if pl := st.players[id]; pl != nil && acc.shots > 0 {
			pl.CounterStrafe = &model.CounterStrafe{
				Shots:       acc.shots,
				Stopped:     acc.stopped,
				StoppedRate: float64(acc.stopped) / float64(acc.shots) * 100,
				AvgSpeed:    acc.speedSum / float64(acc.shots),
			}
		}
	}

	for id, shots := range st.shotsAtEnemy {
		if pl := st.players[id]; pl != nil && shots > 0 {
			pl.SpottedShots = shots
			pl.SpottedHits = st.hitsOnEnemy[id]
			pl.SpottedAccuracy = float64(st.hitsOnEnemy[id]) / float64(shots) * 100
		}
	}

	var sprayIDs []uint64
	for id := range st.curSpray {
		sprayIDs = append(sprayIDs, id)
	}
	for _, id := range sprayIDs {
		st.finalizeSpray(id) // flush any spray still open at demo end
	}

	for id, shots := range st.sprayShots {
		if pl := st.players[id]; pl != nil && shots > 0 {
			pl.SprayAccuracy = float64(st.sprayHits[id]) / float64(shots) * 100
		}
	}

	for id, byw := range st.sprayByWeapon {
		pl := st.players[id]
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
	for id, byw := range st.sprayDev {
		pl := st.players[id]
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

	for id, samples := range st.ttdSamples {
		if pl := st.players[id]; pl != nil && len(samples) > 0 {
			pl.TimeToDamage = adaptiveTTD(samples, st.cal.TTDOutlierFactor, st.cal.TTDClampMs)
			pl.TimeToDamageSamples = len(samples)
		}
	}

	for id, samples := range st.crosshair {
		if pl := st.players[id]; pl != nil && len(samples) > 0 {
			sort.Float64s(samples)
			pl.CrosshairPlacement = median(samples)
			pl.CrosshairSamples = len(samples)
		}
	}

	for _, pl := range st.players {
		st.match.Players = append(st.match.Players, *pl)
	}
	sort.Slice(st.match.Players, func(i, j int) bool {
		return st.match.Players[i].SteamID < st.match.Players[j].SteamID
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
