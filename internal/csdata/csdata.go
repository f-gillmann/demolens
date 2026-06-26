// Package csdata holds static CS2 equipment data and weapon classification helpers.
package csdata

import (
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/common"
)

// UtilityPrice is the CS2 buy price per grenade, in dollars.
var UtilityPrice = map[common.EquipmentType]int{
	common.EqFlash:      200,
	common.EqSmoke:      300,
	common.EqHE:         300,
	common.EqMolotov:    400,
	common.EqIncendiary: 600,
	common.EqDecoy:      50,
}

// WeaponPrice is the CS2 buy-menu price per gun, in dollars. The lib never
// networks a weapon's price, so we keep our own table. Free spawn pistols are 0.
var WeaponPrice = map[common.EquipmentType]int{
	common.EqP2000:        0,
	common.EqGlock:        0,
	common.EqUSP:          0,
	common.EqP250:         300,
	common.EqFiveSeven:    500,
	common.EqTec9:         500,
	common.EqCZ:           500,
	common.EqDualBerettas: 300,
	common.EqDeagle:       700,
	common.EqRevolver:     600,
	common.EqMac10:        1050,
	common.EqMP9:          1250,
	common.EqMP7:          1500,
	common.EqMP5:          1500,
	common.EqUMP:          1200,
	common.EqP90:          2350,
	common.EqBizon:        1400,
	common.EqNova:         1050,
	common.EqXM1014:       2000,
	common.EqSawedOff:     1100,
	common.EqMag7:         1300,
	common.EqM249:         5200,
	common.EqNegev:        1700,
	common.EqGalil:        1800,
	common.EqFamas:        2050,
	common.EqAK47:         2700,
	common.EqM4A4:         3100,
	common.EqM4A1:         2900,
	common.EqSSG08:        1700,
	common.EqSG553:        3000,
	common.EqAUG:          3300,
	common.EqAWP:          4750,
	common.EqScar20:       5000,
	common.EqG3SG1:        5000,
}

// EquipmentClassName returns the lowercase class token for a gun class, or "" for
// any non-gun class.
func EquipmentClassName(c common.EquipmentClass) string {
	switch c {
	case common.EqClassPistols:
		return "pistol"
	case common.EqClassSMG:
		return "smg"
	case common.EqClassHeavy:
		return "heavy"
	case common.EqClassRifle:
		return "rifle"
	default:
		return ""
	}
}

// WeaponMaxSpeed is max move speed in game units/sec for each weapon held. CS2 never networks
// velocity or speed caps, so we keep our own table and turn a killer's speed
// into a per-weapon ratio for counter-strafe.
var WeaponMaxSpeed = map[common.EquipmentType]float64{
	common.EqP2000:        240,
	common.EqGlock:        240,
	common.EqP250:         240,
	common.EqDeagle:       230,
	common.EqFiveSeven:    240,
	common.EqDualBerettas: 240,
	common.EqTec9:         240,
	common.EqCZ:           240,
	common.EqUSP:          240,
	common.EqRevolver:     220,
	common.EqMP7:          220,
	common.EqMP9:          240,
	common.EqBizon:        240,
	common.EqMac10:        240,
	common.EqUMP:          230,
	common.EqP90:          230,
	common.EqMP5:          235,
	common.EqSawedOff:     210,
	common.EqNova:         220,
	common.EqMag7:         225,
	common.EqXM1014:       215,
	common.EqM249:         195,
	common.EqNegev:        150,
	common.EqGalil:        215,
	common.EqFamas:        220,
	common.EqAK47:         215,
	common.EqM4A4:         225,
	common.EqM4A1:         225,
	common.EqSSG08:        230,
	common.EqSG553:        210,
	common.EqAUG:          220,
	common.EqAWP:          200,
	common.EqScar20:       215,
	common.EqG3SG1:        215,
}

// fallback run speed for anything not in weaponMaxSpeed
const defaultMaxSpeed = 215.0

// SpeedRatio normalises horizontal speed against the weapon's cap so one
// threshold works no matter which gun is held.
func SpeedRatio(speed float64, w *common.Equipment) float64 {
	maxSpeed := defaultMaxSpeed
	if w != nil {
		if ms, ok := WeaponMaxSpeed[w.Type]; ok {
			maxSpeed = ms
		}
	}
	return speed / maxSpeed
}

// EngineSpeed reads the player's 2D speed off m_flFrictionStashedSpeed. Velocity
// isn't networked in CS2 but this prop is and it's exact, beating a noisy position
// delta. -1 if absent.
func EngineSpeed(p *common.Player) float64 {
	e := p.PlayerPawnEntity()
	if e == nil {
		return -1
	}
	if v, ok := e.PropertyValue("m_pMovementServices.m_flFrictionStashedSpeed"); ok {
		return float64(v.Float())
	}
	return -1
}

// SprayWeapons holds guns we measure spray accuracy on: full-auto rifles, SMGs, LMGs. Snipers,
// pistols and shotguns have no spray pattern so they're out.
var SprayWeapons = map[common.EquipmentType]bool{
	common.EqAK47:  true,
	common.EqM4A4:  true,
	common.EqM4A1:  true,
	common.EqGalil: true,
	common.EqFamas: true,
	common.EqAUG:   true,
	common.EqSG553: true,
	common.EqMP9:   true,
	common.EqMac10: true,
	common.EqBizon: true,
	common.EqUMP:   true,
	common.EqP90:   true,
	common.EqMP7:   true,
	common.EqMP5:   true,
	common.EqM249:  true,
	common.EqNegev: true,
}

// IsSprayWeapon marks full-auto gun, i.e. has a spray pattern
func IsSprayWeapon(w *common.Equipment) bool {
	return w != nil && SprayWeapons[w.Type]
}

// RifleTypes marks assault rifles, counter-strafe is rifle-only
var RifleTypes = map[common.EquipmentType]bool{
	common.EqAK47:  true,
	common.EqM4A4:  true,
	common.EqM4A1:  true,
	common.EqGalil: true,
	common.EqFamas: true,
	common.EqAUG:   true,
	common.EqSG553: true,
}

func IsRifle(w *common.Equipment) bool {
	return w != nil && RifleTypes[w.Type]
}

// IsGun is true for actual gun shots, so not grenades, knife or zeus.
func IsGun(w *common.Equipment) bool {
	if w == nil {
		return false
	}
	switch w.Class() {
	case common.EqClassPistols, common.EqClassSMG, common.EqClassHeavy, common.EqClassRifle:
		return true
	default:
		return false
	}
}

// IsGroundTrackable reports whether a world weapon entity is one we show as a ground
// item once it is dropped: any gun, any grenade, the zeus, or a knife. The c4 is left
// out (it has its own ground path); non-weapon equipment (kevlar, nvg, health-shot)
// never reaches GameState().Weapons(), so only the zeus and knife are pulled out of
// the broad equipment class.
func IsGroundTrackable(w *common.Equipment) bool {
	if w == nil {
		return false
	}
	if IsGun(w) || w.Class() == common.EqClassGrenade {
		return true
	}
	return w.Type == common.EqZeus || w.Type == common.EqKnife
}
