package metrics

import "github.com/f-gillmann/demolens/model"

// tradeProximitySq is the squared straight-line distance (game units) within
// which a surviving teammate counts as "in position to trade".
const tradeProximitySq = 550.0 * 550.0

type tradeCounts struct {
	killOpportunity, killAttempt, killSuccess    int // trade-kill funnel
	deathOpportunity, deathAttempt, deathSuccess int // traded-death funnel
}

// tradeStats computes the trade funnel per player.
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
			victim, killer, t := death.Victim, death.Killer, death.TimeMicroseconds
			if victim == 0 || killer == 0 || team[victim] == "" {
				continue
			}

			var opp, att, succ bool
			for _, mate := range death.AlivePlayers {
				if mate.SteamID == victim || team[mate.SteamID] != team[victim] {
					continue // only the victim's surviving teammates
				}

				near := distSq(mate.Position, death.KillerPosition) <= tradeProximitySq
				killed := killedWithin(round, mate.SteamID, killer, t, tradeWindowMicros)
				damagedKiller := damagedWithin(round, mate.SteamID, killer, t, tradeWindowMicros)

				// opportunity: in position, or already contesting the killer
				if !near && !damagedKiller && !killed {
					continue
				}

				// attempt: dealt damage to any enemy within the window
				attempted := damagedAnyEnemy(round, mate.SteamID, team[mate.SteamID], team, t, tradeWindowMicros)

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
