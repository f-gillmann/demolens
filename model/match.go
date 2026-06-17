package model

import (
	"strconv"
)

// SteamIDList marshals each SteamID64 as a JSON string.
type SteamIDList []uint64

func (l SteamIDList) MarshalJSON() ([]byte, error) {
	if len(l) == 0 {
		return []byte("[]"), nil
	}
	out := make([]byte, 0, len(l)*20)
	out = append(out, '[')
	for i, id := range l {
		if i > 0 {
			out = append(out, ',')
		}
		out = append(out, '"')
		out = strconv.AppendUint(out, id, 10)
		out = append(out, '"')
	}
	out = append(out, ']')
	return out, nil
}

type Match struct {
	FileHash         string      `json:"file_hash"`      // SHA-256 of the demo bytes
	SchemaVersion    int         `json:"schema_version"` // const 4
	Meta             Meta        `json:"meta"`
	Players          []Player    `json:"players"`
	Rounds           []Round     `json:"rounds"`
	DuelMatrixTotal  []Duel      `json:"duel_matrix_total"`  // whole-match head-to-head rollup with per-pair TTD
	FlashMatrixTotal []FlashPair `json:"flash_matrix_total"` // whole-match who-blinded-whom rollup

	// match-level connect/disconnect/bot-takeover log, playing-team players only,
	// sorted by time at finalize for determinism.
	MatchLifecycle []LifecycleEvent `json:"match_lifecycle,omitempty"`
}

// LifecycleEvent is one connection/bot transition for a playing-team player.
type LifecycleEvent struct {
	Type             string `json:"type"`            // disconnect / connect / bot_connect / bot_taken_over
	SteamID          uint64 `json:"steam_id,string"` // matches the steam_id string convention used across the model
	Name             string `json:"name,omitempty"`
	Round            int    `json:"round"` // 1-based round in progress at the event
	TimeMicroseconds int64  `json:"time_microseconds"`
}

type Meta struct {
	MapName              string     `json:"map_name"`
	ServerName           string     `json:"server_name"`
	ClientName           string     `json:"client_name"`
	ServerPlatform       string     `json:"server_platform"` // best guess: valve / esl / esea / faceit / ... / unknown
	GameMode             string     `json:"game_mode"`       // competitive / premier / wingman / ... valve demos only, often empty
	DemoType             string     `json:"demo_type"`       // gotv / pov
	WorkshopID           string     `json:"workshop_id"`     // addon id, empty for official maps
	IsHltv               bool       `json:"is_hltv"`
	IsDedicatedServer    bool       `json:"is_dedicated_server"`
	BuildNum             string     `json:"build_num"`
	TickRate             float64    `json:"tick_rate"`
	DurationMicroseconds int64      `json:"duration_microseconds"`
	TotalRounds          int        `json:"total_rounds"`
	Score                Score      `json:"score"`
	Output               OutputMeta `json:"output"`
}

type Score struct {
	TeamA int `json:"team_a"`
	TeamB int `json:"team_b"`
	// Clan/team names if the demo carries them. empty in matchmaking/pugs.
	TeamAName string `json:"team_a_name,omitempty"`
	TeamBName string `json:"team_b_name,omitempty"`
}

type Round struct {
	Number                int            `json:"number"` // 1-based
	WinnerSide            string         `json:"winner_side"`
	WinnerTeam            string         `json:"winner_team"`             // "A" or "B"
	Reason                string         `json:"reason"`                  // elimination / bomb_exploded / bomb_defused / time_expired
	PostRoundMicroseconds int64          `json:"post_round_microseconds"` // round end to next freeze, the exit window
	Economy               RoundEconomy   `json:"economy"`
	Players               []RoundPlayer  `json:"players"`
	Kills                 []RoundKill    `json:"kills"`                // live-round kill timeline, enriched with duel semantics
	ExitKills             []RoundKill    `json:"exit_kills,omitempty"` // post-round kills
	Damages               []Damage       `json:"damages"`              // live-round damage events
	OpeningDuel           *OpeningDuel   `json:"opening_duel,omitempty"`
	ShotStats             []ShotStat     `json:"shot_stats,omitempty"` // core per-player-per-weapon aggregate
	Grenades              Grenades       `json:"grenades"`             // typed buckets
	Pickups               []WeaponPickup `json:"pickups,omitempty"`    // TRUE pickups only (original_owner != holder)
	Bomb                  *Bomb          `json:"bomb,omitempty"`       // nil unless the bomb was planted

	// Heavy opt-in detail. set only when at least one stream is on.
	Streams *RoundStreams `json:"streams,omitempty"`
}

// WeaponPickup is one TRUE weapon pickup in a round: the picked-up gun's
// original_owner differs from the holder who picked it up.
type WeaponPickup struct {
	SteamID          uint64 `json:"steam_id,string"`
	Weapon           string `json:"weapon"`
	OriginalOwner    uint64 `json:"original_owner,omitempty,string"`
	FromEnemy        bool   `json:"from_enemy,omitempty"`
	TimeMicroseconds int64  `json:"time_microseconds"`
}

// Bomb is the plant/defuse/explode outcome for a round.
type Bomb struct {
	Site                  string   `json:"site"` // "A" or "B"
	Planter               uint64   `json:"planter,string"`
	PlantTimeMicroseconds int64    `json:"plant_time_microseconds"` // since round start
	PlantPosition         Position `json:"plant_position"`

	Defused                bool     `json:"defused"`
	Defuser                uint64   `json:"defuser,omitempty,string"`
	DefuseTimeMicroseconds int64    `json:"defuse_time_microseconds,omitempty"` // since round start
	DefusePosition         Position `json:"defuse_position,omitempty"`
	Exploded               bool     `json:"exploded"`
}

