package model

type Match struct {
	FileHash    string      `json:"file_hash"` // SHA-256 of the demo bytes
	Meta        Meta        `json:"meta"`
	Players     []Player    `json:"players"`
	Rounds      []Round     `json:"rounds"`
	DuelMatrix  []Duel      `json:"duel_matrix"`  // head-to-head kill counts
	FlashMatrix []FlashPair `json:"flash_matrix"` // who blinded whom, plus total blind time
}

type Meta struct {
	MapName              string  `json:"map_name"`
	ServerName           string  `json:"server_name"`
	ClientName           string  `json:"client_name"`
	ServerPlatform       string  `json:"server_platform"` // best guess: valve / esl / esea / faceit / ... / unknown
	GameMode             string  `json:"game_mode"`       // competitive / premier / wingman / ... valve demos only, often empty
	DemoType             string  `json:"demo_type"`       // gotv / pov
	WorkshopID           string  `json:"workshop_id"`     // addon id, empty for official maps
	IsHltv               bool    `json:"is_hltv"`
	IsDedicatedServer    bool    `json:"is_dedicated_server"`
	BuildNum             string  `json:"build_num"`
	TickRate             float64 `json:"tick_rate"`
	DurationMicroseconds int64   `json:"duration_microseconds"`
	TotalRounds          int     `json:"total_rounds"`
	Score                Score   `json:"score"`
}

type Score struct {
	TeamA int `json:"team_a"`
	TeamB int `json:"team_b"`
	// Clan/team names if the demo carries them. empty in matchmaking/pugs.
	TeamAName string `json:"team_a_name,omitempty"`
	TeamBName string `json:"team_b_name,omitempty"`
}

type Round struct {
	Number     int           `json:"number"` // 1-based
	WinnerSide string        `json:"winner_side"`
	WinnerTeam string        `json:"winner_team"` // "A" or "B"
	Reason     string        `json:"reason"`      // elimination / bomb_exploded / bomb_defused / time_expired
	Economy    RoundEconomy  `json:"economy"`
	Players    []RoundPlayer `json:"players"`
	Kills      []RoundKill   `json:"kills"`   // live-round kill timeline
	Damages    []Damage      `json:"damages"` // live-round damage events

	OpeningDuel           *OpeningDuel `json:"opening_duel,omitempty"`
	ExitKills             []RoundKill  `json:"exit_kills,omitempty"`    // post-round kills
	PostRoundMicroseconds int64        `json:"post_round_microseconds"` // round end to next freeze, the exit window
	Grenades              []Grenade    `json:"grenades,omitempty"`
	Bomb                  *Bomb        `json:"bomb,omitempty"` // nil unless the bomb was planted

	// Heavy samples. only filled in when the matching parse option is on.
	PlayerFrames []PlayerFrame `json:"player_frames,omitempty"`
	Shots        []Shot        `json:"shots,omitempty"`
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

	Utility UtilityStats `json:"utility"`

	// Economy, snapshotted at freeze-time end after buys
	StartMoney     int      `json:"start_money"`
	MoneySpent     int      `json:"money_spent"`
	EquipmentValue int      `json:"equipment_value"`
	Loadout        []string `json:"loadout,omitempty"` // weapons/util held going in

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
	KillerPosition   Position `json:"killer_position"`
	Victim           uint64   `json:"victim,string"`
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
