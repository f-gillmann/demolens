package model

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

// FlashedPlayer is one player blinded by a flashbang, with how long.
type FlashedPlayer struct {
	SteamID           uint64  `json:"steam_id,string"`
	Side              string  `json:"side"` // victim's side
	BlindMicroseconds int64   `json:"blind_microseconds"`
	MaxAlpha          float64 `json:"max_alpha,omitempty"` // peak whiteout 0..255
}
