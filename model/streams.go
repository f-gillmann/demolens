package model

import (
	"encoding/json"
	"sort"
	"strconv"
)

// RoundStreams is the opt-in heavy detail for a round. Presence of each sub-array is
// governed by meta.output.streams; a nil RoundStreams means no stream was on.
type RoundStreams struct {
	Positions    PositionStream    `json:"positions,omitempty"`
	Shots        []Shot            `json:"shots,omitempty"`
	GrenadePaths []GrenadePath     `json:"grenade_paths,omitempty"`
	Inventory    []InventoryChange `json:"inventory,omitempty"`
	GroundItems  []DroppedItem     `json:"ground_items,omitempty"`
}

// PositionStream is the per-round positions sample set. In memory it is a flat,
// time-then-steam_id sorted slice of frames (so the parser can append, sort and RLE-
// compress it), but it marshals as an OBJECT keyed by steam_id string, each value the
// player's time-ordered array of columnar tuples (PositionFields order, no steam_id
// per row). So a 17-char steam_id appears once per player per round, not once per
// sample. Keys are sorted ascending for deterministic output.
type PositionStream []PlayerFrame

func (ps PositionStream) MarshalJSON() ([]byte, error) {
	if len(ps) == 0 {
		return []byte("{}"), nil
	}
	// group frames by steam_id, preserving the incoming (time-sorted) order per player.
	byID := map[uint64][]PlayerFrame{}
	order := make([]uint64, 0)
	for _, f := range ps {
		if _, ok := byID[f.SteamID]; !ok {
			order = append(order, f.SteamID)
		}
		byID[f.SteamID] = append(byID[f.SteamID], f)
	}
	sort.Slice(order, func(i, j int) bool { return order[i] < order[j] })

	out := make([]byte, 0, len(ps)*48)
	out = append(out, '{')
	for i, id := range order {
		if i > 0 {
			out = append(out, ',')
		}
		out = append(out, '"')
		out = strconv.AppendUint(out, id, 10)
		out = append(out, '"', ':')
		rows, err := json.Marshal(byID[id])
		if err != nil {
			return nil, err
		}
		out = append(out, rows...)
	}
	out = append(out, '}')
	return out, nil
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
	SteamID        uint64          `json:"steam_id,string"`
	Side           string          `json:"side,omitempty"`
	Phase          string          `json:"phase"`          // pickup / buy / bomb_plant / round_end
	TMs            int64           `json:"t_ms,omitempty"` // since round start, ms
	Health         int             `json:"health,omitempty"`
	Armor          int             `json:"armor,omitempty"`
	HasHelmet      bool            `json:"has_helmet,omitempty"`
	HasDefuseKit   bool            `json:"has_defuse_kit,omitempty"`
	Money          int             `json:"money,omitempty"`
	EquipmentValue int             `json:"equipment_value,omitempty"`
	ActiveWeapon   string          `json:"active_weapon,omitempty"`
	Weapons        []LoadoutWeapon `json:"weapons,omitempty"`
	Grenades       []LoadoutItem   `json:"grenades,omitempty"`
	Equipment      []LoadoutItem   `json:"equipment,omitempty"`
}

// GroundItemPositionFields is the declared per-row column order of a
// ground_items[].positions tuple, mirrored at meta.output.ground_item_positions_fields
// so the rows are self-describing. Ground items carry no velocity/state, so the tuple
// is just position over time plus the RLE hold count.
var GroundItemPositionFields = []string{"t_ms", "x", "y", "z", "hold_frames"}

// GroundItemFrame is one ground-position sample of a dropped entity. It marshals as a
// compact columnar tuple ordered by GroundItemPositionFields, not an object. HoldFrames
// is set by the finalize-time RLE pass: the number of additional sample periods this
// exact position persisted (so a resting item collapses to a single tuple).
type GroundItemFrame struct {
	TMs        int64
	Position   Position
	HoldFrames int
}

// MarshalJSON emits the frame as a columnar tuple ordered by GroundItemPositionFields.
// Floats are rounded to 2dp (negative zero normalized) to match the rest of the export.
func (f GroundItemFrame) MarshalJSON() ([]byte, error) {
	row := []any{
		f.TMs,
		round2(f.Position.X), round2(f.Position.Y), round2(f.Position.Z),
		f.HoldFrames,
	}
	return json.Marshal(row)
}

// DroppedItem is one item-on-the-ground stint: it lies on the ground from
// dropped_at_ms until picked_up_at_ms (absent = still down at round end). positions is
// the time-ordered, RLE-compressed ground-position track (a static item is one tuple
// with a large hold_frames; a nade-shoved or re-dropped item shows each new position).
// last_owner is the holder who dropped it; on_death marks a drop forced by that owner
// dying. The loose c4 is tracked here too (item "C4", class "c4"); the carried bomb
// rides the positions stream and the planted bomb is round.bomb.plant_position.
type DroppedItem struct {
	Item         string            `json:"item"`
	Class        string            `json:"class,omitempty"`
	Positions    []GroundItemFrame `json:"positions,omitempty"`         // RLE ground-position track (GroundItemPositionFields order)
	DroppedAtMs  int64             `json:"dropped_at_ms"`               // since round start, ms
	IsInitial    bool              `json:"is_initial,omitempty"`        // spawn/round-start state (dropped_at_ms == 0), not a real mid-round drop
	PickedUpAtMs int64             `json:"picked_up_at_ms,omitempty"`   // absent = still on ground at round end
	LastOwner    uint64            `json:"last_owner,omitempty,string"` // who dropped it
	PickedUpBy   uint64            `json:"picked_up_by,omitempty,string"`
	OnDeath      bool              `json:"on_death,omitempty"` // last owner was dead when it dropped
	AmmoMagazine int               `json:"ammo_magazine,omitempty"`
	AmmoReserve  int               `json:"ammo_reserve,omitempty"`
}
