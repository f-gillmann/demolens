package model

// Loadout is the freeze-time-end inventory snapshot for one round-player, grouped
// and counted. No ammo or zoom state; those change per shot, not snapshot facts.
type Loadout struct {
	Weapons    []LoadoutWeapon `json:"weapons,omitempty"`
	Grenades   []LoadoutItem   `json:"grenades,omitempty"`
	Equipment  []LoadoutItem   `json:"equipment,omitempty"`
	TotalValue int             `json:"total_value"`
}

// LoadoutWeapon is one grouped gun in a loadout or inventory change.
type LoadoutWeapon struct {
	Name          string `json:"name"`
	Class         string `json:"class"` // pistol / smg / heavy / rifle
	Count         int    `json:"count"`
	Value         int    `json:"value"`                           // 0 for the free spawn pistol
	OriginalOwner uint64 `json:"original_owner,omitempty,string"` // raw m_OriginalOwnerXuid Low+High
	PrevOwner     uint64 `json:"prev_owner,omitempty,string"`     // raw m_hPrevOwner
}

// LoadoutItem is one grouped grenade or equipment item in a loadout. value is 0 for
// free items such as the default knife.
type LoadoutItem struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
	Value int    `json:"value"`
}
