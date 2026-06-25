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

// flashAlphaKey identifies one (grenade, victim) flash so we can keep the peak
// whiteout alpha across re-samples and apply it to the right flashed[] entry.
type flashAlphaKey struct {
	grenade int64
	victim  uint64
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

// parseGrenade is the per-round working accumulator for one thrown grenade. It
// is an internal parser type, not a model type: at finalize it fans out into the
// right model.Grenades bucket by Type. grenadeID joins it to streams.grenade_paths.
type parseGrenade struct {
	grenadeID string
	thrower   uint64
	side      string
	gtype     string // flash / smoke / he / molotov / incendiary / decoy

	throwTimeMicroseconds    int64
	detonateTimeMicroseconds int64
	expireTimeMicroseconds   int64
	flightMicroseconds       int64
	throwPosition            model.Position
	detonatePosition         model.Position
	detonated                bool // detonate seen, so we don't overwrite

	// flash outcomes (flashbangs only)
	enemiesFlashed   int
	teammatesFlashed int
	flashed          []model.FlashedPlayer

	// per-grenade damage (he + molotov), clamped in onPlayerHurt
	damageDealt int
	teamDamage  int
	victims     []model.GrenadeVictim

	// molotov/incendiary only: the inferno's widest per-flame footprint, captured
	// at peak extent while polling and emitted as fire_cells.
	fireCells []model.Position
}

// shotStatAcc is the per-round, per-(shooter,weapon) tally feeding round.shot_stats.
// shots counts every gun fire; spotted counts fires while an enemy was in vision.
// hits are computed at finalize from the round's own damages.
type shotStatAcc struct {
	shots   int
	spotted int
}

// parseState holds the accumulator maps and per-parse state for one Parse run.
// Per-subsystem accumulators live in sub-structs; core fields stay at the top.
type parseState struct {
	parsed dem.Parser
	opts   Options
	cal    Calibration

	match   *model.Match
	players map[uint64]*model.Player

	roundStart     time.Duration
	roundEndTime   time.Duration // when the round ended, for the exit-window duration
	pending        *model.Round
	pendingPlayers map[uint64]*model.RoundPlayer
	roundLive      bool           // true between freezetime end and round end
	dmgToVictim    map[uint64]int // cumulative health damage per victim this round, capped at 100hp to fix the demoinfocs shotgun killing-pellet overcount

	// per-round accumulators, reset at freezetime end.
	shotStats    map[uint64]map[string]*shotStatAcc // per (shooter, weapon) shots/spotted for round.shot_stats
	lastInvHash  map[uint64]string                  // last inventory fingerprint per player, to skip unchanged snapshots
	firstContact bool                               // latched at the round's first kill/damage, for first_contact snapshots
	droppedOpen  map[int]*model.DroppedWeapon       // open gun-on-ground stints keyed by weapon entity id, closed on pickup / flushed at round end

	// A starts CT, B starts T. Flipped on every side switch so a player's A/B
	// identity stays put when the sides swap.
	sideToTeam map[common.Team]string

	aim      aimState
	vision   visionState
	grenades grenadeState
	econ     economyState
	frames   frameState
}

// aimState holds the per-player aim-calibration accumulators (spray, counter-
// strafe, crosshair, time-to-damage), reset at freezetime end.
type aimState struct {
	counterStrafes map[uint64]*counterStrafeAcc
	shotsAtEnemy   map[uint64]int
	hitsOnEnemy    map[uint64]int
	lastShotTime   map[uint64]time.Duration
	curSpray       map[uint64]*sprayRun
	sprayHits      map[uint64]int
	sprayShots     map[uint64]int
	sprayByWeapon  map[uint64]map[string]*sprayAgg
	sprayDev       map[uint64]map[string]*sprayDevAgg // per-weapon recoil deviation
	hitTimes       map[uint64][]int64                 // per shooter, demo times (us) a bullet hit an enemy
	ttdSamples     map[uint64][]float64               // ms samples per shooter, averaged at the end
	ttdByVictim    map[[2]uint64][]float64            // same samples split by victim for the duel matrix
	crosshair      map[uint64][]float64               // crosshair-move samples in deg
}

// visionState holds the geometric-sighting machinery: collision mesh, live
// smokes, per-pair sighting state, and the reused per-frame scratch slices.
type visionState struct {
	mesh         *geom.Mesh
	meshTried    bool
	activeSmokes map[int]r3.Vector         // entityID to cloud pos while blooming
	engagements  map[[2]uint64]*engagement // (shooter,enemy) sighting state, wiped each round
	live         []pv                      // alive players' eye+view, rebuilt per frame
	cands        []cand                    // frustum-passing pairs deferred to the parallel los pass
}

// grenadeState holds the per-round thrown-grenade accumulators, wiped each round.
type grenadeState struct {
	pendingGrenades map[int64]*parseGrenade   // keyed by projectile UniqueID, fanned out at finalize
	entityToUnique  map[int]int64             // grenade entity id to UniqueID, needed for landing events
	liveInfernos    map[int64]*liveInferno    // polled until they burn out
	flashLead       map[uint64]pendingFlash   // wiped each round
	flashAlpha      map[flashAlphaKey]float64 // peak whiteout alpha per (grenade, victim), wiped each round
	grenadeSeq      int                       // per-round counter for grenade_id assignment
}

// economyState holds the buy-window capture: equipment_value is snapshotted at
// min(go-live+mp_buytime, death).
type economyState struct {
	buyDeadline     time.Duration   // go-live + mp_buytime; survivors captured at/after this frame
	buyCaptured     map[uint64]bool // players whose buy-window value is already locked
	buyWindowClosed bool            // gates the survivor-capture loop to run once per round
}

// frameState holds the per-frame position sampling used to derive speed, since
// CS2 doesn't network velocity.
type frameState struct {
	lastFrameSample  time.Duration
	lastPos          map[uint64]model.Position
	lastPosTime      map[uint64]time.Duration
	playerSpeed      map[uint64]float64
	playerVelocity   map[uint64]model.Position // per-frame velocity vector, same delta source as playerSpeed
	lastActiveWeapon map[uint64]string         // last non-empty active weapon per player, for the active_weapon fallback
}

// newParseState builds the empty per-run state with every accumulator map ready.
func newParseState(parsed dem.Parser, opts Options, match *model.Match) *parseState {
	return &parseState{
		parsed:      parsed,
		opts:        opts,
		cal:         opts.Calibration.withDefaults(),
		match:       match,
		players:     map[uint64]*model.Player{},
		dmgToVictim: map[uint64]int{},
		shotStats:   map[uint64]map[string]*shotStatAcc{},
		lastInvHash: map[uint64]string{},
		droppedOpen: map[int]*model.DroppedWeapon{},
		sideToTeam: map[common.Team]string{
			common.TeamCounterTerrorists: "A",
			common.TeamTerrorists:        "B",
		},
		aim: aimState{
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
			ttdSamples:     map[uint64][]float64{},
			ttdByVictim:    map[[2]uint64][]float64{},
			crosshair:      map[uint64][]float64{},
		},
		vision: visionState{
			activeSmokes: map[int]r3.Vector{},
			engagements:  map[[2]uint64]*engagement{},
			live:         make([]pv, 0, 10),
			cands:        make([]cand, 0, 32),
		},
		grenades: grenadeState{
			flashLead:  map[uint64]pendingFlash{},
			flashAlpha: map[flashAlphaKey]float64{},
		},
		econ: economyState{
			buyCaptured: map[uint64]bool{},
		},
		frames: frameState{
			lastPos:          map[uint64]model.Position{},
			lastPosTime:      map[uint64]time.Duration{},
			playerSpeed:      map[uint64]float64{},
			playerVelocity:   map[uint64]model.Position{},
			lastActiveWeapon: map[uint64]string{},
		},
	}
}

// ensureStreams lazily allocates the round's RoundStreams holder so a stream
// handler can append into it. The pointer stays nil (and so omitempty drops it)
// until the first stream write of the round. Safe to call when pending is nil.
func (st *parseState) ensureStreams() *model.RoundStreams {
	if st.pending == nil {
		return nil
	}
	if st.pending.Streams == nil {
		st.pending.Streams = &model.RoundStreams{}
	}
	return st.pending.Streams
}

// roundMicros is the microseconds elapsed since round start (freeze-end).
func (st *parseState) roundMicros() int64 {
	return (st.parsed.CurrentTime() - st.roundStart).Microseconds()
}
