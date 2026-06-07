package metrics

import "github.com/f-gillmann/demolens/model"

// tradeWindowMicros is the window after a death can count as a trade.
const tradeWindowMicros = 4_000_000

// teamMap maps each player's SteamID to their team id.
func teamMap(m *model.Match) map[uint64]string {
	teams := make(map[uint64]string, len(m.Players))
	for _, p := range m.Players {
		teams[p.SteamID] = p.Team
	}
	return teams
}

// roundIndex is a precomputed lookup over a single round's kills, used by the
// per-round KAST computation.
type roundIndex struct {
	round     model.Round
	killers   map[uint64]bool // got >= 1 kill this round
	assisters map[uint64]bool // got >= 1 assist this round
	died      map[uint64]bool
	deathTime map[uint64]int64  // each victim's time of death
	killerOf  map[uint64]uint64 // each victim's killer
}

func newRoundIndex(round model.Round) roundIndex {
	idx := roundIndex{
		round:     round,
		killers:   map[uint64]bool{},
		assisters: map[uint64]bool{},
		died:      map[uint64]bool{},
		deathTime: map[uint64]int64{},
		killerOf:  map[uint64]uint64{},
	}
	for _, kill := range round.Kills {
		if kill.Killer != 0 {
			idx.killers[kill.Killer] = true
		}
		if kill.Assister != 0 {
			idx.assisters[kill.Assister] = true
		}
		if kill.Victim != 0 {
			idx.died[kill.Victim] = true
			idx.deathTime[kill.Victim] = kill.TimeMicroseconds
			idx.killerOf[kill.Victim] = kill.Killer
		}
	}
	return idx
}

// traded reports whether the victim's killer was killed by a teammate of the
// victim within the trade window after the victim's death.
func (idx roundIndex) traded(victim uint64, team map[uint64]string) bool {
	killer := idx.killerOf[victim]
	if killer == 0 || team[victim] == "" {
		return false
	}

	deathTime := idx.deathTime[victim]
	for _, kill := range idx.round.Kills {
		if kill.Victim != killer {
			continue
		}
		if kill.TimeMicroseconds < deathTime || kill.TimeMicroseconds-deathTime > tradeWindowMicros {
			continue
		}
		if team[kill.Killer] == team[victim] {
			return true
		}
	}
	return false
}
