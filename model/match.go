package model

type Match struct {
	FileHash string   `json:"file_hash"` // SHA-256 of the demo bytes
	Meta     Meta     `json:"meta"`      // match metadata
	Players  []Player `json:"players"`   // per-player raw data + computed metrics
	Rounds   []Round  `json:"rounds"`    // full per-round record
}

type Meta struct {
	MapName              string  `json:"map_name"`
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
}

type Player struct {
	SteamID  uint64 `json:"steam_id"`
	Name     string `json:"name"`
	Team     string `json:"team"`
	TeamName string `json:"team_name"` // clan name. empty in matchmaking/pugs

	// raw data
	Kills        int `json:"kills"`
	Deaths       int `json:"deaths"`
	Assists      int `json:"assists"`
	FlashAssists int `json:"flash_assists"`
	Headshots    int `json:"headshots"`
	Damage       int `json:"damage"`
	TeamDamage   int `json:"team_damage"`

	// computed metrics
	KD                       float64 `json:"kd"`
	ADR                      float64 `json:"adr"`
	KPR                      float64 `json:"kpr"`
	DPR                      float64 `json:"dpr"`
	APR                      float64 `json:"apr"`
	KAST                     float64 `json:"kast"`
	HSPercent                float64 `json:"hs_percent"`
	TradeKillOpportunities   int     `json:"trade_kill_opportunities"`
	TradeKillAttempts        int     `json:"trade_kill_attempts"`
	TradeKills               int     `json:"trade_kills"` // success
	TradedDeathOpportunities int     `json:"traded_death_opportunities"`
	TradedDeathAttempts      int     `json:"traded_death_attempts"`
	TradedDeaths             int     `json:"traded_deaths"`   // success
	Rating1                  float64 `json:"hltv_rating_1-0"` // HLTV 1.0 Rating
	Rating2                  float64 `json:"hltv_rating_2-0"` // HLTV 2.0 Rating (approximate)
}

type Round struct {
	Number     int          `json:"number"`      // starts with 1.
	WinnerSide string       `json:"winner_side"` // "CT" or "T"
	WinnerTeam string       `json:"winner_team"` // "A" or "B"
	Reason     string       `json:"reason"`      // elimination / bomb_exploded / bomb_defused / time_expired
	Economy    RoundEconomy `json:"economy"`
	Kills      []RoundKill  `json:"kills"`
	Damages    []Damage     `json:"damages"`
}

type RoundEconomy struct {
	CT TeamEconomy `json:"ct"`
	T  TeamEconomy `json:"t"`
}

type TeamEconomy struct {
	EquipmentValue int    `json:"equipment_value"`
	BuyType        string `json:"buy_type"` // eco / semi_eco / semi_buy / full_buy
}

type RoundKill struct {
	TimeMicroseconds int64    `json:"time_microseconds"`
	Killer           uint64   `json:"killer"`
	KillerPosition   Position `json:"killer_position"`
	Victim           uint64   `json:"victim"`
	VictimPosition   Position `json:"victim_position"`
	Assister         uint64   `json:"assister,omitempty"`
	Weapon           string   `json:"weapon"`
	Headshot         bool     `json:"headshot"`
	Wallbang         bool     `json:"wallbang"`
	ThroughSmoke     bool     `json:"through_smoke"`
	NoScope          bool     `json:"no_scope"`

	AlivePlayers []AlivePlayer `json:"alive_players"` // players alive at this kill, with positions
}

type Damage struct {
	TimeMicroseconds int64  `json:"time_microseconds"`
	Attacker         uint64 `json:"attacker"`
	Victim           uint64 `json:"victim"`
	HealthDamage     int    `json:"health_damage"` // capped at victim's remaining HP
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
	SteamID  uint64   `json:"steam_id"`
	Position Position `json:"position"`
}
