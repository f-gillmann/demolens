package parser

import (
	"sort"
	"time"

	"github.com/f-gillmann/demolens/v2/model"
)

// sortStreams sorts the map-derived stream slices for deterministic output. shots
// are appended in event order already; positions, grenade_paths, inventory and
// ground-items are built by ranging maps and need a stable order. No-op when nil.
func sortStreams(s *model.RoundStreams, period time.Duration) {
	if s == nil {
		return
	}
	sort.SliceStable(s.Positions, func(i, j int) bool {
		a, b := s.Positions[i], s.Positions[j]
		if a.TMs != b.TMs {
			return a.TMs < b.TMs
		}
		return a.SteamID < b.SteamID
	})
	s.Positions = compressPositions(s.Positions, period)
	sort.SliceStable(s.GrenadePaths, func(i, j int) bool {
		return s.GrenadePaths[i].GrenadeID < s.GrenadePaths[j].GrenadeID
	})
	sort.SliceStable(s.Inventory, func(i, j int) bool {
		a, b := s.Inventory[i], s.Inventory[j]
		if a.SteamID != b.SteamID {
			return a.SteamID < b.SteamID
		}
		if a.TMs != b.TMs {
			return a.TMs < b.TMs
		}
		return a.Phase < b.Phase
	})
	sort.SliceStable(s.GroundItems, func(i, j int) bool {
		a, b := s.GroundItems[i], s.GroundItems[j]
		if a.DroppedAtMs != b.DroppedAtMs {
			return a.DroppedAtMs < b.DroppedAtMs
		}
		if a.Item != b.Item {
			return a.Item < b.Item
		}
		return a.LastOwner < b.LastOwner
	})
}

// compressPositions collapses runs of byte-identical consecutive per-player frames
// into a single frame carrying a hold count (RLE). Input MUST already be time-then-
// steam_id sorted (sortStreams does this just above). Within each player's frames a
// run of identical states is folded onto one kept frame whose HoldFrames counts the
// dropped samples; a time gap larger than ~1.5 sample periods (e.g. a disconnect)
// breaks the run even when the state is identical. Output is re-sorted by time then
// steam_id so it stays deterministic and consistent with the rest of the stream.
func compressPositions(frames []model.PlayerFrame, period time.Duration) []model.PlayerFrame {
	if len(frames) == 0 {
		return frames
	}

	// max gap that still counts as the immediate next sample of a run. ms, matching
	// the frame timestamps and the configured sample period.
	maxGap := int64(period/time.Millisecond) * 3 / 2

	// group by steam_id, preserving the incoming time order within each group.
	order := make([]uint64, 0)
	byPlayer := make(map[uint64][]model.PlayerFrame)
	for _, f := range frames {
		if _, ok := byPlayer[f.SteamID]; !ok {
			order = append(order, f.SteamID)
		}
		byPlayer[f.SteamID] = append(byPlayer[f.SteamID], f)
	}

	out := make([]model.PlayerFrame, 0, len(frames))
	for _, id := range order {
		group := byPlayer[id]
		kept := group[0]
		lastSeen := kept.TMs
		for i := 1; i < len(group); i++ {
			f := group[i]
			gap := f.TMs - lastSeen
			if gap > 0 && gap <= maxGap && sameFrameState(&f, &kept) {
				kept.HoldFrames++
				lastSeen = f.TMs
				continue
			}
			out = append(out, kept)
			kept = f
			lastSeen = f.TMs
		}
		out = append(out, kept)
	}

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].TMs != out[j].TMs {
			return out[i].TMs < out[j].TMs
		}
		return out[i].SteamID < out[j].SteamID
	})
	return out
}

// sameFrameState reports whether two player frames carry an identical state,
// ignoring T and HoldFrames (the only fields RLE is allowed to vary within a run).
// Velocity is a pointer so the struct can't be compared with ==.
// Floats are compared exactly: they are already rounded to 2dp at populate time, so
// identical static frames compare equal.
func sameFrameState(a, b *model.PlayerFrame) bool {
	if a.SteamID != b.SteamID || a.Side != b.Side {
		return false
	}
	if a.Position != b.Position {
		return false
	}
	if a.Yaw != b.Yaw || a.Pitch != b.Pitch {
		return false
	}
	if a.Health != b.Health || a.Armor != b.Armor || a.Money != b.Money {
		return false
	}
	if a.IsAlive != b.IsAlive || a.IsAirborne != b.IsAirborne || a.IsScoped != b.IsScoped ||
		a.IsDucking != b.IsDucking || a.HasDefuseKit != b.HasDefuseKit {
		return false
	}
	if a.ActiveWeapon != b.ActiveWeapon {
		return false
	}
	if a.IsWalking != b.IsWalking || a.InBuyZone != b.InBuyZone || a.InBombZone != b.InBombZone {
		return false
	}
	if a.Stamina != b.Stamina || a.DuckAmount != b.DuckAmount || a.Place != b.Place {
		return false
	}
	if (a.Velocity == nil) != (b.Velocity == nil) {
		return false
	}
	if a.Velocity != nil && *a.Velocity != *b.Velocity {
		return false
	}
	return true
}
