package metrics

import "github.com/f-gillmann/demolens/v2/model"

// turns a player's utility totals into per-throw averages.
func computeUtilityAverages(p *model.Player) {
	u := p.Utility
	a := &p.UtilityAverages

	if u.EnemiesFlashed > 0 {
		a.BlindMs = float64(u.EnemyBlindMs) / float64(u.EnemiesFlashed)
	}

	if u.HEsThrown > 0 {
		a.HEDamage = float64(u.HEDamage) / float64(u.HEsThrown)
		a.HETeamDamage = float64(u.HETeamDamage) / float64(u.HEsThrown)
	}

	if u.MolotovsThrown > 0 {
		a.MolotovDamage = float64(u.MolotovDamage) / float64(u.MolotovsThrown)
		a.MolotovTeamDamage = float64(u.MolotovTeamDamage) / float64(u.MolotovsThrown)
	}

	// avg nade value the player was still holding when they died (wasted util)
	if p.Deaths > 0 {
		a.UnusedUtilityValue = float64(u.UnusedUtilityValue) / float64(p.Deaths)
	}
}
