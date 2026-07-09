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
	GrenadeID        string   `json:"grenade_id"`
	Thrower          uint64   `json:"thrower,string"`
	Side             string   `json:"side,omitempty"`
	Type             string   `json:"kind"` // const "flash"
	ThrowMs          int64    `json:"throw_ms"`
	DetonateMs       int64    `json:"detonate_ms,omitempty"`
	FlightMs         int64    `json:"flight_ms,omitempty"`
	ThrowPosition    Position `json:"throw_position"`
	DetonatePosition Position `json:"detonate_position,omitempty"`

	EnemiesFlashed   int             `json:"enemies_flashed,omitempty"`
	TeammatesFlashed int             `json:"teammates_flashed,omitempty"`
	Flashed          []FlashedPlayer `json:"flashed,omitempty"`
}

// GrenadeHE is one thrown HE grenade. expire == detonate (instant) so expire is
// dropped. Damage fields are omitempty: absent when it hit nobody.
type GrenadeHE struct {
	GrenadeID        string   `json:"grenade_id"`
	Thrower          uint64   `json:"thrower,string"`
	Side             string   `json:"side,omitempty"`
	Type             string   `json:"kind"` // const "he"
	ThrowMs          int64    `json:"throw_ms"`
	DetonateMs       int64    `json:"detonate_ms,omitempty"`
	FlightMs         int64    `json:"flight_ms,omitempty"`
	ThrowPosition    Position `json:"throw_position"`
	DetonatePosition Position `json:"detonate_position,omitempty"`

	DamageDealt int             `json:"damage_dealt,omitempty"` // total HP damage to ENEMIES
	TeamDamage  int             `json:"team_damage,omitempty"`  // total HP damage to TEAMMATES
	Victims     []GrenadeVictim `json:"victims,omitempty"`
}

// GrenadeMolotov is one thrown molotov or incendiary. expire is distinct here
// (burn-out) so it is kept; burn duration = expire - detonate.
type GrenadeMolotov struct {
	GrenadeID        string   `json:"grenade_id"`
	Thrower          uint64   `json:"thrower,string"`
	Side             string   `json:"side,omitempty"`
	Type             string   `json:"kind"` // "molotov" or "incendiary"
	ThrowMs          int64    `json:"throw_ms"`
	DetonateMs       int64    `json:"detonate_ms,omitempty"`
	ExpireMs         int64    `json:"expire_ms,omitempty"`
	FlightMs         int64    `json:"flight_ms,omitempty"`
	ThrowPosition    Position `json:"throw_position"`
	DetonatePosition Position `json:"detonate_position,omitempty"`

	DamageDealt int             `json:"damage_dealt,omitempty"` // total HP fire damage to ENEMIES
	TeamDamage  int             `json:"team_damage,omitempty"`  // total HP fire damage to TEAMMATES
	Victims     []GrenadeVictim `json:"victims,omitempty"`
	FireCells   []Position      `json:"fire_cells,omitempty"` // peak per-flame fire footprint, sorted by x then y then z
}

// GrenadeBasic is one thrown smoke or decoy. No outcome data exists for these types.
type GrenadeBasic struct {
	GrenadeID        string   `json:"grenade_id"`
	Thrower          uint64   `json:"thrower,string"`
	Side             string   `json:"side,omitempty"`
	Type             string   `json:"kind"` // "smoke" or "decoy"
	ThrowMs          int64    `json:"throw_ms"`
	DetonateMs       int64    `json:"detonate_ms,omitempty"`
	ExpireMs         int64    `json:"expire_ms,omitempty"`
	FlightMs         int64    `json:"flight_ms,omitempty"`
	ThrowPosition    Position `json:"throw_position"`
	DetonatePosition Position `json:"detonate_position,omitempty"`

	// smokes only, and only when the grenade_paths stream is on: the cloud's
	// networked voxel occupancy over its lifetime. Absent when the demo does not
	// carry the voxel stream (older CS2 demos): render the fallback circle.
	Voxels *SmokeVoxels `json:"voxels,omitempty"`
}

// SmokeVoxels is a smoke cloud's volumetric occupancy on the 32x32x32 voxel
// grid the server networks per smoke: origin is the grid's world min corner
// (detonation position minus 192 per axis) and voxel (x,y,z) spans
// [origin + v*cell, origin + (v+1)*cell] per axis.
type SmokeVoxels struct {
	Origin  Position           `json:"origin"`  // grid min corner, world units
	Cell    float64            `json:"cell"`    // voxel edge length, game units (12)
	Samples []SmokeVoxelSample `json:"samples"` // occupancy over the cloud's lifetime; hold each shape until the next sample
}

// SmokeVoxelSample is one step of the cloud's occupancy track: every list is
// run-length encoded over the sorted linear voxel indices (x + y*32 + z*1024)
// as flat [start, len, start, len, ...] pairs, one run covering the len
// consecutive indices start..start+len-1. The first sample carries the full
// occupied set at the detonation keyframe; every later sample carries only
// the change against the previous sample's reconstructed set. Later samples
// land only when the shape changed since the last one (at least 1000ms
// apart), and sampling stops once the cloud starts fading.
type SmokeVoxelSample struct {
	TMs      int64 `json:"t_ms"`               // since round start, ms
	Occupied []int `json:"occupied,omitempty"` // first sample only: the full occupied set, as runs
	Add      []int `json:"add,omitempty"`      // later samples: voxels turned occupied since the previous sample, as runs
	Del      []int `json:"del,omitempty"`      // later samples: voxels cleared since the previous sample, as runs
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
