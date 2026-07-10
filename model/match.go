package model

// SteamIDList marshals each SteamID64 as a JSON string.
type SteamIDList []uint64

type Match struct {
	FileHash      string   `json:"file_hash"`      // SHA-256 of the demo bytes
	SchemaVersion int      `json:"schema_version"` // const 6
	Meta          Meta     `json:"meta"`
	Players       []Player `json:"players"`
	Rounds        []Round  `json:"rounds"`
	Stats         Stats    `json:"stats"` // match-level aggregates (duel/flash pairs, lifecycle log)
}

// Stats groups the match-level aggregate blocks at the document root. This is a
// SEPARATE scope from the per-player players[].stats; same key, different level.
type Stats struct {
	DuelPairs  []Duel      `json:"duel_pairs"`  // whole-match head-to-head pairs/edge list with per-pair TTD
	FlashPairs []FlashPair `json:"flash_pairs"` // whole-match who-blinded-whom pairs/edge list

	// match-level connect/disconnect/bot-takeover log, playing-team players only,
	// sorted by time at finalize for determinism.
	MatchLifecycle []LifecycleEvent `json:"match_lifecycle,omitempty"`
}

// LifecycleEvent is one connection/bot transition for a playing-team player.
type LifecycleEvent struct {
	Type    string `json:"kind"`            // disconnect / connect / bot_connect / bot_taken_over
	SteamID uint64 `json:"steam_id,string"` // matches the steam_id string convention used across the model
	Name    string `json:"name,omitempty"`
	Round   int    `json:"round"` // 1-based round in progress at the event
	TMs     int64  `json:"t_ms"`  // absolute match time, ms
}

type Meta struct {
	MapName           string     `json:"map_name"`
	ServerName        string     `json:"server_name"`
	ClientName        string     `json:"client_name"`
	ServerPlatform    string     `json:"server_platform"` // best guess: valve / esl / esea / faceit / ... / unknown
	GameMode          string     `json:"game_mode"`       // competitive / premier / wingman / ... valve demos only, often empty
	DemoType          string     `json:"demo_type"`       // gotv / pov
	WorkshopID        string     `json:"workshop_id"`     // addon id, empty for official maps
	IsHltv            bool       `json:"is_hltv"`
	IsDedicatedServer bool       `json:"is_dedicated_server"`
	BuildNum          string     `json:"build_num"`
	C4WaveSpeed       int        `json:"c4_wave_speed,omitempty"` // planted-c4 shockwave speed, game units/sec; present only on post-rework demos (build_num >= 14168), see finalize.go
	TickRate          float64    `json:"tick_rate"`
	DurationMs        int64      `json:"duration_ms"`
	TotalRounds       int        `json:"total_rounds"`
	Score             Score      `json:"score"`
	Output            OutputMeta `json:"output"`
}

type Score struct {
	TeamA int `json:"team_a"`
	TeamB int `json:"team_b"`
	// Clan/team names if the demo carries them. empty in matchmaking/pugs.
	TeamAName string `json:"team_a_name,omitempty"`
	TeamBName string `json:"team_b_name,omitempty"`
}

type Round struct {
	Number       int            `json:"number"` // 1-based
	WinnerSide   string         `json:"winner_side"`
	WinnerTeam   string         `json:"winner_team"`    // "A" or "B"
	Reason       string         `json:"reason"`         // elimination / bomb_exploded / bomb_defused / time_expired
	RoundStartMs int64          `json:"round_start_ms"` // freeze (round) start, ms; negative, relative to go-live at t=0
	FreezeEndMs  int64          `json:"freeze_end_ms"`  // freeze end / round goes live, ms; always 0 (the round timeline origin)
	RoundEndMs   int64          `json:"round_end_ms"`   // round-live start to round end, ms
	PostRoundMs  int64          `json:"post_round_ms"`  // round end to next freeze, the exit window, ms
	Economy      RoundEconomy   `json:"economy"`
	Players      []RoundPlayer  `json:"players"`
	Kills        []RoundKill    `json:"kills"`                // live-round kill timeline, enriched with duel semantics
	ExitKills    []RoundKill    `json:"exit_kills,omitempty"` // post-round kills
	Damages      []Damage       `json:"damages"`              // live-round damage events, plus the post-round c4 detonation hits
	ShotStats    []ShotStat     `json:"shot_stats,omitempty"` // core per-player-per-weapon aggregate
	Grenades     Grenades       `json:"grenades"`             // typed buckets
	Pickups      []WeaponPickup `json:"pickups,omitempty"`    // TRUE pickups only (original_owner != holder)
	Bomb         *Bomb          `json:"bomb,omitempty"`       // nil unless the bomb was planted

	// Heavy opt-in detail. set only when at least one stream is on.
	Streams *RoundStreams `json:"streams,omitempty"`
}

