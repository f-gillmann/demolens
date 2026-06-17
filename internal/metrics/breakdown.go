package metrics

import (
	"sort"

	"github.com/f-gillmann/demolens/model"
)

// per player, how many rounds ended with exactly n kills. n maxes out at 5 (ace).
func multiKillHistograms(rounds []model.Round) map[uint64][6]int {
	history := map[uint64][6]int{}
	for _, round := range rounds {
		perRound := map[uint64]int{}
		for _, kill := range round.Kills {
			if kill.Killer != 0 {
				perRound[kill.Killer]++
			}
		}

		for id, amount := range perRound {
			if amount > 5 {
				amount = 5
			}
			h := history[id]
			h[amount]++
			history[id] = h
		}
	}
	return history
}

type killTypes struct {
	noScope, wallbang, collateral int
}

// applyTo copies the kill-type tallies onto the player.
func (kt killTypes) applyTo(p *model.Player) {
	p.NoScopeKills = kt.noScope
	p.WallbangKills = kt.wallbang
	p.CollateralKills = kt.collateral
}

// toMultiKills turns a 0..5 kills-per-round histogram into the model breakdown.
func toMultiKills(h [6]int) model.MultiKills {
	return model.MultiKills{K1: h[1], K2: h[2], K3: h[3], K4: h[4], K5: h[5]}
}

// noscope/wallbang/collateral counts per player. collateral = 2+ victims dropped
// by the same shot (same timestamp).
func killTypeCounts(m *model.Match) map[uint64]killTypes {
	counts := map[uint64]killTypes{}
	for _, r := range m.Rounds {
		killsByTime := map[uint64]map[int64]int{} // killer, then kill time, then count
		for _, k := range r.Kills {
			if k.Killer == 0 {
				continue
			}

			c := counts[k.Killer]
			if k.NoScope {
				c.noScope++
			}
			if k.Wallbang {
				c.wallbang++
			}
			counts[k.Killer] = c

			if killsByTime[k.Killer] == nil {
				killsByTime[k.Killer] = map[int64]int{}
			}
			killsByTime[k.Killer][k.TimeMicroseconds]++
		}

		for killer, times := range killsByTime {
			collateral := 0
			for _, n := range times {
				if n >= 2 {
					collateral += n
				}
			}

			if collateral > 0 {
				c := counts[killer]
				c.collateral += collateral
				counts[killer] = c
			}
		}
	}
	return counts
}

// per-weapon kills/headshots/damage for each player.
func weaponStats(m *model.Match) map[uint64]map[string]model.WeaponStat {
	stats := map[uint64]map[string]model.WeaponStat{}
	edit := func(id uint64, weapon string, fn func(*model.WeaponStat)) {
		if id == 0 || weapon == "" {
			return
		}

		if stats[id] == nil {
			stats[id] = map[string]model.WeaponStat{}
		}

		ws := stats[id][weapon]
		fn(&ws)
		stats[id][weapon] = ws
	}

	for _, r := range m.Rounds {
		for _, k := range r.Kills {
			edit(k.Killer, k.Weapon, func(ws *model.WeaponStat) {
				ws.Kills++
				if k.Headshot {
					ws.Headshots++
				}
			})
		}

		for _, d := range r.Damages {
			edit(d.Attacker, d.Weapon, func(ws *model.WeaponStat) {
				ws.Damage += d.HealthDamage
			})
		}
	}
	return stats
}

// who flashed whom: count and total blind time per flasher/victim pair.
func flashMatrix(m *model.Match) []model.FlashPair {
	type pair struct{ flasher, flashed uint64 }
	type agg struct {
		count int
		blind int64
	}
	counts := map[pair]agg{}
	for _, r := range m.Rounds {
		for _, g := range r.Grenades.Flashes {
			if g.Thrower == 0 {
				continue
			}
			for _, f := range g.Flashed {
				if f.SteamID == 0 {
					continue
				}

				a := counts[pair{g.Thrower, f.SteamID}]
				a.count++
				a.blind += f.BlindMicroseconds
				counts[pair{g.Thrower, f.SteamID}] = a
			}
		}
	}

	pairs := make([]model.FlashPair, 0, len(counts))
	for p, a := range counts {
		pairs = append(pairs, model.FlashPair{Flasher: p.flasher, Flashed: p.flashed, Count: a.count, BlindMicroseconds: a.blind})
	}

	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].Flasher != pairs[j].Flasher {
			return pairs[i].Flasher < pairs[j].Flasher
		}
		return pairs[i].Flashed < pairs[j].Flashed
	})
	return pairs
}
