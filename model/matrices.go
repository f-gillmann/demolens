package model

// FlashPair holds how often and how long a flasher blinded one victim.
type FlashPair struct {
	Flasher uint64 `json:"flasher,string"`
	Flashed uint64 `json:"flashed,string"`
	Count   int    `json:"count"`
	BlindMs int64  `json:"blind_ms"` // total blind time on this victim, ms
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
