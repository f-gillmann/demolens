package metrics

import "github.com/f-gillmann/demolens/model"

// enrichKills annotates each round's kills with duel context: opening, traded
// and traded_by, possible_traders, and sides. Sides are set at parse time; we
// only backfill them from sideMap when missing.
func enrichKills(m *model.Match) {
	team := teamMap(m)

	for ri := range m.Rounds {
		r := &m.Rounds[ri]
		if len(r.Kills) == 0 {
			continue
		}

		side := sideMap(*r)
		idx := newRoundIndex(*r)

		for ki := range r.Kills {
			k := &r.Kills[ki]

			k.Opening = ki == 0

			if k.KillerSide == "" {
				k.KillerSide = side[k.Killer]
			}
			if k.VictimSide == "" {
				k.VictimSide = side[k.Victim]
			}

			if avenger := idx.tradedBy(k.Victim, team); avenger != 0 {
				k.Traded = true
				k.TradedBy = avenger
			}
			k.PossibleTraders = possibleTraders(*r, *k, team)
		}
	}
}
