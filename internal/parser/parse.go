package parser

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/f-gillmann/demolens/v2/model"
	dem "github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/events"
)

// CS2Magic is the 8-byte stamp every CS2 demo starts with.
const CS2Magic = "PBDEMS2"

// Options turns on the expensive per-frame captures. Zero value is the cheap path
// (no heavy streams, tier "core"). A non-empty Tier preset flips the stream
// booleans on resolve; individual booleans set explicitly are not overridden.
type Options struct {
	PlayerFrames bool        // "positions" stream: sample player pos + state each frame
	Shots        bool        // "shots" stream: per-shot shooter geometry
	GrenadePaths bool        // "grenade_paths" stream: grenade trajectories + bounces
	Inventory    bool        // "inventory" stream: mid-round inventory change log
	GroundItems  bool        // "ground_items" stream: world weapons at phase boundaries
	PositionsHz  float64     // positions-stream sample rate. <=0 falls back to defaultPositionsHz (4)
	Tier         string      // optional preset: core / detail / full. empty means full.
	MapsDir      string      // .tri mesh dir for TTD los. empty disables TTD
	Calibration  Calibration // aim-stat thresholds, zero fields fall back to defaults
	AimDebugPath string      // when set, write per-frame aim diagnostic CSVs to <path>.cands.csv / <path>.dmg.csv / <path>.legend.csv. empty disables.
}

// stream names exactly as they appear in meta.output.streams, sorted.
const (
	streamPositions    = "positions"
	streamShots        = "shots"
	streamGrenadePaths = "grenade_paths"
	streamInventory    = "inventory"
	streamGroundItems  = "ground_items"
)

// defaultPositionsHz is the positions-stream sample rate when Options.PositionsHz
// is unset (<=0). One sample / 250ms.
const defaultPositionsHz = 4.0

// framePeriod is the positions-stream sample interval for the configured Hz.
func framePeriod(hz float64) time.Duration {
	if hz <= 0 {
		hz = defaultPositionsHz
	}
	return time.Duration(float64(time.Second) / hz)
}

// ResolveTier applies a Tier preset to the stream booleans: core off, detail on for
// light streams, full/empty on for all. Unknown tiers leave caller-set booleans.
func (o *Options) ResolveTier() {
	switch o.Tier {
	case "core":
		o.PlayerFrames, o.Shots, o.GrenadePaths, o.Inventory, o.GroundItems = false, false, false, false, false
	case "detail":
		o.PlayerFrames, o.Shots, o.GrenadePaths = true, true, true
		o.Inventory, o.GroundItems = false, false
	case "full", "":
		o.PlayerFrames, o.Shots, o.GrenadePaths, o.Inventory, o.GroundItems = true, true, true, true, true
	}
}

// tierName reports the tier these stream booleans correspond to: full when all
// five are on, core when none are, detail otherwise. Mirrors ResolveTier.
func (o Options) tierName() string {
	on := 0
	for _, b := range []bool{o.PlayerFrames, o.Shots, o.GrenadePaths, o.Inventory, o.GroundItems} {
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
	if o.GroundItems {
		names = append(names, streamGroundItems)
	}
	sort.Strings(names)
	return names
}

// prerollWindow is how much of each round's freezetime the positions stream keeps
// before go-live, emitted as negative timestamps down to about -prerollWindow.
const prerollWindow = 5 * time.Second

// longest pause between two shots that still counts as one spray
const sprayGap = 300 * time.Millisecond

// blind has to last at least this long to count as "fully flashed"
const flashFullyBlind = 1100 * time.Millisecond

func Parse(r io.Reader, opts Options) (_ *model.Match, err error) {
	hash := sha256.New()
	// tee below bufio so hashing sees every byte exactly once, then peek the
	// header to fail fast on non-CS2 input; demoinfocs reads from the bufio reader.
	buf := bufio.NewReader(io.TeeReader(r, hash))
	header, err := buf.Peek(8)
	if err != nil {
		return nil, fmt.Errorf("read demo header: %w", err)
	}
	if format := strings.TrimRight(string(header), "\x00"); format != CS2Magic {
		return nil, fmt.Errorf("not a CS2 demo (header %q, want %q)", format, CS2Magic)
	}
	parsed := dem.NewParser(buf)
	defer func() {
		if closeErr := parsed.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}()

	opts.ResolveTier() // turn the tier preset into the stream booleans before we wire handlers
	match := &model.Match{SchemaVersion: 6}
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

	parsed.RegisterEventHandler(st.onDataTablesParsed)
	parsed.RegisterEventHandler(st.onGroundItemsPoll)

	parsed.RegisterEventHandler(st.onSmokeStartTrack)
	parsed.RegisterEventHandler(st.onSmokeExpiredTrack)

	parsed.RegisterEventHandler(st.onPlayerFlashed)

	parsed.RegisterEventHandler(st.onBombPlantBegin)
	parsed.RegisterEventHandler(st.onBombPlantAborted)
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
