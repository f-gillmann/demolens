package parser

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"time"

	"github.com/f-gillmann/demolens/model"
	dem "github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/events"
)

// Options turns on the expensive per-frame captures. Zero value is the cheap path.
type Options struct {
	PlayerFrames bool        // sample player pos + state each frame
	Shots        bool        // per-shot shooter geometry
	GrenadePaths bool        // grenade trajectories + bounces
	MapsDir      string      // .tri mesh dir for TTD los. empty disables TTD
	Calibration  Calibration // aim-stat thresholds, zero fields fall back to defaults
}

const frameSamplePeriod = 250 * time.Millisecond

// longest pause between two shots that still counts as one spray
const sprayGap = 300 * time.Millisecond

// blind has to last at least this long to count as "fully flashed"
const flashFullyBlind = 1100 * time.Millisecond

func Parse(r io.Reader, opts Options) (_ *model.Match, err error) {
	hash := sha256.New()
	parsed := dem.NewParser(io.TeeReader(r, hash))
	defer func() {
		if closeErr := parsed.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}()

	match := &model.Match{}
	st := newParseState(parsed, opts, match)

	parsed.RegisterNetMessageHandler(st.onServerInfo)
	parsed.RegisterNetMessageHandler(st.onFileHeader)

	parsed.RegisterEventHandler(st.onTeamSideSwitch)
	parsed.RegisterEventHandler(st.onFreezetimeEnd)
	parsed.RegisterEventHandler(st.onRoundStart)
	parsed.RegisterEventHandler(st.onKill)
	parsed.RegisterEventHandler(st.onPlayerHurt)
	parsed.RegisterEventHandler(st.onWeaponFire)
	parsed.RegisterEventHandler(st.onGrenadeThrow)

	parsed.RegisterEventHandler(func(e events.FlashExplode) { st.detonate(e.GrenadeEntityID, st.grenadeEventPos(e.GrenadeEvent), true) })
	parsed.RegisterEventHandler(func(e events.HeExplode) { st.detonate(e.GrenadeEntityID, st.grenadeEventPos(e.GrenadeEvent), true) })
	parsed.RegisterEventHandler(func(e events.SmokeStart) { st.detonate(e.GrenadeEntityID, st.grenadeEventPos(e.GrenadeEvent), false) })
	parsed.RegisterEventHandler(func(e events.DecoyStart) { st.detonate(e.GrenadeEntityID, st.grenadeEventPos(e.GrenadeEvent), false) })
	parsed.RegisterEventHandler(func(e events.SmokeExpired) { st.expire(e.GrenadeEntityID) })
	parsed.RegisterEventHandler(func(e events.DecoyExpired) { st.expire(e.GrenadeEntityID) })

	parsed.RegisterEventHandler(st.onInfernoStart)
	parsed.RegisterEventHandler(st.onInfernoPoll)
	parsed.RegisterEventHandler(st.onInfernoExpired)

	parsed.RegisterEventHandler(st.onSmokeStartTrack)
	parsed.RegisterEventHandler(st.onSmokeExpiredTrack)

	parsed.RegisterEventHandler(st.onPlayerFlashed)

	parsed.RegisterEventHandler(st.onBombPlanted)
	parsed.RegisterEventHandler(st.onBombDefused)
	parsed.RegisterEventHandler(st.onBombExplode)

	parsed.RegisterEventHandler(st.onGrenadeDestroy)
	parsed.RegisterEventHandler(st.onGrenadeBounce)

	parsed.RegisterEventHandler(st.onPlayerFrames)
	parsed.RegisterEventHandler(st.onSpeedSample)
	parsed.RegisterEventHandler(st.onSighting)

	parsed.RegisterEventHandler(st.onRoundEnd)

	// CS2 GOTV demos tend to end on a truncated final fragment. gameplay is all
	// there by then, so swallow ErrUnexpectedEndOfDemo.
	if err := parsed.ParseToEnd(); err != nil && !errors.Is(err, dem.ErrUnexpectedEndOfDemo) {
		return nil, err
	}

	// append the last round only if it ended cleanly. a round still live at demo
	// end got cut off, so drop it (same as the old behaviour).
	if !st.roundLive {
		st.finalize()
	}

	st.finalizeMatch()

	match.FileHash = hex.EncodeToString(hash.Sum(nil))
	return match, nil
}
