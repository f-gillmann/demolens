package metrics

import "github.com/f-gillmann/demolens/model"

func Compute(m *model.Match) {
	rounds := float64(m.Meta.TotalRounds)
	multiKills := multiKillHistograms(m.Rounds)
	kast := kastValues(m)
	trades := tradeStats(m)

	for i := range m.Players {
		p := &m.Players[i]

		p.KD = killDeathRatio(*p)
		p.KAST = kast[p.SteamID]
		p.ADR = averageDamagePerRound(*p, rounds)
		p.KPR = averageKillsPerRound(*p, rounds)
		p.DPR = averageDeathsPerRound(*p, rounds)
		p.APR = averageAssistsPerRound(*p, rounds)
		p.HSPercent = headshotPercent(*p)
		if tradeKill := trades[p.SteamID]; tradeKill != nil {
			p.TradeKillOpportunities = tradeKill.killOpportunity
			p.TradeKillAttempts = tradeKill.killAttempt
			p.TradeKills = tradeKill.killSuccess
			p.TradedDeathOpportunities = tradeKill.deathOpportunity
			p.TradedDeathAttempts = tradeKill.deathAttempt
			p.TradedDeaths = tradeKill.deathSuccess
		}
		p.Rating1 = ratingHLTV1(*p, rounds, multiKills[p.SteamID])
		p.Rating2 = ratingHLTV2(p.KAST, p.KPR, p.DPR, p.ADR, p.APR)
	}
}
