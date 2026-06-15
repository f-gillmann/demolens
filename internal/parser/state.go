package parser

import (
	"time"

	"github.com/f-gillmann/demolens/internal/geom"
	"github.com/f-gillmann/demolens/model"
	"github.com/golang/geo/r3"
	dem "github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/common"
)

// pendingFlash is who fully blinded a player, so we can credit the flasher if the
// victim dies blind.
type pendingFlash struct {
	flasher uint64
	team    common.Team
	expire  time.Duration
}

// running counter-strafe tally for one player over the match
type counterStrafeAcc struct {
	shots, stopped int
	speedSum       float64
}

// pv is an alive player's eye position and view direction, pulled once per frame
// so the sighting pair loop reads it instead of re-querying the entity.
type pv struct {
	id        uint64
	team      common.Team
	eye, view r3.Vector
}

// cand is a frustum-passing pair whose expensive los/smoke check is deferred to
// the parallel pass.
type cand struct {
	en         *engagement
	sEye, eEye r3.Vector
	vis        bool
}

// parseState holds the accumulator maps and per-parse state for one Parse run.
// Handlers and finalize steps are methods on it that mutate this shared state.
type parseState struct {
	parsed dem.Parser
	opts   Options
	cal    Calibration

	match   *model.Match
	players map[uint64]*model.Player

	roundStart      time.Duration
	roundEndTime    time.Duration // when the round ended, for the exit-window duration
	pending         *model.Round
	pendingPlayers  map[uint64]*model.RoundPlayer
	pendingGrenades map[int64]*model.Grenade // keyed by projectile UniqueID
	entityToUnique  map[int]int64            // grenade entity id to UniqueID, needed for landing events
	liveInfernos    map[int64]*liveInferno   // polled until they burn out
	roundLive       bool                     // true between freezetime end and round end
	lastFrameSample time.Duration
	lastPos         map[uint64]model.Position
	lastPosTime     map[uint64]time.Duration
	// CS2 doesn't network velocity, so we derive speed from the frame-to-frame delta.
	playerSpeed    map[uint64]float64
	counterStrafes map[uint64]*counterStrafeAcc
	shotsAtEnemy   map[uint64]int
	hitsOnEnemy    map[uint64]int
	lastShotTime   map[uint64]time.Duration
	curSpray       map[uint64]*sprayRun
	sprayHits      map[uint64]int
	sprayShots     map[uint64]int
	sprayByWeapon  map[uint64]map[string]*sprayAgg
	sprayDev       map[uint64]map[string]*sprayDevAgg // per-weapon recoil deviation
	hitTimes       map[uint64][]int64                 // per shooter, demo times (us) a bullet hit an enemy, chronological

	// TTD los needs the map collision mesh. load it lazily once we know the map.
	mesh         *geom.Mesh
	meshTried    bool
	activeSmokes map[int]r3.Vector         // entityID to cloud pos while blooming
	engagements  map[[2]uint64]*engagement // (shooter,enemy) sighting state, wiped each round
	flashLead    map[uint64]pendingFlash   // wiped each round
	ttdSamples   map[uint64][]float64      // ms samples per shooter, averaged at the end
	ttdByVictim  map[[2]uint64][]float64   // same samples split by victim for the duel matrix
	crosshair    map[uint64][]float64      // crosshair-move samples in deg

	// A starts CT, B starts T. We flip this on every side switch so a player's
	// A/B identity stays put when the sides swap.
	sideToTeam map[common.Team]string

	// reused across frames by the sighting handler so the los raycasts can run in
	// parallel without a per-frame alloc. live: alive players' eye+view. cands: the
	// frustum-passing pairs whose expensive los/smoke check is deferred to pass 2.
	live  []pv
	cands []cand
}

// newParseState builds the empty per-run state with every accumulator map ready.
func newParseState(parsed dem.Parser, opts Options, match *model.Match) *parseState {
	return &parseState{
		parsed:         parsed,
		opts:           opts,
		cal:            opts.Calibration.withDefaults(),
		match:          match,
		players:        map[uint64]*model.Player{},
		lastPos:        map[uint64]model.Position{},
		lastPosTime:    map[uint64]time.Duration{},
		playerSpeed:    map[uint64]float64{},
		counterStrafes: map[uint64]*counterStrafeAcc{},
		shotsAtEnemy:   map[uint64]int{},
		hitsOnEnemy:    map[uint64]int{},
		lastShotTime:   map[uint64]time.Duration{},
		curSpray:       map[uint64]*sprayRun{},
		sprayHits:      map[uint64]int{},
		sprayShots:     map[uint64]int{},
		sprayByWeapon:  map[uint64]map[string]*sprayAgg{},
		sprayDev:       map[uint64]map[string]*sprayDevAgg{},
		hitTimes:       map[uint64][]int64{},
		activeSmokes:   map[int]r3.Vector{},
		engagements:    map[[2]uint64]*engagement{},
		flashLead:      map[uint64]pendingFlash{},
		ttdSamples:     map[uint64][]float64{},
		ttdByVictim:    map[[2]uint64][]float64{},
		crosshair:      map[uint64][]float64{},
		sideToTeam: map[common.Team]string{
			common.TeamCounterTerrorists: "A",
			common.TeamTerrorists:        "B",
		},
		live:  make([]pv, 0, 10),
		cands: make([]cand, 0, 32),
	}
}
