# demolens

CS2 demo analyzer written in Go. Feed it a `.dem` file, get per-player and per-round stats back as JSON. Works as a CLI or as a library.

## Features

- **Core stats:** kills, deaths, assists, K/D, ADR, KPR/DPR/APR, KAST, headshot %, accuracy, HLTV 1.0 and 2.0 ratings
- **Aim stats:** spotted accuracy, counter-strafe, spray accuracy, time-to-damage, crosshair placement
- **Round detail:** trades, opening duels, clutches, multi-kills, weapon breakdowns, duel and flash matrices
- **Utility:** flashes thrown and landed, blind durations, HE and molotov damage, unused utility on death
- **Optional heavy output:** per-frame positions, per-shot geometry, grenade trajectories, smoke voxel clouds

## Install

```sh
go install github.com/f-gillmann/demolens/v2/cmd/demolens@latest
```

Or build from a checkout:

```sh
go build -o demolens ./cmd/demolens
```

## Usage

```sh
demolens analyze match.dem            # full stats as JSON on stdout
demolens analyze match.dem -o out/    # write out/<hash>.json instead
demolens check match.dem              # just the hash and header
demolens schema -o schema.json        # JSON Schema of the analyze output
```

A few of the aim stats need a line-of-sight test, so they only kick in when a map mesh is around. Grab the mesh for the map first, then point `analyze` at it:

```sh
demolens extract-map --cs2 "<CS2 install>" --map de_mirage
demolens analyze match.dem --maps-dir tris
```

Mesh setup lives in [docs/maps.md](docs/maps.md).

## Library

```go
import "github.com/f-gillmann/demolens/v2"

f, _ := os.Open("match.dem")
defer f.Close()

match, err := demolens.Analyze(f, demolens.Options{MapsDir: "tris"})
```

`Options` toggles the heavy output and holds the calibration knobs for the estimated stats. The zero value is fine for most use.

## Stats

The deterministic stats (kills, accuracy, headshots, ratings) are exact, straight from the demo.
The estimated ones (spotted accuracy, counter-strafe, spray, time-to-damage, crosshair) rebuild visibility and recoil from the demo's geometry and timing, since the game never records that.

[docs/stats.md](docs/stats.md) walks through how each number is computed.
[docs/output.md](docs/output.md) documents the JSON shape for consumers: tiers, streams, absence rules, encodings.
[schema.json](schema.json) is the machine-readable schema; `demolens schema` regenerates it.

## Map data

No map geometry ships with demolens. CS2 maps are Valve's assets, so you pull collision meshes from your own install with `extract-map`. Full setup in [docs/maps.md](docs/maps.md).

## License

MIT. See [LICENSE](LICENSE).
