package metrics

import "github.com/f-gillmann/demolens/model"

// Accuracy is hits/shots. Head accuracy excludes AWP per competitive convention.
func accuracyStats(m *model.Match) {
	team := teamMap(m)
	idx := playerIndex(m)
	type accuracyStats struct {
		hits, nonAWPHits, nonAWPHeadHits int
		seen, seenNonAWP, seenHead       map[int64]bool // keyed on shot time so one bullet counts once
	}
	stats := map[uint64]*accuracyStats{}

	for _, r := range m.Rounds {
		for _, d := range r.Damages {
			if d.DamageType != "bullet" || d.Attacker == 0 || team[d.Attacker] == "" {
				continue
			}
			if team[d.Attacker] == team[d.Victim] { // skip teamdamage
				continue
			}

			a := stats[d.Attacker]
			if a == nil {
				a = &accuracyStats{seen: map[int64]bool{}, seenNonAWP: map[int64]bool{}, seenHead: map[int64]bool{}}
				stats[d.Attacker] = a
			}

			// wallbangs/collaterals log a hit per victim but share a shot time.
			// dedupe so it's still one shot.
			t := d.TimeMicroseconds
			if !a.seen[t] {
				a.seen[t] = true
				a.hits++
			}

			if d.Weapon != "AWP" {
				if !a.seenNonAWP[t] {
					a.seenNonAWP[t] = true
					a.nonAWPHits++
				}
				if d.HitGroup == "head" && !a.seenHead[t] {
					a.seenHead[t] = true
					a.nonAWPHeadHits++
				}
			}
		}
	}

	for id, a := range stats {
		p := idx[id]
		if p == nil {
			continue
		}

		if p.ShotsFired > 0 {
			p.Accuracy = float64(a.hits) / float64(p.ShotsFired) * 100
		}
		if a.nonAWPHits > 0 {
			p.HeadAccuracy = float64(a.nonAWPHeadHits) / float64(a.nonAWPHits) * 100
		}
	}
}
