package metrics

import "github.com/f-gillmann/demolens/model"

func killDeathRatio(p model.Player) float64 {
	if p.Deaths == 0 {
		return float64(p.Kills)
	}
	return float64(p.Kills) / float64(p.Deaths)
}

func perRound(value, rounds float64) float64 {
	if rounds <= 0 {
		return 0
	}
	return value / rounds
}

func headshotPercent(p model.Player) float64 {
	if p.Kills == 0 {
		return 0
	}
	return float64(p.Headshots) / float64(p.Kills) * 100
}
