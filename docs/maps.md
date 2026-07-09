# Map collision (line of sight)

A couple of stats need to know whether one player could see another, which means testing the line between them against walls and smoke.
The demo carries no map geometry, and the CS2 map files are Valve's copyrighted assets, so demolens ships none of it. You extract it from maps you already own.

This is what time-to-damage and crosshair placement run on, since both start from "enemy first appears in your view" and need a real sightline test.
The rest (accuracy, head/spotted/spray accuracy, counter-strafe) needs no map data at all.

## The `.tri` format

A `.tri` is a triangle soup for one map, nothing more:

```
"DLT1"            magic (4 bytes)
uint32            triangle count (little-endian)
float32 * 9 * n   triangles: a.xyz, b.xyz, c.xyz (game units)
```

No Valve asset in there, only coordinates pulled out of your own files.

## How a map gets matched to a demo

The demo names the map and `geom.MapFile` resolves it to a file. Official maps key on the name, `de_mirage.tri`.
Workshop maps key on the addon id from the demo instead, `3070670813.tri`.
That stops remakes reusing an official name from colliding, and it stays stable across updates.

Drop the files in `tris/`, the default and gitignored, or point somewhere else with `analyze --maps-dir <dir>`.

## Extracting collision from your maps

`extract-map` runs Source2Viewer-CLI on the map's `world_physics.vmdl_c`, pulls the collision groups out of the result, and writes a `.tri`.
It keeps solid surfaces and drops the stuff you see and shoot through anyway.
That means clip brushes, the skybox shell, and every see-through class: chainlink, grates, vents, glass, windows, and anything flagged passbullets.
Library users get the same logic via `demolens.ExtractMap`.

Install [source2viewer-cli](https://github.com/ValveResourceFormat/ValveResourceFormat) first and put it on your PATH, or pass `--vrf /path/to/it`. Then:

```sh
# official map from your CS2 install, writes tris/de_mirage.tri
demolens extract-map \
    --cs2 "/path/to/SteamLibrary/steamapps/common/Counter-Strike Global Offensive" \
    --map de_mirage

# workshop map: key on the addon id so it matches the demo
demolens extract-map \
    --vpk ".../steamapps/workshop/content/730/3070670813/de_dust2.vpk" \
    --key 3070670813

# or feed it a .glb/.obj you exported yourself
demolens extract-map --in world_physics_physics.glb --key de_mirage
```

From Go:

```go
import "github.com/f-gillmann/demolens/v2"
path, n, err := demolens.ExtractMap(demolens.ExtractMapParams{CS2Dir: cs2, Map: "de_mirage", OutDir: "tris"})
```

It reads your files and writes `.tri`s locally. Nothing is downloaded or redistributed.

`--cs2` points at the folder that holds `game/csgo/maps/`. Source2Viewer's flags drift between releases, so if an export fails, check `source2viewer-cli --help` and tweak `runVRF` in `internal/maps/extract.go`.
