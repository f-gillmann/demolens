package parser

import (
	"github.com/f-gillmann/demolens/model"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/common"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/events"
)

// lifecycle appends one connect/disconnect/bot-takeover entry, gated to CT/T players
// so GOTV spectator churn stays out. Sorted by time at finalizeMatch for determinism.
func (st *parseState) lifecycle(kind string, pl *common.Player) {
	if pl == nil || pl.SteamID64 == 0 || sideString(pl.Team) == "" {
		return
	}
	st.match.MatchLifecycle = append(st.match.MatchLifecycle, model.LifecycleEvent{
		Type:             kind,
		SteamID:          pl.SteamID64,
		Name:             pl.Name,
		Round:            len(st.match.Rounds) + 1,
		TimeMicroseconds: st.parsed.CurrentTime().Microseconds(),
	})
}

func (st *parseState) onDisconnect(e events.PlayerDisconnected) {
	st.lifecycle("disconnect", e.Player)
}

func (st *parseState) onConnect(e events.PlayerConnect) {
	st.lifecycle("connect", e.Player)
}

func (st *parseState) onBotConnect(e events.BotConnect) {
	st.lifecycle("bot_connect", e.Player)
}

func (st *parseState) onBotTakenOver(e events.BotTakenOver) {
	st.lifecycle("bot_taken_over", e.Taker)
}
