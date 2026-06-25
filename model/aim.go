package model

// Duel is one killer's record against one victim over the whole match.
type Duel struct {
	Killer       uint64         `json:"killer,string"`
	Victim       uint64         `json:"victim,string"`
	Kills        int            `json:"kills"`
	Damage       int            `json:"damage"`                      // health damage killer dealt to victim
	TimeToDamage float64        `json:"time_to_damage_ms,omitempty"` // avg ms seeing to first damage, needs a map mesh
	Weapons      map[string]int `json:"weapons,omitempty"`
}

// WeaponSpray holds a weapon's match spray accuracy: count of 3+ shot sprays
// fired and the share of those bullets that landed.
type WeaponSpray struct {
	Sprays   int     `json:"sprays"`
	Accuracy float64 `json:"accuracy_pct"`
}

// SprayDeviation holds how a player's sprays with one weapon matched its recoil
// pattern, averaged over every 3+ shot spray they fired.
type SprayDeviation struct {
	Weapon       string        `json:"weapon"`      // display weapon name, e.g. "AUG"
	Scoped       bool          `json:"scoped"`      // fired while scoped in (AUG/SG553 have a scoped pattern)
	SilencerOn   bool          `json:"silencer_on"` // silencer attached (M4A1-S has a no-silencer pattern)
	Sprays       int           `json:"sprays"`
	AvgDeviation float64       `json:"avg_deviation"` // mean deg off from ideal recoil compensation
	Bullets      []SprayBullet `json:"bullets"`       // one per shot index
}

// SprayBullet compares, at a given shot index, the ideal aim offset that cancels
// the recoil pattern (ideal) against what the player actually did (actual). Both
// in degrees from the first shot. Good sprayers track ideal closely.
type SprayBullet struct {
	IdealX  float64 `json:"ideal_x"`
	IdealY  float64 `json:"ideal_y"`
	ActualX float64 `json:"actual_x"`
	ActualY float64 `json:"actual_y"`
}
