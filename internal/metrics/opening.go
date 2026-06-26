package metrics

import "github.com/f-gillmann/demolens/v2/model"

// tags each round's opening duel (just the first kill), flags both players on
// their RoundPlayer, and totals opening stats per side. The opening duel itself is
// not emitted as its own field: it is fully recoverable from kills[0] (opening:true)
// plus the RoundPlayer opened/won/traded flags set below.
func computeOpenings(m *model.Match) {
	team := teamMap(m)
	idx := playerIndex(m)

	for ri := range m.Rounds {
		r := &m.Rounds[ri]
		if len(r.Kills) == 0 {
			continue
		}
		first := r.Kills[0]
		if first.KillerID() == 0 || first.Victim == 0 {
			continue
		}

		side := sideMap(*r)
		traded := newRoundIndex(*r).traded(first.Victim, team)

		if rp := roundPlayer(r, first.KillerID()); rp != nil {
			rp.OpenedDuel = true
			rp.WonOpeningDuel = true
		}
		if rp := roundPlayer(r, first.Victim); rp != nil {
			rp.OpenedDuel = true
			rp.OpeningDeathTraded = traded
		}

		addOpening(idx[first.KillerID()], side[first.KillerID()], true, false)
		addOpening(idx[first.Victim], side[first.Victim], false, traded)
	}
}

// records one opening-duel appearance into the overall and the per-side tally.
func addOpening(p *model.Player, side string, won, traded bool) {
	if p == nil {
		return
	}
	applyToSides(&p.OpeningOverall, openingForSide(p, side), func(s *model.OpeningStats) {
		s.Attempts++
		if won {
			s.Kills++
		} else {
			s.Deaths++
			if traded {
				s.Traded++
			}
		}
	})
}
