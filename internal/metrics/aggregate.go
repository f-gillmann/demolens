package metrics

import "github.com/f-gillmann/demolens/model"

// rolls per-round numbers up into match totals. has to run first, KD/ADR/rating
// all read off these.
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
			addUtility(&p.Utility, rp.Utility)
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
	dst.EnemyBlindMicroseconds += src.EnemyBlindMicroseconds
	dst.HEDamage += src.HEDamage
	dst.HETeamDamage += src.HETeamDamage
	dst.MolotovDamage += src.MolotovDamage
	dst.MolotovTeamDamage += src.MolotovTeamDamage
	dst.UsedUtilityValue += src.UsedUtilityValue
	dst.UnusedUtilityValue += src.UnusedUtilityValue
}
