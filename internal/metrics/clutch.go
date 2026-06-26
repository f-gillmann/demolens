package metrics

import (
	"sort"

	"github.com/f-gillmann/demolens/v2/model"
)

// snapshot of when a side dropped to its last man.
type clutchStart struct {
	side        string
	opponents   int      // enemies still up at that moment
	startTime   int64    // round time when it kicked off
	opponentIDs []uint64 // those enemies' steamids
}

// finds 1vN per round (one side down to its last player, enemies left alive),
// stamps the clutch onto the clutcher's RoundPlayer and rolls it into the
// per-side totals. note a 1v1 fires a clutch for both sides.
func computeClutches(m *model.Match) {
	idx := playerIndex(m)

	for ri := range m.Rounds {
		r := &m.Rounds[ri]
		side := sideMap(*r)

		alive := map[string]map[uint64]bool{"CT": {}, "T": {}}
		for _, rp := range r.Players {
			if rp.Side == "CT" || rp.Side == "T" {
				alive[rp.Side][rp.SteamID] = true
			}
		}

		flagged := map[string]bool{}
		starts := map[uint64]clutchStart{} // clutcher -> situation

		for _, k := range r.Kills {
			s := side[k.Victim]
			if s != "CT" && s != "T" {
				continue
			}

			delete(alive[s], k.Victim)

			other := opposite(s)
			if !flagged[s] && len(alive[s]) == 1 && len(alive[other]) >= 1 {
				flagged[s] = true
				var last uint64
				for id := range alive[s] {
					last = id
				}
				opp := make([]uint64, 0, len(alive[other]))
				for id := range alive[other] {
					opp = append(opp, id)
				}
				starts[last] = clutchStart{side: s, opponents: len(alive[other]), startTime: k.TMs, opponentIDs: opp}
			}
		}

		for clutcher, start := range starts {
			c := buildClutch(r, clutcher, start)
			if rp := roundPlayer(r, clutcher); rp != nil {
				rp.Clutch = &c
			}
			addClutch(idx[clutcher], start.side, c)
		}
	}
}

// counts kills the clutcher got during the clutch, then works out the outcome
// from the round winner and whether they lived.
func buildClutch(r *model.Round, clutcher uint64, start clutchStart) model.Clutch {
	kills, died := 0, false
	for _, k := range r.Kills {
		if k.KillerID() == clutcher && k.TMs >= start.startTime {
			kills++
		}
		if k.Victim == clutcher {
			died = true
		}
	}

	won := r.WinnerSide == start.side
	opp := append([]uint64(nil), start.opponentIDs...)
	sort.Slice(opp, func(i, j int) bool { return opp[i] < opp[j] })
	return model.Clutch{
		Opponents:   start.opponents,
		Kills:       kills,
		Won:         won,
		Saved:       !won && !died,
		StartMs:     start.startTime,
		OpponentIDs: opp,
	}
}

// bumps the outcome into both the overall and the per-side tally.
func addClutch(p *model.Player, side string, c model.Clutch) {
	if p == nil {
		return
	}
	applyToSides(&p.ClutchOverall, clutchForSide(p, side), func(s *model.ClutchStats) {
		s.Kills += c.Kills
		switch {
		case c.Won:
			s.Won++
		case c.Saved:
			s.Saved++
		default:
			s.Lost++
		}
	})
}
