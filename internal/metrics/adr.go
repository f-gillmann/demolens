package metrics

import "github.com/f-gillmann/demolens/model"

func averageDamagePerRound(p model.Player, rounds float64) float64 {
	if rounds <= 0 {
		return 0
	}
	return float64(p.Damage) / rounds
}
