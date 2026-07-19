package metrics

import "github.com/f-gillmann/demolens/v2/model"

// Aggregates per-round stats into match totals; must run first (used by KD, ADR, rating).
func aggregateRounds(m *model.Match) {
	idx := playerIndex(m)
	for _, r := range m.Rounds {
		for _, rp := range r.Players {
			p := idx[rp.SteamID]
			if p == nil {
				continue
			}

			p.Kills += rp.Kills
			p.Deaths += rp.Deaths
			p.Assists += rp.Assists
			p.FlashAssists += rp.FlashAssists
			p.Headshots += rp.Headshots
			p.Damage += rp.Damage
			p.TeamDamage += rp.TeamDamage
			p.ShotsFired += rp.ShotsFired
			p.ExitKills += rp.ExitKills
			p.ExitDeaths += rp.ExitDeaths
			p.KnifeKills += rp.KnifeKills
			p.ZeusKills += rp.ZeusKills
			p.AirborneKills += rp.AirborneKills
			p.BlindKills += rp.BlindKills
			p.ScopedKills += rp.ScopedKills
			p.PickedUpKills += rp.PickedUpKills
			addUtility(&p.Utility, rp.Utility)

			if r.MvpSteamID != 0 && rp.SteamID == r.MvpSteamID {
				p.Mvps++
			}
		}
	}
}

func addUtility(dst *model.UtilityStats, src model.UtilityStats) {
	dst.FlashesThrown += src.FlashesThrown
	dst.SmokesThrown += src.SmokesThrown
	dst.HEsThrown += src.HEsThrown
	dst.MolotovsThrown += src.MolotovsThrown
	dst.DecoysThrown += src.DecoysThrown
	dst.EnemiesFlashed += src.EnemiesFlashed
	dst.TeammatesFlashed += src.TeammatesFlashed
	dst.FlashesLeadingToKill += src.FlashesLeadingToKill
	dst.EnemyBlindMs += src.EnemyBlindMs
	dst.HEDamage += src.HEDamage
	dst.HETeamDamage += src.HETeamDamage
	dst.MolotovDamage += src.MolotovDamage
	dst.MolotovTeamDamage += src.MolotovTeamDamage
	dst.UsedUtilityValue += src.UsedUtilityValue
	dst.UnusedUtilityValue += src.UnusedUtilityValue
}
