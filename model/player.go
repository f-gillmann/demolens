package model

type Player struct {
	SteamID  uint64 `json:"steam_id,string"`
	Name     string `json:"name"`
	Team     string `json:"team"`
	TeamName string `json:"team_name"`          // clan name, empty in matchmaking/pugs
	Color    string `json:"color,omitempty"`    // minimap slot color: yellow/purple/green/blue/orange. empty for grey/unknown
	ClanTag  string `json:"clan_tag,omitempty"` // per-player clan tag, distinct from team_name (the clan name)

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
	KAST         float64 `json:"kast_pct"`
	HSPercent    float64 `json:"hs_pct"`            // headshot kills / kills
	Accuracy     float64 `json:"accuracy_pct"`      // hits / shots fired
	HeadAccuracy float64 `json:"head_accuracy_pct"` // head hits / hits, AWP excluded
	// The omitempty fields below need a map mesh for line of sight and are dropped when none is loaded.
	SpottedAccuracy          float64 `json:"spotted_accuracy_pct,omitempty"`   // hits / shots, enemy in view
	SpottedShots             int     `json:"spotted_shots,omitempty"`          // denominator of the above
	SpottedHits              int     `json:"spotted_hits,omitempty"`           // numerator
	SprayAccuracy            float64 `json:"spray_accuracy_pct,omitempty"`     // share of spray bullets that hit
	TimeToDamage             float64 `json:"time_to_damage_ms,omitempty"`      // avg ms, seeing an enemy to first damage
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

	NoScopeKills    int `json:"no_scope_kills"`
	WallbangKills   int `json:"wallbang_kills"`
	CollateralKills int `json:"collateral_kills"` // 2+ enemies on one bullet
	// kill-type counters. chicken_kills is player-total only (no chicken victim per round).
	KnifeKills    int        `json:"knife_kills,omitempty"`
	ZeusKills     int        `json:"zeus_kills,omitempty"`
	ChickenKills  int        `json:"chicken_kills,omitempty"`
	AirborneKills int        `json:"airborne_kills,omitempty"`  // killer_airborne
	BlindKills    int        `json:"blind_kills,omitempty"`     // killer was blind (attacker_blind)
	ScopedKills   int        `json:"scoped_kills,omitempty"`    // killer was scoped
	PickedUpKills int        `json:"picked_up_kills,omitempty"` // kills made with a picked-up gun
	MultiKills    MultiKills `json:"multi_kills"`

	// Valve comp/premier rank. only set for Valve MM demos, 0 otherwise.
	Rank            int `json:"rank"`
	RankType        int `json:"rank_type"`
	CompetitiveWins int `json:"competitive_wins"`
	// rank predictions. Valve-MM/Premier only; 0/absent otherwise.
	RankIfWin     int    `json:"rank_if_win,omitempty"`
	RankIfLoss    int    `json:"rank_if_loss,omitempty"`
	RankIfTie     int    `json:"rank_if_tie,omitempty"`
	CrosshairCode string `json:"crosshair_code,omitempty"` // shareable crosshair profile string

	WeaponStats map[string]WeaponStat `json:"weapon_stats"`

	SprayWeapons map[string]WeaponSpray `json:"spray_weapons,omitempty"` // spray accuracy per weapon

	SprayPatterns []SprayDeviation `json:"spray_patterns,omitempty"` // recoil-pattern deviation, one per weapon variant

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

// CounterStrafe measures how stopped a player was when firing. We look at rifle
// shots at an enemy in vision (not fully crouched) and call a shot "good" when
// speed sits below 40% of the weapon's max move speed.
type CounterStrafe struct {
	Shots       int     `json:"shots"`            // rifle shots measured, enemy in vision, not crouched
	Stopped     int     `json:"stopped"`          // of those, fired under the accuracy speed cap
	StoppedRate float64 `json:"stopped_rate_pct"` // stopped / shots, as a percent
	AvgSpeed    float64 `json:"avg_speed"`        // avg horizontal speed when firing, u/s
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
