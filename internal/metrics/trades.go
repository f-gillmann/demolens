package metrics

import (
	"sort"

	"github.com/f-gillmann/demolens/v2/model"
)

// squared distance (game units) under which a live teammate is close enough to
// count as in position for a trade. squared so we skip the sqrt.
const tradeProximitySq = 550.0 * 550.0

// how long after a death a kill still counts as a trade, ms.
const tradeWindowMs = 4_000

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
		if kill.KillerID() != 0 {
			idx.killers[kill.KillerID()] = true
		}
		if kill.Assister != 0 {
			idx.assisters[kill.Assister] = true
		}
		if kill.Victim != 0 {
			idx.died[kill.Victim] = true
			idx.deathTime[kill.Victim] = kill.TMs
			idx.killerOf[kill.Victim] = kill.KillerID()
		}
	}
	return idx
}

// did a teammate kill the victim's killer inside the trade window? that's a trade.
func (idx roundIndex) traded(victim uint64, team map[uint64]string) bool {
	return idx.tradedBy(victim, team) != 0
}

// tradedBy returns the teammate who actually traded this death: the earliest
// same-team kill of the victim's killer inside the trade window. 0 if untraded.
func (idx roundIndex) tradedBy(victim uint64, team map[uint64]string) uint64 {
	killer := idx.killerOf[victim]
	if killer == 0 || team[victim] == "" {
		return 0
	}

	deathTime := idx.deathTime[victim]
	var avenger uint64
	var avengerTime int64
	for _, kill := range idx.round.Kills {
		if kill.Victim != killer {
			continue
		}
		if kill.TMs < deathTime || kill.TMs-deathTime > tradeWindowMs {
			continue
		}
		if team[kill.KillerID()] != team[victim] {
			continue
		}
		if avenger == 0 || kill.TMs < avengerTime {
			avenger = kill.KillerID()
			avengerTime = kill.TMs
		}
	}
	return avenger
}

type tradeCounts struct {
	killOpportunity, killAttempt, killSuccess    int // could-trade / tried / got it
	deathOpportunity, deathAttempt, deathSuccess int // was-tradeable / tried for / got traded
}

// applyTo copies the trade tallies onto the player.
func (tc *tradeCounts) applyTo(p *model.Player) {
	p.TradeKillOpportunities = tc.killOpportunity
	p.TradeKillAttempts = tc.killAttempt
	p.TradeKills = tc.killSuccess
	p.TradedDeathOpportunities = tc.deathOpportunity
	p.TradedDeathAttempts = tc.deathAttempt
	p.TradedDeaths = tc.deathSuccess
}

// the per-player trade funnel.
func tradeStats(m *model.Match) map[uint64]*tradeCounts {
	team := teamMap(m)
	stats := map[uint64]*tradeCounts{}
	getOrCreate := func(id uint64) *tradeCounts {
		c := stats[id]
		if c == nil {
			c = &tradeCounts{}
			stats[id] = c
		}
		return c
	}

	for _, round := range m.Rounds {
		for _, death := range round.Kills {
			victim, killer, deathTime := death.Victim, death.KillerID(), death.TMs
			if victim == 0 || killer == 0 || team[victim] == "" {
				continue
			}

			var opportunity, attempt, success bool
			for _, mate := range death.AlivePlayers {
				if mate.SteamID == victim || team[mate.SteamID] != team[victim] {
					continue // victim's living teammates only
				}

				// opportunity: close enough, or already trading shots with the killer
				near, killed, damagedKiller := tradeReach(round, death, mate)
				if !near && !damagedKiller && !killed {
					continue
				}

				// attempt: shot at any enemy inside the window
				attempted := damagedAnyEnemy(round, mate.SteamID, team[mate.SteamID], team, deathTime, tradeWindowMs)

				c := getOrCreate(mate.SteamID)
				c.killOpportunity++
				opportunity = true
				if attempted {
					c.killAttempt++
					attempt = true
				}
				if killed {
					c.killSuccess++
					success = true
				}
			}

			if opportunity {
				getOrCreate(victim).deathOpportunity++
			}
			if attempt {
				getOrCreate(victim).deathAttempt++
			}
			if success {
				getOrCreate(victim).deathSuccess++
			}
		}
	}
	return stats
}

// possibleTraders lists the victim's living teammates who could have traded this
// death: close enough, already damaging the killer, or who killed the killer.
// Mirrors the tradeStats() opportunity gate for a single kill, sorted by steam_id.
func possibleTraders(r model.Round, death model.RoundKill, team map[uint64]string) []uint64 {
	victim, killer := death.Victim, death.KillerID()
	if victim == 0 || killer == 0 || team[victim] == "" {
		return nil
	}

	var out []uint64
	for _, mate := range death.AlivePlayers {
		if mate.SteamID == victim || team[mate.SteamID] != team[victim] {
			continue // victim's living teammates only
		}

		near, killed, damagedKiller := tradeReach(r, death, mate)
		if !near && !damagedKiller && !killed {
			continue
		}
		out = append(out, mate.SteamID)
	}

	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// tradeReach reports a living teammate's relation to a death, all inside the
// trade window: near the kill spot, killed the killer, or already damaging the
// killer. The trade-opportunity gate is "any of the three".
func tradeReach(r model.Round, death model.RoundKill, mate model.AlivePlayer) (near, killed, damagedKiller bool) {
	// non-player kills (bomb/world/suicide) carry no killer position, so there is no
	// spot to be "near"; the killed/damaged trade signals still apply.
	if death.KillerPosition != nil {
		near = distSq(mate.Position, *death.KillerPosition) <= tradeProximitySq
	}
	killed = killedWithin(r, mate.SteamID, death.KillerID(), death.TMs, tradeWindowMs)
	damagedKiller = damagedWithin(r, mate.SteamID, death.KillerID(), death.TMs, tradeWindowMs)
	return
}

func damagedWithin(r model.Round, attacker, victim uint64, after, window int64) bool {
	for _, d := range r.Damages {
		if d.Attacker == attacker && d.Victim == victim &&
			d.TMs >= after && d.TMs-after <= window {
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
		if d.TMs >= after && d.TMs-after <= window {
			return true
		}
	}
	return false
}

func killedWithin(r model.Round, killer, victim uint64, after, window int64) bool {
	for _, k := range r.Kills {
		if k.KillerID() == killer && k.Victim == victim &&
			k.TMs >= after && k.TMs-after <= window {
			return true
		}
	}
	return false
}

func distSq(a, b model.Position) float64 {
	dx, dy, dz := a.X-b.X, a.Y-b.Y, a.Z-b.Z
	return dx*dx + dy*dy + dz*dz
}
