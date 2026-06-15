package parser

import (
	"github.com/f-gillmann/demolens/internal/csdata"
	"github.com/f-gillmann/demolens/model"
	"github.com/golang/geo/r3"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/common"
)

// wrapDeg folds a degree delta into [-180, 180] so yaw diffs across the +/-180
// seam don't blow up.
func wrapDeg(d float64) float64 {
	for d > 180 {
		d -= 360
	}
	for d < -180 {
		d += 360
	}
	return d
}

// grenadeTypeString is the model-facing name for a grenade type.
func grenadeTypeString(t common.EquipmentType) string {
	switch t {
	case common.EqFlash:
		return "flash"
	case common.EqSmoke:
		return "smoke"
	case common.EqHE:
		return "he"
	case common.EqMolotov:
		return "molotov"
	case common.EqIncendiary:
		return "incendiary"
	case common.EqDecoy:
		return "decoy"
	default:
		return ""
	}
}

// demoinfocs world vector to our Position
func toPosition(v r3.Vector) model.Position {
	return model.Position{X: v.X, Y: v.Y, Z: v.Z}
}

func grenadePosition(g *common.GrenadeProjectile) model.Position {
	if g == nil {
		return model.Position{}
	}
	return toPosition(g.Position())
}

// grenadeInventoryValue is the dollar value of nades a player is holding.
// Non-grenades fall through utilityPrice as 0.
func grenadeInventoryValue(p *common.Player) int {
	value := 0
	for _, w := range p.Weapons() {
		value += csdata.UtilityPrice[w.Type]
	}
	return value
}
