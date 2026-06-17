package metrics

import "github.com/f-gillmann/demolens/model"

// published HLTV Rating 1.0 / 2.0 coefficients.
const (
	// Rating 1.0: league averages and weights.
	avgKillsPerRound    = 0.679
	avgSurvivalPerRound = 0.317
	avgMultiKillValue   = 1.277
	r1SurvivalWeight    = 0.7
	r1Normalizer        = 2.7

	// Rating 2.0: regression weights and intercept.
	r2Kast      = 0.0073
	r2Kpr       = 0.3591
	r2Dpr       = -0.5329
	r2Impact    = 0.2372
	r2Adr       = 0.0032
	r2Intercept = 0.1587

	// Rating 2.0 impact stand-in weights.
	impactKpr       = 2.13
	impactApr       = 0.42
	impactIntercept = -0.41
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

	return (killRating + r1SurvivalWeight*survivalRating + multiKillRating) / r1Normalizer
}

// ratingHLTV2 approximates Rating 2.0. coefficients are the published ones fit
// over rounds, KAST, KPR, DPR, ADR. close but not exact, HLTV never released the
// real model.
func ratingHLTV2(kast, kpr, dpr, adr, apr float64) float64 {
	return r2Kast*kast + r2Kpr*kpr + r2Dpr*dpr + r2Impact*impactHLTV(kpr, apr) + r2Adr*adr + r2Intercept
}

// rough impact stand-in from KPR and APR.
func impactHLTV(kpr, apr float64) float64 {
	return impactKpr*kpr + impactApr*apr + impactIntercept
}
