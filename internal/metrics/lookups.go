package metrics

import "github.com/f-gillmann/demolens/v2/model"

// SteamID to team id.
func teamMap(m *model.Match) map[uint64]string {
	teams := make(map[uint64]string, len(m.Players))
	for _, p := range m.Players {
		teams[p.SteamID] = p.Team
	}
	return teams
}

// SteamID to the match-level Player, by pointer so callers can mutate.
func playerIndex(m *model.Match) map[uint64]*model.Player {
	idx := make(map[uint64]*model.Player, len(m.Players))
	for i := range m.Players {
		idx[m.Players[i].SteamID] = &m.Players[i]
	}
	return idx
}

// SteamID to side for one round (people swap halves so this is per-round).
func sideMap(r model.Round) map[uint64]string {
	sides := make(map[uint64]string, len(r.Players))
	for _, rp := range r.Players {
		sides[rp.SteamID] = rp.Side
	}
	return sides
}

// per-round record for one player, nil if they weren't in.
func roundPlayer(r *model.Round, id uint64) *model.RoundPlayer {
	for i := range r.Players {
		if r.Players[i].SteamID == id {
			return &r.Players[i]
		}
	}
	return nil
}

// the other side.
func opposite(side string) string {
	if side == "CT" {
		return "T"
	}
	return "CT"
}
