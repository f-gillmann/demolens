package metrics

import "github.com/f-gillmann/demolens/model"

func headshotPercent(p model.Player) float64 {
	if p.Kills == 0 {
		return 0
	}
	return float64(p.Headshots) / float64(p.Kills) * 100
}
