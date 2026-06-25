package model

// RoundStreams is the opt-in heavy detail for a round. Presence of each sub-array is
// governed by meta.output.streams; a nil RoundStreams means no stream was on.
type RoundStreams struct {
	Positions      []PlayerFrame     `json:"positions,omitempty"`
	Shots          []Shot            `json:"shots,omitempty"`
	GrenadePaths   []GrenadePath     `json:"grenade_paths,omitempty"`
	Inventory      []InventoryChange `json:"inventory,omitempty"`
	DroppedWeapons []DroppedWeapon   `json:"dropped_weapons,omitempty"`
}

// GrenadePath is one grenade's flight trajectory plus bounce points. It joins back
// to round.grenades.<bucket>[].grenade_id via grenade_id.
type GrenadePath struct {
	GrenadeID string     `json:"grenade_id"`
	Path      []Position `json:"path,omitempty"`    // sampled in-flight positions
	Bounces   []Position `json:"bounces,omitempty"` // wall/floor bounce points
}

// InventoryChange is one snapshot at a phase where inventory actually changed: a
// change log, not a per-tick dump. Freeze-end is not here; that is the core loadout.
type InventoryChange struct {
	SteamID          uint64          `json:"steam_id,string"`
	Side             string          `json:"side,omitempty"`
	Phase            string          `json:"phase"` // pickup / buy / bomb_plant / round_end
	TimeMicroseconds int64           `json:"time_microseconds,omitempty"`
	Health           int             `json:"health,omitempty"`
	Armor            int             `json:"armor,omitempty"`
	HasHelmet        bool            `json:"has_helmet,omitempty"`
	HasDefuseKit     bool            `json:"has_defuse_kit,omitempty"`
	Money            int             `json:"money,omitempty"`
	EquipmentValue   int             `json:"equipment_value,omitempty"`
	ActiveWeapon     string          `json:"active_weapon,omitempty"`
	Weapons          []LoadoutWeapon `json:"weapons,omitempty"`
	Grenades         []LoadoutItem   `json:"grenades,omitempty"`
	Equipment        []LoadoutItem   `json:"equipment,omitempty"`
}

// DroppedWeapon is one gun-on-the-ground stint: it lies at Position from
// dropped_at until picked_up_at (absent = still down at round end). last_owner is
// the holder who dropped it; on_death marks a drop forced by that owner dying.
type DroppedWeapon struct {
	Weapon                 string    `json:"weapon"`
	Class                  string    `json:"class,omitempty"`
	Position               *Position `json:"position"`                            // where it lies (static; rounded via toPosition)
	DroppedAtMicroseconds  int64     `json:"dropped_at_microseconds"`             // since round start
	PickedUpAtMicroseconds int64     `json:"picked_up_at_microseconds,omitempty"` // absent = still on ground at round end
	LastOwner              uint64    `json:"last_owner,omitempty,string"`         // who dropped it
	PickedUpBy             uint64    `json:"picked_up_by,omitempty,string"`
	OnDeath                bool      `json:"on_death,omitempty"` // last owner was dead when it dropped
	AmmoMagazine           int       `json:"ammo_magazine,omitempty"`
	AmmoReserve            int       `json:"ammo_reserve,omitempty"`
}
