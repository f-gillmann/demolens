package model

// ShotStat is the core-tier per-player-per-weapon shot aggregate, so stat consumers
// lose nothing when the per-shot stream is off. spotted_shots is mesh-gated.
type ShotStat struct {
	SteamID      uint64 `json:"steam_id,string"`
	Weapon       string `json:"weapon"`
	Shots        int    `json:"shots"`
	SpottedShots int    `json:"spotted_shots,omitempty"` // mesh-gated
	Hits         int    `json:"hits,omitempty"`
}
