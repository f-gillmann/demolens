package metrics

import "github.com/f-gillmann/demolens/model"

// kastValues returns each player's KAST percentage (0-100):
// the share of rounds in which they got a Kill, Assist, Survived, or were Traded.
func kastValues(m *model.Match) map[uint64]float64 {
	totalRounds := len(m.Rounds)
	if totalRounds == 0 {
		return map[uint64]float64{}
	}

	team := teamMap(m)
	credit := map[uint64]int{}

	for _, r := range m.Rounds {
		idx := newRoundIndex(r)
		for id := range team {
			switch {
			case idx.killers[id] || idx.assisters[id]: // Kill or Assist
				credit[id]++
			case !idx.died[id]: // Survived
				credit[id]++
			case idx.traded(id, team): // Traded
				credit[id]++
			}
		}
	}

	kast := make(map[uint64]float64, len(credit))
	for id, c := range credit {
		kast[id] = float64(c) / float64(totalRounds) * 100
	}
	return kast
}
