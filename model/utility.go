package model

// UtilityStats holds raw utility counts. Used both per round (RoundPlayer) and as
// a match total (Player).
type UtilityStats struct {
	FlashesThrown        int   `json:"flashes_thrown"`
	SmokesThrown         int   `json:"smokes_thrown"`
	HEsThrown            int   `json:"hes_thrown"`
	MolotovsThrown       int   `json:"molotovs_thrown"` // molotov + incendiary together
	DecoysThrown         int   `json:"decoys_thrown"`
	EnemiesFlashed       int   `json:"enemies_flashed"`
	TeammatesFlashed     int   `json:"teammates_flashed"`
	FlashesLeadingToKill int   `json:"flashes_leading_to_kill"` // enemy killed by thrower's team while fully blind from this flash
	EnemyBlindMs         int64 `json:"enemy_blind_ms"`          // total blind time put on enemies, ms
	HEDamage             int   `json:"he_damage"`
	HETeamDamage         int   `json:"he_team_damage"`
	MolotovDamage        int   `json:"molotov_damage"`
	MolotovTeamDamage    int   `json:"molotov_team_damage"`
	UsedUtilityValue     int   `json:"used_utility_value"`   // $ of util thrown
	UnusedUtilityValue   int   `json:"unused_utility_value"` // $ of util still in hand at death, summed over deaths
}

// UtilityAverages holds a player's match-level utility averages.
type UtilityAverages struct {
	BlindMs            float64 `json:"blind_ms"`  // blind time per enemy flashed, ms
	HEDamage           float64 `json:"he_damage"` // per HE thrown
	HETeamDamage       float64 `json:"he_team_damage"`
	MolotovDamage      float64 `json:"molotov_damage"` // per molotov thrown
	MolotovTeamDamage  float64 `json:"molotov_team_damage"`
	UnusedUtilityValue float64 `json:"unused_utility_value"` // unused util per death
}

// FlashedPlayer is one player blinded by a flashbang, with how long.
type FlashedPlayer struct {
	SteamID uint64 `json:"steam_id,string"`
	Side    string `json:"side"`     // victim's side
	BlindMs int64  `json:"blind_ms"` // calibrated effective blind time, ms
	// raw networked flash effect duration, ms: the clock the client whiteout fade
	// runs on (blind_ms is this scaled by the blind-time calibration). the fade
	// starts at the grenade's detonate_ms; curve in docs/output.md.
	FlashDurationMs int64   `json:"flash_duration_ms"`
	MaxAlpha        float64 `json:"max_alpha,omitempty"` // peak whiteout 0..255
}
