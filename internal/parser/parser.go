package parser

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"sort"
	"strconv"
	"time"

	"github.com/f-gillmann/demolens/model"
	dem "github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/events"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/msg"
)

func Parse(r io.Reader) (_ *model.Match, err error) {
	hash := sha256.New()
	parsed := dem.NewParser(io.TeeReader(r, hash))
	defer func() {
		if closeErr := parsed.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}()

	match := &model.Match{}
	players := map[uint64]*model.Player{}

	var roundStart time.Duration
	var pending *model.Round
	var winnerReps []uint64 // winning-side representative steamid per round

	track := func(id uint64, name string) *model.Player {
		pl, ok := players[id]
		if !ok {
			pl = &model.Player{SteamID: id, Name: name}
			players[id] = pl
		}
		if name != "" {
			pl.Name = name
		}
		return pl
	}

	parsed.RegisterNetMessageHandler(func(info *msg.CSVCMsg_ServerInfo) {
		match.Meta.MapName = info.GetMapName()
		match.Meta.IsHltv = info.GetIsHltv()
		match.Meta.IsDedicatedServer = info.GetIsDedicated()
	})

	parsed.RegisterNetMessageHandler(func(header *msg.CDemoFileHeader) {
		match.Meta.BuildNum = strconv.Itoa(int(header.GetPatchVersion()))
	})

	// Freeze time end = buys done, round goes live: open a new round record.
	parsed.RegisterEventHandler(func(events.RoundFreezetimeEnd) {
		gs := parsed.GameState()
		if gs.IsWarmupPeriod() {
			return
		}
		roundStart = parsed.CurrentTime()
		pending = &model.Round{
			Number:  len(match.Rounds) + 1,
			Economy: roundEconomy(gs),
		}
		captureTeams(gs, players)
	})

	parsed.RegisterEventHandler(func(kill events.Kill) {
		if parsed.GameState().IsWarmupPeriod() {
			return
		}

		if kill.Killer != nil && kill.Killer.SteamID64 != 0 {
			pl := track(kill.Killer.SteamID64, kill.Killer.Name)
			pl.Kills++
			if kill.IsHeadshot {
				pl.Headshots++
			}
		}

		// Deaths
		if kill.Victim != nil && kill.Victim.SteamID64 != 0 {
			track(kill.Victim.SteamID64, kill.Victim.Name).Deaths++
		}

		// Assists + Flash Assists
		if kill.Assister != nil && kill.Assister.SteamID64 != 0 {
			if kill.AssistedFlash {
				track(kill.Assister.SteamID64, kill.Assister.Name).FlashAssists++
			} else {
				track(kill.Assister.SteamID64, kill.Assister.Name).Assists++
			}
		}

		if pending != nil {
			rk := roundKill(kill, parsed.CurrentTime()-roundStart)
			rk.AlivePlayers = aliveSnapshot(parsed.GameState())
			pending.Kills = append(pending.Kills, rk)
		}
	})

	parsed.RegisterEventHandler(func(hurt events.PlayerHurt) {
		if parsed.GameState().IsWarmupPeriod() {
			return
		}
		if hurt.Attacker == nil || hurt.Attacker.SteamID64 == 0 {
			return
		}

		if hurt.Player != nil && hurt.Player.Team == hurt.Attacker.Team {
			track(hurt.Attacker.SteamID64, hurt.Attacker.Name).TeamDamage += hurt.HealthDamageTaken
		} else {
			track(hurt.Attacker.SteamID64, hurt.Attacker.Name).Damage += hurt.HealthDamageTaken
		}

		if pending != nil {
			pending.Damages = append(pending.Damages, damageEvent(hurt, parsed.CurrentTime()-roundStart))
		}
	})

	parsed.RegisterEventHandler(func(end events.RoundEnd) {
		gs := parsed.GameState()
		if gs.IsWarmupPeriod() || pending == nil {
			return
		}
		pending.WinnerSide = sideString(end.Winner)
		pending.Reason = reasonString(end.Reason)
		match.Rounds = append(match.Rounds, *pending)
		pending = nil

		// Remember a player on the winning side; their team tells us which team (A/B) won this round.
		var rep uint64
		if members := gs.Participants().TeamMembers(end.Winner); len(members) > 0 {
			rep = members[0].SteamID64
		}
		winnerReps = append(winnerReps, rep)
	})

	// CS2 GOTV demos often end with a cut final fragment.
	// Since the gameplay is fully parsed by then, we tolerate the error.
	if err := parsed.ParseToEnd(); err != nil && !errors.Is(err, dem.ErrUnexpectedEndOfDemo) {
		return nil, err
	}

	gs := parsed.GameState()
	match.Meta.TickRate = parsed.TickRate()
	match.Meta.DurationMicroseconds = parsed.CurrentTime().Microseconds()
	match.Meta.TotalRounds = gs.TotalRoundsPlayed()
	match.Meta.Score = model.Score{
		TeamA: gs.TeamCounterTerrorists().Score(),
		TeamB: gs.TeamTerrorists().Score(),
	}

	for _, pl := range players {
		match.Players = append(match.Players, *pl)
	}
	for i, rep := range winnerReps {
		if pl, ok := players[rep]; ok && i < len(match.Rounds) {
			match.Rounds[i].WinnerTeam = pl.Team
		}
	}
	sort.Slice(match.Players, func(i, j int) bool {
		return match.Players[i].SteamID < match.Players[j].SteamID
	})

	match.FileHash = hex.EncodeToString(hash.Sum(nil))
	return match, nil
}