// WeaponPickup is one TRUE weapon pickup in a round: the picked-up gun's
// original_owner differs from the holder who picked it up.
type WeaponPickup struct {
	SteamID       uint64 `json:"steam_id,string"`
	Weapon        string `json:"weapon"`
	OriginalOwner uint64 `json:"original_owner,omitempty,string"`
	FromEnemy     bool   `json:"from_enemy,omitempty"`
	TMs           int64  `json:"t_ms"` // since round start, ms
}

// Bomb is the plant/defuse/explode outcome for a round. It also exists for a round
// with only a fake plant (plant_attempts but no completed plant), so the completed-
// plant fields are all omitempty.
type Bomb struct {
	Site          string    `json:"site,omitempty"` // "A" or "B"
	Planter       uint64    `json:"planter,omitempty,string"`
	PlantMs       int64     `json:"plant_ms,omitempty"`       // since round start, ms
	PlantPosition *Position `json:"plant_position,omitempty"` // nil until a plant completes

	Defused        bool      `json:"defused"`
	Defuser        uint64    `json:"defuser,omitempty,string"`
	DefuseMs       int64     `json:"defuse_ms,omitempty"`       // completion, since round start, ms
	DefusePosition *Position `json:"defuse_position,omitempty"` // nil/absent when nobody defused; never a {0,0,0} sentinel
	Exploded       bool      `json:"exploded"`

	DefuseStartedMs int64           `json:"defuse_started_ms,omitempty"` // the successful defuse's start, since round start, ms
	HasKit          bool            `json:"has_kit,omitempty"`           // successful defuse used a kit (same token as DefuseAttempt.has_kit)
	DefuseAttempts  []DefuseAttempt `json:"defuse_attempts,omitempty"`
	PlantAttempts   []PlantAttempt  `json:"plant_attempts,omitempty"`
}

// DefuseAttempt is one started defuse, completed or aborted.
type DefuseAttempt struct {
	TMs     int64  `json:"t_ms"` // start, since round start, ms
	Defuser uint64 `json:"defuser,string"`
	HasKit  bool   `json:"has_kit,omitempty"`
	Aborted bool   `json:"aborted,omitempty"` // started then cancelled (fake/forced off)
}

// PlantAttempt is one started plant, completed or aborted (a fake plant). Mirrors
// DefuseAttempt; a round can carry plant_attempts with no completed-plant fields.
type PlantAttempt struct {
	TMs     int64  `json:"t_ms"` // start, since round start, ms
	Planter uint64 `json:"planter,string"`
	Aborted bool   `json:"aborted,omitempty"` // started then cancelled (fake plant)

	Completed bool `json:"-"` // in-memory only: matched a completed plant, so no longer open for an abort
}

// PositionFields is the declared per-row column order of a streams.positions tuple,
// mirrored at meta.output.positions_fields so the rows are self-describing.
// streams.positions is an object keyed by steam_id, so steam_id is NOT a row field.
// The flags column packs the booleans alive=1, airborne=2, scoped=4, ducking=8,
// has_defuse_kit=16, buyzone=32, walking=64, bomb_zone=128; every other field rides
// inline.
var PositionFields = []string{
	"t_ms", "side", "x", "y", "z", "vx", "vy", "vz", "yaw", "pitch",
	"health", "armor", "money", "flags", "active_weapon", "place",
	"stamina", "duck_amount", "hold_frames",
}

// position flag bits packed into the tuple's flags column. The consumer decodes
// exactly these values.
const (
	frameFlagAlive        = 1
	frameFlagAirborne     = 2
	frameFlagScoped       = 4
	frameFlagDucking      = 8
	frameFlagHasDefuseKit = 16
	frameFlagBuyZone      = 32
	frameFlagWalking      = 64
	frameFlagBombZone     = 128
)

