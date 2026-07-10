package parser

import (
	"time"

	"github.com/f-gillmann/demolens/v2/internal/geom"
	"github.com/f-gillmann/demolens/v2/model"
	"github.com/golang/geo/r3"
	dem "github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/common"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/sendtables"
)

// kitStint is one open defuse-kit ground stint: the live world entity plus the
// ground-item record built from it. The kit ships as a generic CBaseAnimGraph
// prop that never networks an owner, so a pickup is paired from a CT's defuse-kit
// flag gaining next to the entity (see pollKits), not from an owner handle.
type kitStint struct {
	ent     sendtables.Entity
	dw      *model.DroppedItem
	lastPos model.Position // last sampled ground position, for pickup proximity
	gone    bool           // entity destroyed: stop sampling, keep until pickup/flush
}

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
	id          uint64
	team        common.Team
	eye, view   r3.Vector
	feet        r3.Vector // feet/origin position, the body-column anchor for losAnyPartFeet
	blindRemain float64   // seconds of flash blindness left this frame, 0 if not blind
	blindFrac   float64   // fraction of the current flash's duration still remaining this frame, 0 if no active flash
}

// cand is a frustum-passing pair whose expensive mesh raycasts are deferred to
// the batched parallel pass. The cheap occlusion gates (smoke, fire, third-party
// bodies) run eagerly at capture time because they read per-frame mutable state
// (voxel clouds, live infernos, player positions) that the deferred pass must
// not touch; their results ride along in noSmoke/visGate/needVis.
type cand struct {
	en         *engagement
	sEye, eEye r3.Vector
	eFeet      r3.Vector // enemy feet/origin, the body-column anchor for the losAnyPartFeet vis test
	sView      r3.Vector // shooter view at this frame, the crosshair peek anchor
	sID, vID   uint64    // shooter / victim steam ids, for the aim-debug dump only
	sBlind     float64   // shooter's remaining flash blindness (seconds) this frame, for the aim-debug dump only
	sBlindFrac float64   // fraction of the shooter's current flash still remaining this frame, gates the sighting clocks
	angOff     float64   // degrees between shooter view and dir-to-victim, for the per-metric fov gate
	noSmoke    bool      // captured at frame time: sightline clear of active smoke clouds
	needVis    bool      // vis is consumed this frame (inside a metric fov, or the aim-debug dump wants it)
	visGate    bool      // captured cheap gates for vis: noSmoke and not fire- or body-blocked
	vis        bool      // dense any-part losAnyPartFeet visibility, minus smoke (crosshair detection)
	visN       bool      // narrow 9-ray losClear visibility, minus smoke (TTD detection)
	visTorso   bool      // strict torso-column losTorso visibility, minus smoke; aim-debug probe only, filled only when AimDebugPath set
}

// batchFrame delimits one frame's slice of the deferred los batch: which cands
// and which not-visible engagement resets belong to it, and the frame's time.
type batchFrame struct {
	now                time.Duration
	candLo, candHi     int
	notVisLo, notVisHi int
}

// losBatch buffers frames of sighting work so the mesh raycasts, ~all of the
// runtime, fan out across every core instead of one frame's handful of pairs.
// Machine updates replay strictly in frame order at flush, and every reader of
// engagement state (player-hurt, weapon-fire, round reset) drains the batch
// first, so the observable state at any read matches the unbatched run exactly.
type losBatch struct {
	cands  []cand
	frames []batchFrame
	notVis []*engagement
}

// parseGrenade is the per-round working accumulator for one thrown grenade. It
// is an internal parser type, not a model type: at finalize it fans out into the
// right model.Grenades bucket by Type. grenadeID joins it to streams.grenade_paths.
type parseGrenade struct {
	grenadeID string
	thrower   uint64
	side      string
	gtype     string // flash / smoke / he / molotov / incendiary / decoy

	throwT           int64 // ms, since round start
	detonateT        int64 // ms
	expireT          int64 // ms
	flightMs         int64 // ms
	throwPosition    model.Position
	detonatePosition model.Position
	detonated        bool // detonate seen, so we don't overwrite

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

	// smoke only: the exported voxel-cloud occupancy track, appended by
	// sampleSmokeVoxels while the grenade_paths stream is on.
	voxels *model.SmokeVoxels
}

// shotStatAcc is the per-round, per-(shooter,weapon) tally feeding round.shot_stats.
// shots counts every gun fire; spotted counts fires while an enemy was in vision.
// hits are computed at finalize from the round's own damages.
type shotStatAcc struct {
	shots   int
	spotted int
}

// frame-capture phase for the positions stream. Drives onPlayerFrames: live/post
// attach frames to the current round, freeze buffers the upcoming round's pre-roll.
// phaseFreeze is the zero value so a fresh parseState starts buffering.
const (
	phaseFreeze = iota
	phaseLive
	phasePost
)

// bufferedFrame is a freezetime position snapshot held until the round goes live,
// then rebased to a negative timestamp relative to go-live at the pre-roll flush.
type bufferedFrame struct {
	abs   time.Duration
	frame model.PlayerFrame
}

