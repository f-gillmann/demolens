package model

import (
	"encoding/json"
	"math"
	"sort"
	"strconv"
)

func (l SteamIDList) MarshalJSON() ([]byte, error) {
	if len(l) == 0 {
		return []byte("[]"), nil
	}
	out := make([]byte, 0, len(l)*20)
	out = append(out, '[')
	for i, id := range l {
		if i > 0 {
			out = append(out, ',')
		}
		out = append(out, '"')
		out = strconv.AppendUint(out, id, 10)
		out = append(out, '"')
	}
	out = append(out, ']')
	return out, nil
}

// MarshalJSON emits the frame as a columnar tuple ordered by PositionFields, with
// the eight state booleans packed into the flags integer. steam_id is not a row field
// (streams.positions is an object keyed by it). Floats are rounded to 2dp (negative
// zero normalized) to match the rest of the export. Velocity is emitted as a 0 vector
// when unknown.
func (f PlayerFrame) MarshalJSON() ([]byte, error) {
	flags := 0
	if f.IsAlive {
		flags |= frameFlagAlive
	}
	if f.IsAirborne {
		flags |= frameFlagAirborne
	}
	if f.IsScoped {
		flags |= frameFlagScoped
	}
	if f.IsDucking {
		flags |= frameFlagDucking
	}
	if f.HasDefuseKit {
		flags |= frameFlagHasDefuseKit
	}
	if f.InBuyZone {
		flags |= frameFlagBuyZone
	}
	if f.IsWalking {
		flags |= frameFlagWalking
	}
	if f.InBombZone {
		flags |= frameFlagBombZone
	}

	var vx, vy, vz float64
	if f.Velocity != nil {
		vx, vy, vz = round2(f.Velocity.X), round2(f.Velocity.Y), round2(f.Velocity.Z)
	}

	row := []any{
		f.TMs,
		f.Side,
		round2(f.Position.X), round2(f.Position.Y), round2(f.Position.Z),
		vx, vy, vz,
		round2(f.Yaw), round2(f.Pitch),
		f.Health, f.Armor, f.Money,
		flags,
		f.ActiveWeapon,
		f.Place,
		round2(f.Stamina), round2(f.DuckAmount),
		f.HoldFrames,
	}
	return json.Marshal(row)
}

// round2 rounds f to 2 decimals and clears the negative-zero sign bit so the
// JSON shows 0 instead of -0. Output-only; never feeds a metric computation.
// must stay in sync with round2 in internal/parser/floats.go.
func round2(f float64) float64 {
	r := math.Round(f*100) / 100
	if r == 0 {
		r = 0
	}
	return r
}

// MarshalJSON rounds X/Y/Z to 2 decimals for output. Because this runs only at
// encode time, the in-memory full-precision values still feed all geometry and
// metric math; this rounds every exported position in one place.
func (p Position) MarshalJSON() ([]byte, error) {
	out := make([]byte, 0, 48)
	out = append(out, `{"x":`...)
	out = strconv.AppendFloat(out, round2(p.X), 'f', -1, 64)
	out = append(out, `,"y":`...)
	out = strconv.AppendFloat(out, round2(p.Y), 'f', -1, 64)
	out = append(out, `,"z":`...)
	out = strconv.AppendFloat(out, round2(p.Z), 'f', -1, 64)
	out = append(out, '}')
	return out, nil
}

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

// MarshalJSON emits the histogram as a fixed [k1,k2,k3,k4,k5] array instead of an
// object. The struct fields stay so aggregation code is unchanged.
func (m MultiKills) MarshalJSON() ([]byte, error) {
	return json.Marshal([5]int{m.K1, m.K2, m.K3, m.K4, m.K5})
}