// PlayerFrame is one player's sampled position and state at a single frame. It
// marshals as a compact columnar tuple (see PositionFields / MarshalJSON), not an
// object, so the repeated key names drop out of the positions stream.
type PlayerFrame struct {
	TMs          int64     // since round start, ms
	SteamID      uint64    //
	Side         string    //
	Position     Position  //
	Velocity     *Position // velocity vector (u/s), derived from the position delta (CS2 doesn't network m_vecVelocity); 0 vector when unknown
	Yaw          float64   //
	Pitch        float64   //
	Health       int       //
	Armor        int       //
	Money        int       //
	IsAlive      bool      //
	IsAirborne   bool      //
	IsScoped     bool      //
	IsDucking    bool      //
	HasDefuseKit bool      //
	// active weapon, always resolved (never empty). On a weapon-switch/defuse/dead
	// tick where the engine reports no active weapon, this falls back to a sentinel:
	// "defuse_kit" while defusing, "c4" while planting or carrying the bomb, else the
	// last-known active weapon for the player.
	ActiveWeapon string

	// cheap per-frame state. ride only on the positions frame, not on kills.
	IsWalking  bool    // packed into the flags column (walking bit)
	InBuyZone  bool    // packed into the flags column (buyzone bit)
	InBombZone bool    // packed into the flags column (bomb_zone bit)
	Stamina    float64 // jump/landing stamina
	DuckAmount float64 // 0..1 partial crouch (raw m_flDuckAmount)
	Place      string  // m_szLastPlaceName callout region, e.g. "Banana", "Mid"

	// HoldFrames is the number of additional sample periods this exact state
	// persisted; the consumer holds the player static for HoldFrames more sample
	// periods after t (no interpolation during a hold). 0 = a normal single frame.
	// Set by the finalize-time RLE compression pass.
	HoldFrames int
}

// Shot is one weapon-fire event plus the shooter's geometry.
type Shot struct {
	TMs      int64    `json:"t_ms"` // since round start, ms
	Shooter  uint64   `json:"shooter,string"`
	Weapon   string   `json:"weapon"`
	Position Position `json:"position"`
	Yaw      float64  `json:"yaw"`
	Pitch    float64  `json:"pitch"`
	// RecoilIndex is the engine recoil index: the shot number into the current
	// spray. Map it against the shipped players[].spray_patterns table rather than
	// reimplementing recoil math. NOTE: spray_patterns are base/pattern-space bytes
	// from the weaponX/weaponY recoil table; they are NOT real GOTV recoil (real
	// recoil from CMsgTEFireBullets is ~2x and lives in a different space).
	RecoilIndex float64 `json:"recoil_index"`
}

// RoundPlayer is one player's contribution in a single round. This is the
// per-round source of truth. Match-level Player stats are just the sum over rounds.
type RoundPlayer struct {
	SteamID uint64 `json:"steam_id,string"`
	Side    string `json:"side"` // "CT" or "T"

	Kills        int `json:"kills"`
	Deaths       int `json:"deaths"`
	Assists      int `json:"assists"`
	FlashAssists int `json:"flash_assists"`
	Headshots    int `json:"headshots"`
	Damage       int `json:"damage"`
	TeamDamage   int `json:"team_damage"`
	ShotsFired   int `json:"shots_fired"`
	ExitKills    int `json:"exit_kills"` // after the round was decided
	ExitDeaths   int `json:"exit_deaths"`

	// per-round kill-type counters. NO chicken_kills here (chickens are player-total only).
	KnifeKills    int `json:"knife_kills,omitempty"`
	ZeusKills     int `json:"zeus_kills,omitempty"`
	AirborneKills int `json:"airborne_kills,omitempty"`  // killer_airborne
	BlindKills    int `json:"blind_kills,omitempty"`     // killer was blind (attacker_blind)
	ScopedKills   int `json:"scoped_kills,omitempty"`    // killer was scoped
	PickedUpKills int `json:"picked_up_kills,omitempty"` // kills made with a picked-up gun

	Utility UtilityStats `json:"utility"`

	// Economy. EquipmentValue is captured at buy-window close, capped at death (was
	// freeze-time-end). Loadout stays the freeze-time-end item snapshot.
	StartMoney     int     `json:"start_money"`
	MoneySpent     int     `json:"money_spent"`
	EquipmentValue int     `json:"equipment_value"` // value at buy-window close, capped at death (was freeze-time-end)
	Loadout        Loadout `json:"loadout"`         // freeze-time-end inventory snapshot

	// per-round connection state, captured at freeze-end.
	IsConnected      bool `json:"is_connected,omitempty"`       // connected at freeze-end (false = bot fill / gone)
	IsControllingBot bool `json:"is_controlling_bot,omitempty"` // human driving a bot this round

	// Opening-duel role, computed
	OpenedDuel         bool `json:"opened_duel"`
	WonOpeningDuel     bool `json:"won_opening_duel"`
	OpeningDeathTraded bool `json:"opening_death_traded"` // lost the opener and got traded

	Clutch *Clutch `json:"clutch,omitempty"` // set only if this player clutched
}

// Clutch is a 1vN where the player is last alive on their side. It hangs off the
// clutcher's RoundPlayer, so clutcher and side are already implied.
type Clutch struct {
	Opponents int  `json:"opponents"` // the N in 1vN, measured when the clutch began
	Kills     int  `json:"kills"`     // clutcher kills during it
	Won       bool `json:"won"`       // clutcher's side took the round
	Saved     bool `json:"saved"`     // round lost but the clutcher lived

	StartMs     int64       `json:"start_ms,omitempty"`     // when the clutch began, since round start, ms
	OpponentIDs SteamIDList `json:"opponent_ids,omitempty"` // enemies alive when the clutch began, sorted ascending
}

