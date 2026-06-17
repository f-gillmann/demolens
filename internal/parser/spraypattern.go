package parser

import (
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/common"
)

// sprayRun is one in-progress run of consecutive rifle shots, i.e. a spray.
// Spray accuracy is the hit ratio of spray bullets, so we stash each shot's time
// and tally later how many landed on an enemy.
type sprayRun struct {
	weapon    common.EquipmentType
	label     string       // display weapon name
	scoped    bool         // shooter was scoped in (AUG/SG553 have a scoped pattern)
	silenced  bool         // silencer attached (M4A1-S has a no-silencer pattern)
	shotTimes []int64      // every consecutive shot (us). this gates the 3+ spray
	visTimes  []int64      // shots at a visible enemy, the ones we actually count
	views     [][2]float64 // yaw/pitch at each shot, for recoil-pattern deviation
}

// running hit-accuracy tally for one weapon
type sprayAgg struct {
	hits, shots, sprays int
}

// sprayDevAgg sums a player's aim offset per shot index over their sprays with one
// weapon, feeding the recoil-pattern deviation.
type sprayDevAgg struct {
	weapon           common.EquipmentType
	label            string // display weapon name, e.g. "AUG"
	scoped, silenced bool   // weapon variant, selects the recoil reference
	sprays           int
	sumX, sumY       []float64 // summed aim offset (deg) from the first shot, per shot index
	n                []int     // sprays hitting each shot index
}
