package metrics

import "github.com/f-gillmann/demolens/v2/model"

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

	m.Stats.FlashPairs = flashMatrix(m)

	for i := range m.Players {
		p := &m.Players[i]

		p.Stats.KD = killDeathRatio(*p)
		p.Stats.KAST = kast[p.SteamID]
		p.Stats.ADR = perRound(float64(p.Damage), rounds)
		p.Stats.KPR = perRound(float64(p.Kills), rounds)
		p.Stats.DPR = perRound(float64(p.Deaths), rounds)
		p.Stats.APR = perRound(float64(p.Assists), rounds)
		p.Stats.HSPercent = headshotPercent(*p)

		if tc := trades[p.SteamID]; tc != nil {
			tc.applyTo(p)
		}

		h := multiKills[p.SteamID]
		p.Stats.Rating1 = ratingHLTV1(*p, rounds, h)
		p.Stats.Rating2 = ratingHLTV2(p.Stats.KAST, p.Stats.KPR, p.Stats.DPR, p.Stats.ADR, p.Stats.APR)
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
