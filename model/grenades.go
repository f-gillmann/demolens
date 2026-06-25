package model

// Grenades holds the typed per-round grenade buckets. molotovs[] folds both
// "molotov" and "incendiary". Order within a bucket is throw-time then thrower.
type Grenades struct {
	Flashes  []GrenadeFlash   `json:"flashes,omitempty"`
	HEs      []GrenadeHE      `json:"hes,omitempty"`
	Molotovs []GrenadeMolotov `json:"molotovs,omitempty"`
	Smokes   []GrenadeBasic   `json:"smokes,omitempty"`
	Decoys   []GrenadeBasic   `json:"decoys,omitempty"`
}

// GrenadeFlash is one thrown flashbang. expire == detonate (instant) so expire is
// dropped. No damage fields; flashes deal no HP damage.
type GrenadeFlash struct {
	GrenadeID                string   `json:"grenade_id"`
	Thrower                  uint64   `json:"thrower,string"`
	Side                     string   `json:"side,omitempty"`
	Type                     string   `json:"type"` // const "flash"
	ThrowTimeMicroseconds    int64    `json:"throw_time_microseconds"`
	DetonateTimeMicroseconds int64    `json:"detonate_time_microseconds,omitempty"`
	FlightMicroseconds       int64    `json:"flight_microseconds,omitempty"`
	ThrowPosition            Position `json:"throw_position"`
	DetonatePosition         Position `json:"detonate_position,omitempty"`

	EnemiesFlashed   int             `json:"enemies_flashed,omitempty"`
	TeammatesFlashed int             `json:"teammates_flashed,omitempty"`
	Flashed          []FlashedPlayer `json:"flashed,omitempty"`
}

// GrenadeHE is one thrown HE grenade. expire == detonate (instant) so expire is
// dropped. Damage fields are omitempty: absent when it hit nobody.
type GrenadeHE struct {
	GrenadeID                string   `json:"grenade_id"`
	Thrower                  uint64   `json:"thrower,string"`
	Side                     string   `json:"side,omitempty"`
	Type                     string   `json:"type"` // const "he"
	ThrowTimeMicroseconds    int64    `json:"throw_time_microseconds"`
	DetonateTimeMicroseconds int64    `json:"detonate_time_microseconds,omitempty"`
	FlightMicroseconds       int64    `json:"flight_microseconds,omitempty"`
	ThrowPosition            Position `json:"throw_position"`
	DetonatePosition         Position `json:"detonate_position,omitempty"`

	DamageDealt int             `json:"damage_dealt,omitempty"` // total HP damage to ENEMIES
	TeamDamage  int             `json:"team_damage,omitempty"`  // total HP damage to TEAMMATES
	Victims     []GrenadeVictim `json:"victims,omitempty"`
}

// GrenadeMolotov is one thrown molotov or incendiary. expire is distinct here
// (burn-out) so it is kept; burn duration = expire - detonate.
type GrenadeMolotov struct {
	GrenadeID                string   `json:"grenade_id"`
	Thrower                  uint64   `json:"thrower,string"`
	Side                     string   `json:"side,omitempty"`
	Type                     string   `json:"type"` // "molotov" or "incendiary"
	ThrowTimeMicroseconds    int64    `json:"throw_time_microseconds"`
	DetonateTimeMicroseconds int64    `json:"detonate_time_microseconds,omitempty"`
	ExpireTimeMicroseconds   int64    `json:"expire_time_microseconds,omitempty"`
	FlightMicroseconds       int64    `json:"flight_microseconds,omitempty"`
	ThrowPosition            Position `json:"throw_position"`
	DetonatePosition         Position `json:"detonate_position,omitempty"`

	DamageDealt int             `json:"damage_dealt,omitempty"` // total HP fire damage to ENEMIES
	TeamDamage  int             `json:"team_damage,omitempty"`  // total HP fire damage to TEAMMATES
	Victims     []GrenadeVictim `json:"victims,omitempty"`
	FireCells   []Position      `json:"fire_cells,omitempty"` // peak per-flame fire footprint, sorted by x then y then z
}

// GrenadeBasic is one thrown smoke or decoy. No outcome data exists for these types.
type GrenadeBasic struct {
	GrenadeID                string   `json:"grenade_id"`
	Thrower                  uint64   `json:"thrower,string"`
	Side                     string   `json:"side,omitempty"`
	Type                     string   `json:"type"` // "smoke" or "decoy"
	ThrowTimeMicroseconds    int64    `json:"throw_time_microseconds"`
	DetonateTimeMicroseconds int64    `json:"detonate_time_microseconds,omitempty"`
	ExpireTimeMicroseconds   int64    `json:"expire_time_microseconds,omitempty"`
	FlightMicroseconds       int64    `json:"flight_microseconds,omitempty"`
	ThrowPosition            Position `json:"throw_position"`
	DetonatePosition         Position `json:"detonate_position,omitempty"`
}

// GrenadeVictim is one player damaged by an HE detonation or molotov inferno.
// damage is set when the victim is an enemy, team_damage when a teammate; one or
// the other per entry.
type GrenadeVictim struct {
	SteamID    uint64 `json:"steam_id,string"`
	Side       string `json:"side"` // victim side CT/T at this round
	Damage     int    `json:"damage,omitempty"`
	TeamDamage int    `json:"team_damage,omitempty"`
}