// parseState holds the accumulator maps and per-parse state for one Parse run.
// Per-subsystem accumulators live in sub-structs; core fields stay at the top.
type parseState struct {
	parsed dem.Parser
	opts   Options
	cal    Calibration

	match   *model.Match
	players map[uint64]*model.Player

	framePeriod    time.Duration // positions-stream sample interval, from Options.PositionsHz
	roundStart     time.Duration
	freezeStart    time.Duration // most recent (non-warmup) RoundStart, for the round_start_t marker
	roundEndTime   time.Duration // when the round ended, for the exit-window duration
	pending        *model.Round
	pendingPlayers map[uint64]*model.RoundPlayer
	roundLive      bool           // true between freezetime end and round end
	dmgToVictim    map[uint64]int // cumulative health damage per victim this round, capped at 100hp to fix the demoinfocs shotgun killing-pellet overcount

	framePhase int             // positions-stream capture phase, see phase consts
	prerollBuf []bufferedFrame // freezetime frames awaiting rebase onto the next round at go-live

	// per-round accumulators, reset at freezetime end.
	shotStats        map[uint64]map[string]*shotStatAcc // per (shooter, weapon) shots/spotted for round.shot_stats
	lastInvHash      map[uint64]string                  // last inventory fingerprint per player, to skip unchanged snapshots
	firstContact     bool                               // latched at the round's first kill/damage, for first_contact snapshots
	groundItemsOpen  map[int]*model.DroppedItem         // open gun-on-ground stints keyed by weapon entity id, closed on pickup / flushed at round end
	groundItemSerial map[int]int                        // entity serial per open stint, to detect slot reuse (Source 2 reuses entity ids) so a track doesn't teleport across two weapons
	kitOpen          map[int]*kitStint                  // open defuse-kit ground stints keyed by kit entity id, closed on pickup / flushed at round end
	kitHad           map[uint64]bool                    // last-frame defuse-kit flag per player, to detect a pickup's flag gain
	kitModel         uint64                             // locked CBaseAnimGraph model handle of the dropped kit; later props with another model are ignored
	kitModelSet      bool
	deathTimes       map[uint64]int64 // victim steam_id -> death time (roundMs); resolves ground-item on_death by drop-to-death proximity, since the weapon un-owns ~1 tick before the death event fires

	// A starts CT, B starts T. Flipped on every side switch so a player's A/B
	// identity stays put when the sides swap.
	sideToTeam map[common.Team]string

	aim      aimState
	vision   visionState
	grenades grenadeState
	econ     economyState
	frames   frameState

	aimDump *aimDumper // raw aim-calibration CSV dumper, non-nil only when opts.AimDebugPath is set
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

// activeSmoke is one live smoke cloud: where it bloomed and when, so the
// occlusion probe can track the visual lifetime instead of the entity's.
// vox carries the decoded voxel occupancy when the demo networks it; the
// sphere fallback in smokeBlocked only applies while vox has no usable data.
type activeSmoke struct {
	pos   r3.Vector
	start time.Duration // demo time at SmokeStart (detonation)
	vox   *voxelSmoke
}

// visionState holds the geometric-sighting machinery: collision mesh, live
// smokes, per-pair sighting state, and the reused per-frame scratch slices.
type visionState struct {
	mesh         *geom.Mesh
	meshTried    bool
	activeSmokes map[int]activeSmoke       // entityID to live smoke cloud
	voxelSmokes  map[int]*voxelSmoke       // entityID to decoded voxel stream, removed on entity destroy
	engagements  map[[2]uint64]*engagement // (shooter,enemy) sighting state, wiped each round
	live         []pv                      // alive players' eye+view, rebuilt per frame
	batch        losBatch                  // frames of deferred mesh raycasts + machine updates
	byID         map[uint64]*common.Player // alive players this frame, id->player, for the aim-debug spotted lookup; nil unless AimDebugPath set
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
	lastFrameSample      time.Duration
	lastGroundItemSample time.Duration // ground-item position-track sample cursor, same framePeriod cadence as lastFrameSample
	lastPos              map[uint64]model.Position
	lastPosTime          map[uint64]time.Duration
	playerSpeed          map[uint64]float64
	playerVelocity       map[uint64]model.Position // per-frame velocity vector, same delta source as playerSpeed
	lastActiveWeapon     map[uint64]string         // last non-empty active weapon per player, for the active_weapon fallback
}

// newParseState builds the empty per-run state with every accumulator map ready.
func newParseState(parsed dem.Parser, opts Options, match *model.Match) *parseState {
	st := &parseState{
		parsed:           parsed,
		opts:             opts,
		cal:              opts.Calibration.withDefaults(),
		framePeriod:      framePeriod(opts.PositionsHz),
		match:            match,
		players:          map[uint64]*model.Player{},
		dmgToVictim:      map[uint64]int{},
		shotStats:        map[uint64]map[string]*shotStatAcc{},
		lastInvHash:      map[uint64]string{},
		groundItemsOpen:  map[int]*model.DroppedItem{},
		groundItemSerial: map[int]int{},
		kitOpen:          map[int]*kitStint{},
		kitHad:           map[uint64]bool{},
		deathTimes:       map[uint64]int64{},
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
			activeSmokes: map[int]activeSmoke{},
			voxelSmokes:  map[int]*voxelSmoke{},
			engagements:  map[[2]uint64]*engagement{},
			live:         make([]pv, 0, 10),
			batch: losBatch{
				cands:  make([]cand, 0, 1024),
				frames: make([]batchFrame, 0, 256),
				notVis: make([]*engagement, 0, 1024),
			},
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
	// allocate the dumper (files open lazily on first write) only when enabled.
	if opts.AimDebugPath != "" {
		st.aimDump = newAimDumper(opts.AimDebugPath)
		st.vision.byID = map[uint64]*common.Player{}
	}
	return st
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

// roundMs is the milliseconds elapsed since round start (freeze-end / go-live).
func (st *parseState) roundMs() int64 {
	return (st.parsed.CurrentTime() - st.roundStart).Milliseconds()
}
