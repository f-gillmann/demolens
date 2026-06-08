package metrics

import "github.com/f-gillmann/demolens/model"

// HLTV Rating 1.0 average constants.
const (
	avgKillsPerRound    = 0.679
	avgSurvivalPerRound = 0.317
	avgMultiKillValue   = 1.277
)

// ratingHLTV1 uses exact HLTV Rating 1.0.
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

// ratingHLTV2 is an approximation of HLTV Rating 2.0.
// source: https://dave.xn--tckwe/posts/reverse-engineering-hltv-rating/
func ratingHLTV2(kast, kpr, dpr, adr, apr float64) float64 {
	return 0.0073*kast + 0.3591*kpr - 0.5329*dpr + 0.2372*impactHLTV(kpr, apr) + 0.0032*adr + 0.1587
}

// impactHLTV is a dirty approximation of HTLV impact.
// source: https://dave.xn--tckwe/posts/reverse-engineering-hltv-rating/
func impactHLTV(kpr, apr float64) float64 {
	return 2.13*kpr + 0.42*apr - 0.41
}

// multiKillHistograms calculates how many rounds had exactly n kills per player
func multiKillHistograms(rounds []model.Round) map[uint64][6]int {
	history := map[uint64][6]int{}
	for _, round := range rounds {
		perRound := map[uint64]int{}
		for _, kill := range round.Kills {
			if kill.Killer != 0 {
				perRound[kill.Killer]++
			}
		}
		for id, amount := range perRound {
			if amount > 5 {
				amount = 5
			}
			h := history[id]
			h[amount]++
			history[id] = h
		}
	}
	return history
}