type RoundEconomy struct {
	CT TeamEconomy `json:"CT"`
	T  TeamEconomy `json:"T"`
}

type TeamEconomy struct {
	EquipmentValue int    `json:"equipment_value"`
	BuyType        string `json:"buy_type"` // eco / semi_eco / semi_buy / full_buy
}

// RoundKill is one kill with the geometry and circumstances around it.

type RoundKill struct {
	TMs              int64     `json:"t_ms"`                      // since round start, ms
	Killer           *uint64   `json:"killer,string"`             // null when kind != "player" (bomb/world/suicide have no player killer)
	KillerSide       string    `json:"killer_side,omitempty"`     // CT/T at this round; absent for non-player kills
	KillerPosition   *Position `json:"killer_position,omitempty"` // nil for non-player kills (no live killer)
	Victim           uint64    `json:"victim,string"`
	VictimSide       string    `json:"victim_side"` // CT/T at this round
	VictimPosition   Position  `json:"victim_position"`
	Assister         uint64    `json:"assister,omitempty,string"`
	FlashAssister    uint64    `json:"flash_assister,omitempty,string"`
	Weapon           string    `json:"weapon"`
	WeaponClass      string    `json:"weapon_class"`         // pistol / smg / rifle / heavy / "" for bomb/world
	Kind             string    `json:"kind"`                 // player / bomb / world / suicide
	Collateral       bool      `json:"collateral,omitempty"` // 2+ kills shared the same (killer, time)
	Headshot         bool      `json:"headshot"`
	Wallbang         bool      `json:"wallbang"`
	Penetration      int       `json:"penetration"` // objects the bullet passed through
	ThroughSmoke     bool      `json:"through_smoke"`
	NoScope          bool      `json:"no_scope"`
	Distance         float64   `json:"distance,omitempty"` // killer to victim, game units; absent for non-player kills
	AttackerBlind    bool      `json:"attacker_blind"`
	VictimBlind      bool      `json:"victim_blind"`
	KillerAirborne   bool      `json:"killer_airborne"`
	VictimAirborne   bool      `json:"victim_airborne"`
	KillerSpeed      float64   `json:"killer_speed,omitempty"`       // killer horizontal speed at the kill, u/s, derived; absent for non-player kills
	KillerSpeedRatio float64   `json:"killer_speed_ratio,omitempty"` // speed / weapon max move speed. lower is better; absent for non-player kills

	// duel-semantics enrichment, derived in metrics.
	Opening         bool        `json:"opening"`                    // true for the round's first kill (kills[0])
	Traded          bool        `json:"traded,omitempty"`           // death traded within 4s by a teammate
	TradedBy        uint64      `json:"traded_by,omitempty,string"` // the teammate who traded this death
	PossibleTraders SteamIDList `json:"possible_traders,omitempty"` // teammates alive and in a position to trade

	PickedUp     bool `json:"picked_up,omitempty"`     // kill-weapon was picked up: original_owner != killer
	KillerScoped bool `json:"killer_scoped,omitempty"` // killer was scoped at the kill

	// alive at this kill. used internally for trade proximity, not serialized.
	AlivePlayers []AlivePlayer `json:"-"`
}

// KillerID is the killer's SteamID64, or 0 when there is no player killer (bomb/
// world/suicide). Lets the metrics keep their uint64 keying without nil checks.
func (k RoundKill) KillerID() uint64 {
	if k.Killer == nil {
		return 0
	}
	return *k.Killer
}

type Damage struct {
	TMs          int64  `json:"t_ms"`                      // since round start, ms; a c4 detonation hit lands past round_end_ms (shockwave travel)
	Attacker     uint64 `json:"attacker,string,omitempty"` // absent for the c4 detonation (no player attacker)
	Victim       uint64 `json:"victim,string"`
	HealthDamage int    `json:"health_damage"` // capped at the victim's remaining HP
	ArmorDamage  int    `json:"armor_damage"`
	HitGroup     string `json:"hit_group"`   // head / chest / stomach / left_arm / ... / generic
	Weapon       string `json:"weapon"`      // e.g. AK-47, HE Grenade, Molotov
	DamageType   string `json:"damage_type"` // bullet / he / fire / knife / taser / bomb / world / other

	// set only when the positions stream is on, to keep core-tier output lean.
	AttackerPosition *Position `json:"attacker_position,omitempty"`
	VictimPosition   *Position `json:"victim_position,omitempty"`
}

type Position struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	Z float64 `json:"z"`
}

type AlivePlayer struct {
	SteamID  uint64   `json:"steam_id,string"`
	Position Position `json:"position"`
}
