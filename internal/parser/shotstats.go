package parser

import (
	"sort"

	"github.com/f-gillmann/demolens/v2/model"
)

// finalizeShotStats flattens the per-round shot tally into the sorted shot_stats
// slice. hits are computed from this round's own damages, deduped by shot time per
// (attacker, weapon) so a wallbang/collateral counts once, matching accuracy.go.
func finalizeShotStats(tally map[uint64]map[string]*shotStatAcc, damages []model.Damage) []model.ShotStat {
	if len(tally) == 0 {
		return nil
	}

	// hits per (attacker, weapon), deduped by shot time. bullet damage only, since
	// shot_stats is about gun fire.
	type hk struct {
		id     uint64
		weapon string
	}
	seen := map[hk]map[int64]bool{}
	hits := map[hk]int{}
	for _, d := range damages {
		if d.DamageType != "bullet" || d.Attacker == 0 {
			continue
		}
		k := hk{d.Attacker, d.Weapon}
		if seen[k] == nil {
			seen[k] = map[int64]bool{}
		}
		if seen[k][d.TMs] {
			continue
		}
		seen[k][d.TMs] = true
		hits[k]++
	}

	var out []model.ShotStat
	for id, byWeapon := range tally {
		for weapon, acc := range byWeapon {
			out = append(out, model.ShotStat{
				SteamID:      id,
				Weapon:       weapon,
				Shots:        acc.shots,
				SpottedShots: acc.spotted,
				Hits:         hits[hk{id, weapon}],
			})
		}
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].SteamID != out[j].SteamID {
			return out[i].SteamID < out[j].SteamID
		}
		return out[i].Weapon < out[j].Weapon
	})
	return out
}
