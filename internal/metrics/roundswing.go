package metrics

import (
	"math"

	"github.com/f-gillmann/demolens/v2/model"
)

// mpWinCT is the empirical CT round-win fraction by manpower, indexed
// [ct alive][t alive] on 1..5 (row 0 / col 0 unused). Source: public empirical
// CS win-rate analysis by players alive.
var mpWinCT = [6][6]float64{
	1: {1: 0.4303, 2: 0.1234, 3: 0.0288, 4: 0.0069, 5: 0.0022},
	2: {1: 0.7915, 2: 0.4399, 3: 0.1872, 4: 0.0683, 5: 0.0227},
	3: {1: 0.9420, 2: 0.7327, 3: 0.4551, 4: 0.2360, 5: 0.1061},
	4: {1: 0.9850, 2: 0.8933, 3: 0.7003, 4: 0.4698, 5: 0.2750},
	5: {1: 0.9967, 2: 0.9614, 3: 0.8582, 4: 0.6835, 5: 0.4873},
}

// econLogitPerTier shifts the win probability in logit space per buy-tier of
// economy difference. Measured from the round outcomes in our own CS2 demo corpus
// (round-start 5v5 win rate regressed on the buy-tier difference: -2 tiers -> 7%,
// even -> 49%, +2 -> 85%; weighted logit slope 0.88).
const econLogitPerTier = 0.88

// plantLogitShift lowers CT win probability once the bomb is planted, so post-plant
// retake kills are valued higher. Measured from our corpus (mean CT logit drop across
// post-plant states vs the same manpower unplanted).
const plantLogitShift = 0.76

// tier maps a round buy_type to an ordinal richness rank. Unknown or other tokens
// fall back to full_buy so a missing economy reads as neutral.
func tier(buyType string) int {
	switch buyType {
	case "eco":
		return 0
	case "semi_eco":
		return 1
	case "semi_buy":
		return 2
	default: // full_buy and anything unrecognized
		return 3
	}
}

func logit(p float64) float64   { return math.Log(p / (1 - p)) }
func sigmoid(x float64) float64 { return 1 / (1 + math.Exp(-x)) }

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// winProbCT is the CT-perspective win probability for a manpower state, shifted by
// the economy difference in logit space. econDiff is tier(CT) minus tier(T).
func winProbCT(aliveCT, aliveT, econDiff float64, planted bool) float64 {
	if aliveCT > 5 {
		aliveCT = 5
	}
	if aliveT > 5 {
		aliveT = 5
	}

	switch {
	case aliveCT <= 0 && aliveT <= 0:
		return 0.5
	case aliveCT <= 0:
		return 0.0
	case aliveT <= 0:
		return 1.0
	}

	base := mpWinCT[int(aliveCT)][int(aliveT)]
	l := logit(base) + econLogitPerTier*econDiff
	if planted {
		l -= plantLogitShift
	}
	return clamp(sigmoid(l), 0.02, 0.98)
}

