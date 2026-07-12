package parser

import (
	"strconv"
	"strings"

	dem "github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs"
)

// server-name substring to platform, checked in order
var sourcePatterns = []struct{ substr, source string }{
	{"faceit", "faceit"},
	{"valve", "valve"},
	{"esea", "esea"},
	{"esl", "esl"},
	{"fastcup", "fastcup"},
	{"esportal", "esportal"},
	{"pracc", "pracc"},
	{"gamersclub", "gamersclub"},
	{"gamers club", "gamersclub"},
	{"popflash", "popflash"},
	{"cevo", "cevo"},
	{"matchzy", "matchzy"},
	{"challengermode", "challengermode"},
	{"renown", "renown"},
	{"perfect world", "perfectworld"},
	{"完美世界", "perfectworld"},
	{"5eplay", "5eplay"},
}

// guessSource sniffs the origin platform out of the server name.
func guessSource(serverName string) string {
	name := strings.ToLower(serverName)
	for _, p := range sourcePatterns {
		if strings.Contains(name, p.substr) {
			return p.source
		}
	}
	return "unknown"
}

// gameMode reads the game_type/game_mode convars into a readable mode. Those
// convars are frequently absent on Valve matchmaking demos; when they are, it
// falls back to valveMatchmakingMode. Returns empty when neither can name the mode.
func gameMode(rules dem.GameRules) string {
	convars := rules.ConVars()
	gt, ok1 := atoi(convars["game_type"])
	gm, ok2 := atoi(convars["game_mode"])
	if !ok1 || !ok2 {
		return valveMatchmakingMode(rules)
	}

	switch {
	case gt == 0 && gm == 0:
		return "casual"
	case gt == 0 && gm == 1:
		return "competitive" // convars can't tell premier apart from this
	case gt == 0 && gm == 2:
		return "wingman"
	case gt == 1 && gm == 0:
		return "arms_race"
	case gt == 1 && gm == 1:
		return "demolition"
	case gt == 1 && gm == 2:
		return "deathmatch"
	default:
		return "custom"
	}
}

// valveMatchmakingMode names the mode from demo state when the game_type/game_mode
// convars are absent (common on Valve matchmaking/Premier demos). It reports only
// what real demos let us verify: the CCSGameRulesProxy m_bIsQueuedMatchmaking flag
// is the sole reliable Valve-matchmaking marker (true across every mm-*/Valve-server
// demo, false on ESL/third-party), and a queued 5v5 MR12 game (mp_maxrounds == 24)
// is competitive or premier, which the convar path also collapses to "competitive"
// (m_nQueuedMatchmakingMode is 0 on all samples, so it cannot split the two). Every
// other case (wingman, casual, deathmatch, non-Valve demos) stays empty rather than
// guess, since no sample demo exists to confirm it.
func valveMatchmakingMode(rules dem.GameRules) string {
	ent := rules.Entity()
	if ent == nil {
		return ""
	}
	v, ok := ent.PropertyValue("m_pGameRules.m_bIsQueuedMatchmaking")
	if !ok {
		return ""
	}
	queued, isBool := v.Any.(bool)
	if !isBool || !queued {
		return ""
	}
	if mr, ok := atoi(rules.ConVars()["mp_maxrounds"]); ok && mr == 24 {
		return "competitive"
	}
	return ""
}

// demoType tells GOTV (spectator) recordings from POV ones.
func demoType(isHltv bool, clientName string) string {
	name := strings.ToLower(clientName)
	if isHltv || strings.Contains(name, "sourcetv") || strings.Contains(name, "gotv") {
		return "gotv"
	}
	return "pov"
}

func atoi(s string) (int, bool) {
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, false
	}
	return n, true
}
