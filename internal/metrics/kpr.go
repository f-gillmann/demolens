package metrics

import "github.com/f-gillmann/demolens/model"

func averageKillsPerRound(p model.Player, rounds float64) float64 {
	if rounds <= 0 {
		return 0
	}
	return float64(p.Kills) / rounds
}
