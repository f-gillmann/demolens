package model

// PlayerStats is the per-player nested derived block (players[].stats): the ratios,
// ratings and percentages computed FROM the player's raw counts. Kept separate from
// those counts so the two can't silently disagree. NOTE: this is a different scope
// from the root-level Stats (match aggregates); same key name, by design.
type PlayerStats struct {
	KD           float64 `json:"kd"`
	ADR          float64 `json:"adr"`
	KPR          float64 `json:"kpr"`
	DPR          float64 `json:"dpr"`
	APR          float64 `json:"apr"`
	KAST         float64 `json:"kast_pct"`
	HSPercent    float64 `json:"hs_pct"`            // headshot kills / kills
	Accuracy     float64 `json:"accuracy_pct"`      // hits / shots fired
	HeadAccuracy float64 `json:"head_accuracy_pct"` // head hits / hits, AWP excluded
	// mesh-gated (line of sight): dropped when no map mesh is loaded.
	SpottedAccuracy    float64        `json:"spotted_accuracy_pct,omitempty"` // hits / shots, enemy in view
	SprayAccuracy      float64        `json:"spray_accuracy_pct,omitempty"`   // share of spray bullets that hit
	TimeToDamage       float64        `json:"time_to_damage_ms,omitempty"`    // median ms, seeing an enemy to first damage
	CrosshairPlacement float64        `json:"crosshair_placement"`            // median deg moved from sighting to hit
	Rating1            float64        `json:"hltv_rating_1"`
	Rating2            float64        `json:"hltv_rating_2"`         // 2.0, approximate
	RoundSwing         float64        `json:"round_swing"`           // summed win-probability added (WPA) across the match; a zero-sum win-prob model
	RoundSwingPerRound float64        `json:"round_swing_per_round"` // round_swing normalized per round played; the comparable per-round WPA
	SwingBreakdown     SwingBreakdown `json:"swing_breakdown"`       // where round_swing came from
}

// SwingBreakdown splits a player's round_swing by source. The five source fields
// (kills, damage, flash, trade, deaths) sum to round_swing; the pair
// in_won_rounds + in_lost_rounds also sums to round_swing.
type SwingBreakdown struct {
	Kills  float64 `json:"kills"`  // the 35% final-kill share, taken as the killer
	Damage float64 `json:"damage"` // the 30% damage share, by health damage dealt to the victim
	Flash  float64 `json:"flash"`  // the 15% flash-assist share
	Trade  float64 `json:"trade"`  // the 20% share taken as the avenged (traded) teammate
	Deaths float64 `json:"deaths"` // the negative eaten as a victim (<= 0)
	InWon  float64 `json:"in_won_rounds"`
	InLost float64 `json:"in_lost_rounds"`
}

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
	Mvps         int `json:"mvps"` // scoreboard MVP awards (m_iMVPs), summed from the per-round MVPs

	// Derived ratios / ratings / percentages, nested so they can't be confused with
	// the raw counts above. See PlayerStats.
	Stats PlayerStats `json:"stats"`

	// Raw counts that feed the derived stats (kept top-level, directly tallied).
	// The omitempty fields below need a map mesh for line of sight and drop when none is loaded.
	SpottedShots        int `json:"spotted_shots,omitempty"`          // spotted-accuracy denominator
	SpottedHits         int `json:"spotted_hits,omitempty"`           // spotted-accuracy numerator
	TimeToDamageSamples int `json:"time_to_damage_samples,omitempty"` // engagements measured. low means noisy
	CrosshairSamples    int `json:"crosshair_samples"`                // low means noisy

	// raw per-engagement samples, only emitted with the aim-debug option. used by the
	// offline calibration sweep; omitted (nil) in normal output.
	TimeToDamageRaw          []float64 `json:"time_to_damage_raw,omitempty"` // raw ms samples, pre-reduction (no exclude/clamp)
	CrosshairRaw             []float64 `json:"crosshair_raw,omitempty"`      // raw deg samples, pre-median
	TradeKillOpportunities   int       `json:"trade_kill_opportunities"`
	TradeKillAttempts        int       `json:"trade_kill_attempts"`
	TradeKills               int       `json:"trade_kills"`
	TradedDeathOpportunities int       `json:"traded_death_opportunities"`
	TradedDeathAttempts      int       `json:"traded_death_attempts"`
	TradedDeaths             int       `json:"traded_deaths"`

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
	RankIfWin  int `json:"rank_if_win,omitempty"`
	RankIfLoss int `json:"rank_if_loss,omitempty"`
	RankIfTie  int `json:"rank_if_tie,omitempty"`
	// rank revealed by the end-of-match ServerRankUpdate message; unlike Rank it
	// includes a rank earned by the match itself (placement finished, rank up/down).
	// 0/absent when the demo ends before the reveal or the player stayed unranked.
	RankAfter     int    `json:"rank_after,omitempty"`
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