// computeRoundSwing tallies each player's win-probability added (WPA) across the
// match and stamps every live-round kill with the post-death CT win probability
// and the swing it caused. The model is kills-only and, for a normal player kill,
// zero-sum per kill: the gain credited to the killing side equals the victim's
// loss. Non-player deaths (bomb/world/suicide) and team kills only penalize the
// victim, a documented non-zero-sum edge. Shape follows an established public
// esports win-probability-added model.
func computeRoundSwing(m *model.Match) {
	idx := playerIndex(m)

	toKills := func(b *model.SwingBreakdown) *float64 { return &b.Kills }
	toDamage := func(b *model.SwingBreakdown) *float64 { return &b.Damage }
	toFlash := func(b *model.SwingBreakdown) *float64 { return &b.Flash }
	toTrade := func(b *model.SwingBreakdown) *float64 { return &b.Trade }
	toDeaths := func(b *model.SwingBreakdown) *float64 { return &b.Deaths }

	for ri := range m.Rounds {
		r := &m.Rounds[ri]

		econDiff := float64(tier(r.Economy.CT.BuyType) - tier(r.Economy.T.BuyType))

		sideByID := make(map[uint64]string, len(r.Players))
		aliveCT, aliveT := 0, 0
		for _, rp := range r.Players {
			sideByID[rp.SteamID] = rp.Side
			switch rp.Side {
			case "CT":
				aliveCT++
			case "T":
				aliveT++
			}
		}

		// award credits a player and books both where the swing came from and whether
		// it was earned in a round their team won or lost. Splitting equally per side
		// keeps the result independent of map iteration order (so it stays deterministic).
		award := func(sid uint64, amount float64, pick func(*model.SwingBreakdown) *float64) {
			p := idx[sid]
			if p == nil {
				return
			}
			p.Stats.RoundSwing += amount
			b := &p.Stats.SwingBreakdown
			*pick(b) += amount
			if sideByID[sid] == r.WinnerSide {
				b.InWon += amount
			} else {
				b.InLost += amount
			}
		}

		// once the bomb is planted, the win-prob curve shifts toward T for the rest of
		// the round. The plant's own jump is left uncredited, the same way the round-start
		// baseline is: only kills and the end-of-round residual hand out swing.
		plantMs, hasPlant := int64(0), false
		if r.Bomb != nil && r.Bomb.PlantMs > 0 {
			plantMs, hasPlant = r.Bomb.PlantMs, true
		}

		r.WinProbCTStart = winProbCT(float64(aliveCT), float64(aliveT), econDiff, false)

		// live-round kills only; exit kills are post-round and carry no swing.
		for ki := range r.Kills {
			k := &r.Kills[ki]

			planted := hasPlant && k.TMs >= plantMs
			wpBefore := winProbCT(float64(aliveCT), float64(aliveT), econDiff, planted)
			switch k.VictimSide {
			case "CT":
				aliveCT--
			case "T":
				aliveT--
			}
			wpAfter := winProbCT(float64(aliveCT), float64(aliveT), econDiff, planted)
			wp := wpAfter
			k.WinProbCT = &wp
			deltaCT := wpAfter - wpBefore

			killerSide := k.KillerSide
			isPlayerKill := k.Kind == "player" && k.Killer != nil
			isTeamKill := isPlayerKill && killerSide == k.VictimSide

			if !isPlayerKill || isTeamKill {
				// no acting side to credit: dock only the victim's side loss.
				sideLoss := wpAfter - wpBefore
				if k.VictimSide == "CT" {
					sideLoss = wpBefore - wpAfter
				}
				award(k.Victim, -sideLoss, toDeaths)
				sw := math.Abs(deltaCT)
				k.Swing = &sw
				continue
			}

			gain := deltaCT
			if killerSide != "CT" {
				gain = -deltaCT
			}
			sw := gain
			k.Swing = &sw

			// credit split: killer 35, damage dealers 30 (by health-damage share),
			// flash assist 15, trade 20. absent roles carry no weight, so the present
			// weights are renormalized to hand out exactly the full gain.
			share := damageShare(r, k, killerSide)
			avenged := avengedTeammate(r, ki, killerSide)
			hasDamage := len(share) > 0
			hasFlash := k.FlashAssister != 0
			hasTrade := avenged != 0

			sumWeights := 35.0
			if hasDamage {
				sumWeights += 30
			}
			if hasFlash {
				sumWeights += 15
			}
			if hasTrade {
				sumWeights += 20
			}
			factor := gain / sumWeights

			award(k.KillerID(), 35*factor, toKills)
			if hasDamage {
				for a, s := range share {
					award(a, 30*factor*s, toDamage)
				}
			}
			if hasFlash {
				award(k.FlashAssister, 15*factor, toFlash)
			}
			if hasTrade {
				award(avenged, 20*factor, toTrade)
			}
			award(k.Victim, -gain, toDeaths)
		}
	}
}

// damageShare returns each attacker's fraction of the health damage dealt to this
// kill's victim up to the moment of death (the victim dies once per round, so this
// is that life). Only the killing side's damage counts, so friendly fire on the
// victim never earns kill credit. Empty when no such damage was recorded.
func damageShare(r *model.Round, k *model.RoundKill, killerSide string) map[uint64]float64 {
	sideByID := make(map[uint64]string, len(r.Players))
	for _, rp := range r.Players {
		sideByID[rp.SteamID] = rp.Side
	}
	byAttacker := map[uint64]int{}
	total := 0
	for _, d := range r.Damages {
		if d.Victim != k.Victim || d.Attacker == 0 || d.TMs > k.TMs {
			continue
		}
		if sideByID[d.Attacker] != killerSide {
			continue // only the killing side's damage earns kill credit
		}
		byAttacker[d.Attacker] += d.HealthDamage
		total += d.HealthDamage
	}
	if total == 0 {
		return nil
	}
	share := make(map[uint64]float64, len(byAttacker))
	for a, dmg := range byAttacker {
		share[a] = float64(dmg) / float64(total)
	}
	return share
}

// avengedTeammate returns the teammate this kill avenges: the victim of the most
// recent prior kill in the round where this kill's victim killed one of the
// killer's teammates inside the trade window. 0 when the kill is not a trade.
func avengedTeammate(r *model.Round, ki int, killerSide string) uint64 {
	k := r.Kills[ki]
	for j := ki - 1; j >= 0; j-- {
		prev := r.Kills[j]
		if k.TMs-prev.TMs > tradeWindowMs {
			continue
		}
		if prev.Killer != nil && *prev.Killer == k.Victim && prev.VictimSide == killerSide {
			return prev.Victim
		}
	}
	return 0
}
