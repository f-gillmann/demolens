package metrics

import "github.com/f-gillmann/demolens/model"

func averageAssistsPerRound(p model.Player, rounds float64) float64 {
	if rounds <= 0 {
		return 0
	}
	return float64(p.Assists) / rounds
}
