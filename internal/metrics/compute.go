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
	enrichKills(m)
	computeClutches(m)
	accuracyStats(m)

	m.FlashMatrixTotal = flashMatrix(m)

	for i := range m.Players {
		p := &m.Players[i]

		p.KD = killDeathRatio(*p)
		p.KAST = kast[p.SteamID]
		p.ADR = perRound(float64(p.Damage), rounds)
		p.KPR = perRound(float64(p.Kills), rounds)
		p.DPR = perRound(float64(p.Deaths), rounds)
		p.APR = perRound(float64(p.Assists), rounds)
		p.HSPercent = headshotPercent(*p)

		if tc := trades[p.SteamID]; tc != nil {
			tc.applyTo(p)
		}

		h := multiKills[p.SteamID]
		p.Rating1 = ratingHLTV1(*p, rounds, h)
		p.Rating2 = ratingHLTV2(p.KAST, p.KPR, p.DPR, p.ADR, p.APR)
		p.MultiKills = toMultiKills(h)

		if kt, ok := killTypes[p.SteamID]; ok {
			kt.applyTo(p)
		}

		if ws := weapons[p.SteamID]; ws != nil {
			p.WeaponStats = ws
		} else {
			p.WeaponStats = map[string]model.WeaponStat{}
		}

		computeUtilityAverages(p)
	}
}