// PlayerFrame is one player's sampled position and state at a single frame.
type PlayerFrame struct {
	TimeMicroseconds int64    `json:"time_microseconds"` // since round start
	SteamID          uint64   `json:"steam_id,string"`
	Side             string   `json:"side"`
	Position         Position `json:"position"`
	Yaw              float64  `json:"yaw"`
	Pitch            float64  `json:"pitch"`
	Health           int      `json:"health"`
	Armor            int      `json:"armor"`
	Money            int      `json:"money"`
	IsAlive          bool     `json:"is_alive"`
	IsAirborne       bool     `json:"is_airborne"`
	IsScoped         bool     `json:"is_scoped"`
	IsDucking        bool     `json:"is_ducking"`
	HasDefuseKit     bool     `json:"has_defuse_kit"`
	ActiveWeapon     string   `json:"active_weapon"`

	// cheap per-frame state. ride only on the positions frame, not on kills.
	IsWalking  bool    `json:"is_walking,omitempty"` // walk (shift) modifier held
	InBuyZone  bool    `json:"in_buy_zone,omitempty"`
	InBombZone bool    `json:"in_bomb_zone,omitempty"`
	Stamina    float64 `json:"stamina,omitempty"`     // jump/landing stamina
	DuckAmount float64 `json:"duck_amount,omitempty"` // 0..1 partial crouch (raw m_flDuckAmount)
}

// Shot is one weapon-fire event plus the shooter's geometry.
type Shot struct {
	TimeMicroseconds int64    `json:"time_microseconds"` // since round start
	Shooter          uint64   `json:"shooter,string"`
	Weapon           string   `json:"weapon"`
	Position         Position `json:"position"`
	Yaw              float64  `json:"yaw"`
	Pitch            float64  `json:"pitch"`
	RecoilIndex      float64  `json:"recoil_index"` // shots into the current spray
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

// OpeningDuel is a round's first kill.
type OpeningDuel struct {
	TimeMicroseconds int64  `json:"time_microseconds"`
	Killer           uint64 `json:"killer,string"`
	KillerSide       string `json:"killer_side"`
	Victim           uint64 `json:"victim,string"`
	VictimSide       string `json:"victim_side"`
	Weapon           string `json:"weapon"`
	Traded           bool   `json:"traded"` // victim was traded inside the trade window
}

// Clutch is a 1vN where the player is last alive on their side. It hangs off the
// clutcher's RoundPlayer, so clutcher and side are already implied.
type Clutch struct {
	Opponents int  `json:"opponents"` // the N in 1vN, measured when the clutch began
	Kills     int  `json:"kills"`     // clutcher kills during it
	Won       bool `json:"won"`       // clutcher's side took the round
	Saved     bool `json:"saved"`     // round lost but the clutcher lived
}

type RoundEconomy struct {
	CT TeamEconomy `json:"ct"`
	T  TeamEconomy `json:"t"`
}

type TeamEconomy struct {
	EquipmentValue int    `json:"equipment_value"`
	BuyType        string `json:"buy_type"` // eco / semi_eco / semi_buy / full_buy
}

// RoundKill is one kill with the geometry and circumstances around it.

type RoundKill struct {
	TimeMicroseconds int64    `json:"time_microseconds"`
	Killer           uint64   `json:"killer,string"`
	KillerSide       string   `json:"killer_side"` // CT/T at this round
	KillerPosition   Position `json:"killer_position"`
	Victim           uint64   `json:"victim,string"`
	VictimSide       string   `json:"victim_side"` // CT/T at this round
	VictimPosition   Position `json:"victim_position"`
	Assister         uint64   `json:"assister,omitempty,string"`
	Weapon           string   `json:"weapon"`
	Headshot         bool     `json:"headshot"`
	Wallbang         bool     `json:"wallbang"`
	Penetration      int      `json:"penetration"` // objects the bullet passed through
	ThroughSmoke     bool     `json:"through_smoke"`
	NoScope          bool     `json:"no_scope"`
	Distance         float64  `json:"distance"` // killer to victim, game units
	AttackerBlind    bool     `json:"attacker_blind"`
	VictimBlind      bool     `json:"victim_blind"`
	KillerAirborne   bool     `json:"killer_airborne"`
	VictimAirborne   bool     `json:"victim_airborne"`
	KillerSpeed      float64  `json:"killer_speed"`       // killer horizontal speed at the kill, u/s, derived
	KillerSpeedRatio float64  `json:"killer_speed_ratio"` // speed / weapon max move speed. lower is better

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

type Damage struct {
	TimeMicroseconds int64  `json:"time_microseconds"`
	Attacker         uint64 `json:"attacker,string"`
	Victim           uint64 `json:"victim,string"`
	HealthDamage     int    `json:"health_damage"` // capped at the victim's remaining HP
	ArmorDamage      int    `json:"armor_damage"`
	HitGroup         string `json:"hit_group"`   // head / chest / stomach / left_arm / ... / generic
	Weapon           string `json:"weapon"`      // e.g. AK-47, HE Grenade, Molotov
	DamageType       string `json:"damage_type"` // bullet / he / fire / knife / taser / bomb / world / other
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
