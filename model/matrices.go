package model

import "encoding/json"

// FlashPair holds how often and how long a flasher blinded one victim.
type FlashPair struct {
	Flasher           uint64 `json:"flasher,string"`
	Flashed           uint64 `json:"flashed,string"`
	Count             int    `json:"count"`
	BlindMicroseconds int64  `json:"blind_microseconds"`
}

// MultiKills counts rounds in which a player got exactly n kills.
type MultiKills struct {
	K1 int `json:"k1"`
	K2 int `json:"k2"`
	K3 int `json:"k3"`
	K4 int `json:"k4"`
	K5 int `json:"k5"`
}

// MarshalJSON emits the histogram as a fixed [k1,k2,k3,k4,k5] array instead of an
// object. The struct fields stay so aggregation code is unchanged.
func (m MultiKills) MarshalJSON() ([]byte, error) {
	return json.Marshal([5]int{m.K1, m.K2, m.K3, m.K4, m.K5})
}

// WeaponStat is a player's kill/damage breakdown for a single weapon.
type WeaponStat struct {
	Kills     int `json:"kills"`
	Headshots int `json:"headshots"`
	Damage    int `json:"damage"`
}
