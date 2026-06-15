// Package demosource detects a demo's provenance (source, game mode, demo type) from header and convar data.
package demosource

import (
	"strconv"
	"strings"
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
func GuessSource(serverName string) string {
	name := strings.ToLower(serverName)
	for _, p := range sourcePatterns {
		if strings.Contains(name, p.substr) {
			return p.source
		}
	}
	return "unknown"
}

// gameMode reads the game_type/game_mode convars into a readable mode. Returns
// empty when those convars are missing, which is normal on third-party servers.
func GameMode(convars map[string]string) string {
	gt, ok1 := atoi(convars["game_type"])
	gm, ok2 := atoi(convars["game_mode"])
	if !ok1 || !ok2 {
		return ""
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

// demoType tells GOTV (spectator) recordings from POV ones.
func DemoType(isHltv bool, clientName string) string {
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
