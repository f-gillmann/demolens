package model

type Match struct {
	FileHash    string      `json:"file_hash"` // SHA-256 of the demo bytes
	Meta        Meta        `json:"meta"`
	Players     []Player    `json:"players"`
	Rounds      []Round     `json:"rounds"`
	DuelMatrix  []Duel      `json:"duel_matrix"`  // head-to-head kill counts
	FlashMatrix []FlashPair `json:"flash_matrix"` // who blinded whom, plus total blind time
}

// Duel is one killer's record against one victim over the whole match.
type Duel struct {
	Killer       uint64         `json:"killer,string"`
	Victim       uint64         `json:"victim,string"`
	Kills        int            `json:"kills"`
	Damage       int            `json:"damage"`                   // health damage killer dealt to victim
	TimeToDamage float64        `json:"time_to_damage,omitempty"` // avg ms seeing to first damage, needs a map mesh
	Weapons      map[string]int `json:"weapons,omitempty"`
}

// FlashPair: how often and how long a flasher blinded one victim.
type FlashPair struct {
	Flasher           uint64 `json:"flasher,string"`
	Flashed           uint64 `json:"flashed,string"`
	Count             int    `json:"count"`
	BlindMicroseconds int64  `json:"blind_microseconds"`
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

type Player struct {
	SteamID  uint64 `json:"steam_id,string"`
	Name     string `json:"name"`
	Team     string `json:"team"`
	TeamName string `json:"team_name"` // clan name, empty in matchmaking/pugs

	// Raw data
	Kills        int `json:"kills"`
	Deaths       int `json:"deaths"`
	Assists      int `json:"assists"`
	FlashAssists int `json:"flash_assists"`
	Headshots    int `json:"headshots"`
	Damage       int `json:"damage"`
	TeamDamage   int `json:"team_damage"`
	ShotsFired   int `json:"shots_fired"`
	ExitKills    int `json:"exit_kills"` // after the round was decided, kept out of K/D
	ExitDeaths   int `json:"exit_deaths"`

	// Computed metrics
	KD           float64 `json:"kd"`
	ADR          float64 `json:"adr"`
	KPR          float64 `json:"kpr"`
	DPR          float64 `json:"dpr"`
	APR          float64 `json:"apr"`
	KAST         float64 `json:"kast"`
	HSPercent    float64 `json:"hs_percent"`    // headshot kills / kills
	Accuracy     float64 `json:"accuracy"`      // hits / shots fired
	HeadAccuracy float64 `json:"head_accuracy"` // head hits / hits, AWP excluded
	// The rest of this block needs a map mesh for line of sight and is dropped when none is loaded.
	SpottedAccuracy          float64 `json:"spotted_accuracy,omitempty"`       // hits / shots, enemy in view
	SpottedShots             int     `json:"spotted_shots,omitempty"`          // denominator of the above
	SpottedHits              int     `json:"spotted_hits,omitempty"`           // numerator
	SprayAccuracy            float64 `json:"spray_accuracy,omitempty"`         // share of spray bullets that hit
	TimeToDamage             float64 `json:"time_to_damage,omitempty"`         // avg ms, seeing an enemy to first damage
	TimeToDamageSamples      int     `json:"time_to_damage_samples,omitempty"` // engagements measured. low means noisy
	CrosshairPlacement       float64 `json:"crosshair_placement"`              // median deg moved from sighting to hit
	CrosshairSamples         int     `json:"crosshair_samples"`                // low means noisy
	TradeKillOpportunities   int     `json:"trade_kill_opportunities"`
	TradeKillAttempts        int     `json:"trade_kill_attempts"`
	TradeKills               int     `json:"trade_kills"`
	TradedDeathOpportunities int     `json:"traded_death_opportunities"`
	TradedDeathAttempts      int     `json:"traded_death_attempts"`
	TradedDeaths             int     `json:"traded_deaths"`
	Rating1                  float64 `json:"hltv_rating_1"`
	Rating2                  float64 `json:"hltv_rating_2"` // 2.0, approximate

	NoScopeKills    int        `json:"no_scope_kills"`
	WallbangKills   int        `json:"wallbang_kills"`
	CollateralKills int        `json:"collateral_kills"` // 2+ enemies on one bullet
	MultiKills      MultiKills `json:"multi_kills"`

	// Valve comp/premier rank. only set for Valve MM demos, 0 otherwise.
	Rank            int `json:"rank"`
	RankType        int `json:"rank_type"`
	CompetitiveWins int `json:"competitive_wins"`

	WeaponStats map[string]WeaponStat `json:"weapon_stats"`

	SprayWeapons map[string]WeaponSpray `json:"spray_weapons,omitempty"` // spray accuracy per weapon

	SprayPatterns map[string]SprayDeviation `json:"spray_patterns,omitempty"` // recoil-pattern deviation per weapon

	CounterStrafe *CounterStrafe `json:"counter_strafe,omitempty"` // needs a map mesh

	// Opening duels (first kill of each round), split by side
	OpeningOverall OpeningStats `json:"opening_overall"`
	OpeningT       OpeningStats `json:"opening_t"`
	OpeningCT      OpeningStats `json:"opening_ct"`

	// Clutches (last alive on their side vs N enemies), split by side
	ClutchOverall ClutchStats `json:"clutch_overall"`
	ClutchT       ClutchStats `json:"clutch_t"`
	ClutchCT      ClutchStats `json:"clutch_ct"`

	Utility         UtilityStats    `json:"utility"` // match totals, summed over rounds
	UtilityAverages UtilityAverages `json:"utility_averages"`
}

// MultiKills counts rounds in which a player got exactly n kills.
type MultiKills struct {
	K1 int `json:"k1"`
	K2 int `json:"k2"`
	K3 int `json:"k3"`
	K4 int `json:"k4"`
	K5 int `json:"k5"`
}

// WeaponStat is a player's kill/damage breakdown for a single weapon.
type WeaponStat struct {
	Kills     int `json:"kills"`
	Headshots int `json:"headshots"`
	Damage    int `json:"damage"`
}

// WeaponSpray holds a weapon's match spray accuracy: count of 3+ shot sprays
// fired and the share of those bullets that landed.
type WeaponSpray struct {
	Sprays   int     `json:"sprays"`
	Accuracy float64 `json:"accuracy"`
}

// SprayDeviation: how a player's sprays with one weapon matched its recoil
// pattern, averaged over every 3+ shot spray they fired.
type SprayDeviation struct {
	Sprays       int           `json:"sprays"`
	AvgDeviation float64       `json:"avg_deviation"` // mean deg off from ideal recoil compensation
	Bullets      []SprayBullet `json:"bullets"`       // one per shot index
}

// SprayBullet compares, at a given shot index, the ideal aim offset that cancels
// the recoil pattern (should) against what the player actually did (player). Both
// in degrees from the first shot. Good sprayers track should closely.
type SprayBullet struct {
	Index   int     `json:"index"`
	ShouldX float64 `json:"should_x"`
	ShouldY float64 `json:"should_y"`
	PlayerX float64 `json:"player_x"`
	PlayerY float64 `json:"player_y"`
}

// CounterStrafe measures how stopped a player was when firing. We look at rifle
// shots at an enemy in vision (not fully crouched) and call a shot "good" when
// speed sits below 40% of the weapon's max move speed.
type CounterStrafe struct {
	Shots       int     `json:"shots"`        // rifle shots measured, enemy in vision, not crouched
	Stopped     int     `json:"stopped"`      // of those, fired under the accuracy speed cap
	StoppedRate float64 `json:"stopped_rate"` // stopped / shots, as a percent
	AvgSpeed    float64 `json:"avg_speed"`    // avg horizontal speed when firing, u/s
}

// OpeningStats sums up a player's opening-duel involvement.
type OpeningStats struct {
	Attempts int `json:"attempts"` // opening duels taken part in, kill or death
	Kills    int `json:"kills"`    // won
	Deaths   int `json:"deaths"`   // lost
	Traded   int `json:"traded"`   // opening deaths that got traded
}

// ClutchStats sums up a player's clutch outcomes.
type ClutchStats struct {
	Won   int `json:"won"`
	Lost  int `json:"lost"`
	Saved int `json:"saved"` // lost the round but the clutcher lived
	Kills int `json:"kills"` // kills during clutches
}

// UtilityStats holds raw utility counts. Used both per round (RoundPlayer) and as
// a match total (Player).
type UtilityStats struct {
	FlashesThrown          int   `json:"flashes_thrown"`
	SmokesThrown           int   `json:"smokes_thrown"`
	HEsThrown              int   `json:"hes_thrown"`
	MolotovsThrown         int   `json:"molotovs_thrown"` // molotov + incendiary together
	DecoysThrown           int   `json:"decoys_thrown"`
	EnemiesFlashed         int   `json:"enemies_flashed"`
	TeammatesFlashed       int   `json:"teammates_flashed"`
	FlashesLeadingToKill   int   `json:"flashes_leading_to_kill"`  // enemy killed by thrower's team while fully blind from this flash
	EnemyBlindMicroseconds int64 `json:"enemy_blind_microseconds"` // total blind time put on enemies
	HEDamage               int   `json:"he_damage"`
	HETeamDamage           int   `json:"he_team_damage"`
	MolotovDamage          int   `json:"molotov_damage"`
	MolotovTeamDamage      int   `json:"molotov_team_damage"`
	UsedUtilityValue       int   `json:"used_utility_value"`   // $ of util thrown
	UnusedUtilityValue     int   `json:"unused_utility_value"` // $ of util still in hand at death, summed over deaths
}

// UtilityAverages holds a player's match-level utility averages.
type UtilityAverages struct {
	BlindMicroseconds  float64 `json:"blind_microseconds"` // blind time per enemy flashed
	HEDamage           float64 `json:"he_damage"`          // per HE thrown
	HETeamDamage       float64 `json:"he_team_damage"`
	MolotovDamage      float64 `json:"molotov_damage"` // per molotov thrown
	MolotovTeamDamage  float64 `json:"molotov_team_damage"`
	UnusedUtilityValue float64 `json:"unused_utility_value"` // unused util per death
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

// Grenade tracks one thrown grenade from release to detonation.
type Grenade struct {
	Thrower                  uint64   `json:"thrower,string"`
	Side                     string   `json:"side"`                       // thrower's side, "CT"/"T"
	Type                     string   `json:"type"`                       // flash / smoke / he / molotov / incendiary / decoy
	ThrowTimeMicroseconds    int64    `json:"throw_time_microseconds"`    // since round start
	DetonateTimeMicroseconds int64    `json:"detonate_time_microseconds"` // landing / pop
	ExpireTimeMicroseconds   int64    `json:"expire_time_microseconds"`   // fade / burn-out. equals detonate for flash/he
	FlightMicroseconds       int64    `json:"flight_microseconds"`        // throw to detonation
	ThrowPosition            Position `json:"throw_position"`
	DetonatePosition         Position `json:"detonate_position"`

	// Flash outcomes, only for flashbangs
	EnemiesFlashed   int             `json:"enemies_flashed,omitempty"`
	TeammatesFlashed int             `json:"teammates_flashed,omitempty"`
	Flashed          []FlashedPlayer `json:"flashed,omitempty"` // blind time per victim

	// Trajectory. only filled when the grenade-paths option is set.
	Path    []Position `json:"path,omitempty"`    // sampled in-flight positions
	Bounces []Position `json:"bounces,omitempty"` // wall/floor bounce points
}

// FlashedPlayer is one player blinded by a flashbang, with how long.
type FlashedPlayer struct {
	SteamID           uint64 `json:"steam_id,string"`
	Side              string `json:"side"` // victim's side
	BlindMicroseconds int64  `json:"blind_microseconds"`
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
