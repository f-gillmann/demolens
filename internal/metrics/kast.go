package metrics

import "github.com/f-gillmann/demolens/v2/model"

// kastValues returns KAST per player as 0-100. that's the fraction of rounds
// where they did at least one of: kill, assist, survive, or got traded.
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
			case idx.killers[id] || idx.assisters[id]: // K or A
				credit[id]++
			case !idx.died[id]: // S
				credit[id]++
			case idx.traded(id, team): // T
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
