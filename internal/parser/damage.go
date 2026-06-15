package parser

import (
	"time"

	"github.com/f-gillmann/demolens/model"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/common"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/events"
)

// damageEvent turns a PlayerHurt into our damage record. into is time since the
// round went live.
func damageEvent(e events.PlayerHurt, into time.Duration) model.Damage {
	d := model.Damage{
		TimeMicroseconds: into.Microseconds(),
		HealthDamage:     e.HealthDamageTaken,
		ArmorDamage:      e.ArmorDamageTaken,
		HitGroup:         hitGroupString(e.HitGroup),
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

	switch w.Class() {
	case common.EqClassPistols, common.EqClassSMG, common.EqClassHeavy, common.EqClassRifle:
		return "bullet"
	case common.EqClassGrenade:
		return "grenade_impact"
	default:
		return "other"
	}
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
