package metrics

import "github.com/f-gillmann/demolens/model"

// squared distance (game units) under which a live teammate is close enough to
// count as in position for a trade. squared so we skip the sqrt.
const tradeProximitySq = 550.0 * 550.0

// how long after a death a kill still counts as a trade.
const tradeWindowMicros = 4_000_000

// precomputed view of one round's kills. shared by KAST and trade detection.
type roundIndex struct {
	round     model.Round
	killers   map[uint64]bool // got a kill this round
	assisters map[uint64]bool // got an assist
	died      map[uint64]bool
	deathTime map[uint64]int64  // victim to time of death
	killerOf  map[uint64]uint64 // victim to whoever killed them
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

// did a teammate kill the victim's killer inside the trade window? that's a trade.
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

type tradeCounts struct {
	killOpportunity, killAttempt, killSuccess    int // could-trade / tried / got it
	deathOpportunity, deathAttempt, deathSuccess int // was-tradeable / tried for / got traded
}

// the per-player trade funnel.
func tradeStats(m *model.Match) map[uint64]*tradeCounts {
	team := teamMap(m)
	stats := map[uint64]*tradeCounts{}
	get := func(id uint64) *tradeCounts {
		c := stats[id]
		if c == nil {
			c = &tradeCounts{}
			stats[id] = c
		}
		return c
	}

	for _, round := range m.Rounds {
		for _, death := range round.Kills {
			victim, killer, deathTime := death.Victim, death.Killer, death.TimeMicroseconds
			if victim == 0 || killer == 0 || team[victim] == "" {
				continue
			}

			var opp, att, succ bool
			for _, mate := range death.AlivePlayers {
				if mate.SteamID == victim || team[mate.SteamID] != team[victim] {
					continue // victim's living teammates only
				}

				near := distSq(mate.Position, death.KillerPosition) <= tradeProximitySq
				killed := killedWithin(round, mate.SteamID, killer, deathTime, tradeWindowMicros)
				damagedKiller := damagedWithin(round, mate.SteamID, killer, deathTime, tradeWindowMicros)

				// opportunity: close enough, or already trading shots with the killer
				if !near && !damagedKiller && !killed {
					continue
				}

				// attempt: shot at any enemy inside the window
				attempted := damagedAnyEnemy(round, mate.SteamID, team[mate.SteamID], team, deathTime, tradeWindowMicros)

				c := get(mate.SteamID)
				c.killOpportunity++
				opp = true
				if attempted {
					c.killAttempt++
					att = true
				}
				if killed {
					c.killSuccess++
					succ = true
				}
			}
			if opp {
				get(victim).deathOpportunity++
			}
			if att {
				get(victim).deathAttempt++
			}
			if succ {
				get(victim).deathSuccess++
			}
		}
	}
	return stats
}

func damagedWithin(r model.Round, attacker, victim uint64, after, window int64) bool {
	for _, d := range r.Damages {
		if d.Attacker == attacker && d.Victim == victim &&
			d.TimeMicroseconds >= after && d.TimeMicroseconds-after <= window {
			return true
		}
	}
	return false
}

func damagedAnyEnemy(r model.Round, attacker uint64, attackerTeam string, team map[uint64]string, after, window int64) bool {
	for _, d := range r.Damages {
		if d.Attacker != attacker || team[d.Victim] == "" || team[d.Victim] == attackerTeam {
			continue
		}
		if d.TimeMicroseconds >= after && d.TimeMicroseconds-after <= window {
			return true
		}
	}
	return false
}

func killedWithin(r model.Round, killer, victim uint64, after, window int64) bool {
	for _, k := range r.Kills {
		if k.Killer == killer && k.Victim == victim &&
			k.TimeMicroseconds >= after && k.TimeMicroseconds-after <= window {
			return true
		}
	}
	return false
}

func distSq(a, b model.Position) float64 {
	dx, dy, dz := a.X-b.X, a.Y-b.Y, a.Z-b.Z
	return dx*dx + dy*dy + dz*dz
}
