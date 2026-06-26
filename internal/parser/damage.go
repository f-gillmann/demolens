package parser

import (
	"time"

	"github.com/f-gillmann/demolens/v2/internal/csdata"
	"github.com/f-gillmann/demolens/v2/model"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/common"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/events"
)

// damageEvent turns a PlayerHurt into our damage record. into is time since the
// round went live. withPositions rides the positions stream, so attacker/victim
// world positions only attach when that heavy stream is on.
func damageEvent(e events.PlayerHurt, into time.Duration, healthDamage int, withPositions bool) model.Damage {
	d := model.Damage{
		TMs:          into.Milliseconds(),
		HealthDamage: healthDamage,
		ArmorDamage:  e.ArmorDamageTaken,
		HitGroup:     hitGroupString(e.HitGroup),
	}

	if e.Attacker != nil {
		d.Attacker = e.Attacker.SteamID64
	}
	if e.Player != nil {
		d.Victim = e.Player.SteamID64
	}
	if e.Weapon != nil {
		d.Weapon = e.Weapon.String()
		d.DamageType = damageType(e.Weapon)
	}

	if withPositions {
		if e.Attacker != nil {
			p := toPosition(e.Attacker.Position())
			d.AttackerPosition = &p
		}
		if e.Player != nil {
			p := toPosition(e.Player.Position())
			d.VictimPosition = &p
		}
	}

	return d
}

func damageType(w *common.Equipment) string {
	switch w.Type {
	case common.EqHE:
		return "he"
	case common.EqMolotov, common.EqIncendiary:
		return "fire"
	case common.EqKnife:
		return "knife"
	case common.EqZeus:
		return "taser"
	case common.EqBomb:
		return "bomb"
	case common.EqWorld:
		return "world"
	}

	if csdata.IsGun(w) {
		return "bullet"
	}
	if w.Class() == common.EqClassGrenade {
		return "grenade_impact"
	}
	return "other"
}

func hitGroupString(hg events.HitGroup) string {
	switch hg {
	case events.HitGroupHead:
		return "head"
	case events.HitGroupChest:
		return "chest"
	case events.HitGroupStomach:
		return "stomach"
	case events.HitGroupLeftArm:
		return "left_arm"
	case events.HitGroupRightArm:
		return "right_arm"
	case events.HitGroupLeftLeg:
		return "left_leg"
	case events.HitGroupRightLeg:
		return "right_leg"
	case events.HitGroupNeck:
		return "neck"
	case events.HitGroupGear:
		return "gear"
	default:
		return "generic"
	}
}
