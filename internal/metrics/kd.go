package metrics

import "github.com/f-gillmann/demolens/model"

func killDeathRatio(p model.Player) float64 {
	if p.Deaths == 0 {
		return float64(p.Kills)
	}
	return float64(p.Kills) / float64(p.Deaths)
}
