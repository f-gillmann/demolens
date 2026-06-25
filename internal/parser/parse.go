package parser

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"sort"
	"time"

	"github.com/f-gillmann/demolens/model"
	dem "github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/events"
)

// Options turns on the expensive per-frame captures. Zero value is the cheap path
// (no heavy streams, tier "core"). A non-empty Tier preset flips the stream
// booleans on resolve; individual booleans set explicitly are not overridden.
type Options struct {
	PlayerFrames   bool        // "positions" stream: sample player pos + state each frame
	Shots          bool        // "shots" stream: per-shot shooter geometry
	GrenadePaths   bool        // "grenade_paths" stream: grenade trajectories + bounces
	Inventory      bool        // "inventory" stream: mid-round inventory change log
	DroppedWeapons bool        // "dropped_weapons" stream: world weapons at phase boundaries
	Tier           string      // optional preset: core / detail / full. empty means full.
	MapsDir        string      // .tri mesh dir for TTD los. empty disables TTD
	Calibration    Calibration // aim-stat thresholds, zero fields fall back to defaults
}

// stream names exactly as they appear in meta.output.streams, sorted.
const (
	streamPositions      = "positions"
	streamShots          = "shots"
	streamGrenadePaths   = "grenade_paths"
	streamInventory      = "inventory"
	streamDroppedWeapons = "dropped_weapons"
)

// positionsSampleHz is the positions-stream sample rate, derived from the frame
// sample period so it stays in lockstep with frameSamplePeriod.
const positionsSampleHz = float64(time.Second) / float64(frameSamplePeriod)

// ResolveTier applies a Tier preset to the stream booleans: core off, detail on for
// light streams, full/empty on for all. Unknown tiers leave caller-set booleans.
func (o *Options) ResolveTier() {
	switch o.Tier {
	case "core":
		o.PlayerFrames, o.Shots, o.GrenadePaths, o.Inventory, o.DroppedWeapons = false, false, false, false, false
	case "detail":
		o.PlayerFrames, o.Shots, o.GrenadePaths = true, true, true
		o.Inventory, o.DroppedWeapons = false, false
	case "full", "":
		o.PlayerFrames, o.Shots, o.GrenadePaths, o.Inventory, o.DroppedWeapons = true, true, true, true, true
	}
}

// tierName reports the tier these stream booleans correspond to: full when all
// five are on, core when none are, detail otherwise. Mirrors ResolveTier.
func (o Options) tierName() string {
	on := 0
	for _, b := range []bool{o.PlayerFrames, o.Shots, o.GrenadePaths, o.Inventory, o.DroppedWeapons} {
		if b {
			on++
		}
	}
	switch on {
	case 5:
		return "full"
	case 0:
		return "core"
	default:
		return "detail"
	}
}

// enabledStreamNames is the sorted list of on streams for meta.output.streams.
func (o Options) enabledStreamNames() []string {
	var names []string
	if o.PlayerFrames {
		names = append(names, streamPositions)
	}
	if o.Shots {
		names = append(names, streamShots)
	}
	if o.GrenadePaths {
		names = append(names, streamGrenadePaths)
	}
	if o.Inventory {
		names = append(names, streamInventory)
	}
	if o.DroppedWeapons {
		names = append(names, streamDroppedWeapons)
	}
	sort.Strings(names)
	return names
}

const frameSamplePeriod = 250 * time.Millisecond

// prerollWindow is how much of each round's freezetime the positions stream keeps
// before go-live, emitted as negative timestamps down to about -prerollWindow.
const prerollWindow = 5 * time.Second

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

	opts.ResolveTier() // turn the tier preset into the stream booleans before we wire handlers
	match := &model.Match{SchemaVersion: 5}
	st := newParseState(parsed, opts, match)

	parsed.RegisterNetMessageHandler(st.onServerInfo)
	parsed.RegisterNetMessageHandler(st.onFileHeader)

	parsed.RegisterEventHandler(st.onTeamSideSwitch)
	parsed.RegisterEventHandler(st.onFreezetimeEnd)
	parsed.RegisterEventHandler(st.onRoundStart)
	parsed.RegisterEventHandler(st.onKill)
	parsed.RegisterEventHandler(st.onOtherDeath)
	parsed.RegisterEventHandler(st.onPlayerHurt)
	parsed.RegisterEventHandler(st.onWeaponFire)
	parsed.RegisterEventHandler(st.onItemPickup)
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

	parsed.RegisterEventHandler(st.onDroppedWeaponsPoll)

	parsed.RegisterEventHandler(st.onSmokeStartTrack)
	parsed.RegisterEventHandler(st.onSmokeExpiredTrack)

	parsed.RegisterEventHandler(st.onPlayerFlashed)

	parsed.RegisterEventHandler(st.onBombPlanted)
	parsed.RegisterEventHandler(st.onBombDefuseStart)
	parsed.RegisterEventHandler(st.onBombDefuseAborted)
	parsed.RegisterEventHandler(st.onBombDefused)
	parsed.RegisterEventHandler(st.onBombExplode)

	parsed.RegisterEventHandler(st.onGrenadeDestroy)
	parsed.RegisterEventHandler(st.onGrenadeBounce)

	parsed.RegisterEventHandler(st.onPlayerFrames)
	parsed.RegisterEventHandler(st.onBuyWindowClose)
	parsed.RegisterEventHandler(st.onSpeedSample)
	parsed.RegisterEventHandler(st.onSighting)

	parsed.RegisterEventHandler(st.onDisconnect)
	parsed.RegisterEventHandler(st.onConnect)
	parsed.RegisterEventHandler(st.onBotConnect)
	parsed.RegisterEventHandler(st.onBotTakenOver)

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
