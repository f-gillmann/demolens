package metrics

import "github.com/f-gillmann/demolens/model"

// Compute calculates all per-player and match-level metrics.
func Compute(m *model.Match) {
	aggregateRounds(m)

	rounds := float64(len(m.Rounds))
	multiKills := multiKillHistograms(m.Rounds)
	kast := kastValues(m)
	trades := tradeStats(m)
	weapons := weaponStats(m)
	killTypes := killTypeCounts(m)

	computeOpenings(m)
	computeClutches(m)
	accuracyStats(m)

	m.FlashMatrix = flashMatrix(m)

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

		h := multiKills[p.SteamID]
		p.MultiKills = model.MultiKills{K1: h[1], K2: h[2], K3: h[3], K4: h[4], K5: h[5]}

		if kt, ok := killTypes[p.SteamID]; ok {
			p.NoScopeKills = kt.noScope
			p.WallbangKills = kt.wallbang
			p.CollateralKills = kt.collateral
		}

		if ws := weapons[p.SteamID]; ws != nil {
			p.WeaponStats = ws
		} else {
			p.WeaponStats = map[string]model.WeaponStat{}
		}

		computeUtilityAverages(p)
	}
}
