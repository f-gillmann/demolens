package metrics

import "github.com/f-gillmann/demolens/model"

// league averages baked into Rating 1.0.
const (
	avgKillsPerRound    = 0.679
	avgSurvivalPerRound = 0.317
	avgMultiKillValue   = 1.277
)

// ratingHLTV1 is the exact 1.0 formula.
func ratingHLTV1(p model.Player, rounds float64, mk [6]int) float64 {
	if rounds <= 0 {
		return 0
	}

	killRating := (float64(p.Kills) / rounds) / avgKillsPerRound
	survivalRating := ((rounds - float64(p.Deaths)) / rounds) / avgSurvivalPerRound

	multiKills := 1*mk[1] + 4*mk[2] + 9*mk[3] + 16*mk[4] + 25*mk[5]
	multiKillRating := (float64(multiKills) / rounds) / avgMultiKillValue

	return (killRating + 0.7*survivalRating + multiKillRating) / 2.7
}

// ratingHLTV2 approximates Rating 2.0. coefficients are the published ones fit
// over rounds, KAST, KPR, DPR, ADR. close but not exact, HLTV never released the
// real model.
func ratingHLTV2(kast, kpr, dpr, adr, apr float64) float64 {
	return 0.0073*kast + 0.3591*kpr - 0.5329*dpr + 0.2372*impactHLTV(kpr, apr) + 0.0032*adr + 0.1587
}

// rough impact stand-in from KPR and APR.
func impactHLTV(kpr, apr float64) float64 {
	return 2.13*kpr + 0.42*apr - 0.41
}
