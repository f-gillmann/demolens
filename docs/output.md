# The JSON output

What the analyze JSON means, for people who parse it and build on it.
[schema.json](../schema.json) is the authoritative field list; this doc covers the semantics a schema can't:
gating, absence rules, encodings, layout conventions. How the numbers are computed is in [stats.md](stats.md).

## Top-level layout

The document root is `meta`, `players`, `rounds`, `stats`, `file_hash` (SHA-256 of the demo bytes), and `schema_version` (currently 6).

The root `stats` block holds the match-level aggregates: `duel_pairs` (killer/victim head-to-head),
`flash_pairs` (who blinded whom), and `match_lifecycle` (connect/disconnect/bot events).
It is a different scope from `players[].stats` (per-player ratios and ratings). Same key, different level, by design.

Every SteamID64 in the document is a decimal **string**, whether a value or an object key.
Exported floats are rounded to 2 decimals.

Inside a round, events and time series are split. The top-level round arrays
(`kills`, `exit_kills`, `damages`, `grenades`, `pickups`, `shot_stats`) are event lists: one entry per thing that happened.
`round.streams` holds the opt-in columnar time series: `positions`, `shots`, `grenade_paths`, `inventory`, `ground_items`.
If it isn't sampled over time, it isn't in `streams`.

Two of those pairs look redundant but aren't. `grenades` (the event: who threw what, when, and its outcome)
and `streams.grenade_paths` (the time series: the flight trajectory and bounce points) describe the same
throw at two different resolutions; they join on `grenade_id`. `pickups` (true transfers only, see below)
and `streams.ground_items` (every item-on-the-ground stint, including ones nobody ever picked back up) both
touch "who's holding what," but `ground_items` is the complete record; `pickups` is a derived subset of it.

## Tiers and streams

`--tier` picks a stream preset: `core` turns all five streams off, `detail` turns on
`positions`, `shots` and `grenade_paths`, `full` (the CLI default) turns on all five.
Each stream also has an override flag (`--positions`, `--shots`, `--grenade-paths`, `--inventory`, `--ground-items`)
that wins over the preset for that one stream.

`meta.output` is the document's self-description:

- `tier`: `core` / `detail` / `full`, derived from which streams ended up on
- `streams`: the enabled stream names, sorted; `null` when none are on
- `positions_sample_hz`: the positions sample rate (`--positions-hz`, default 4); only present when positions are on
- `positions_fields` / `ground_item_positions_fields`: the declared column order of the columnar tuples; only present when their stream is on
- `map_mesh_loaded`: whether a collision mesh was found, the gate for the geometric line-of-sight stats

A consumer must check `meta.output` to tell "disabled" from "empty".
`round.streams` is absent until something actually wrote into it that round,
so a missing `streams` block alone doesn't say whether a stream was off or just quiet.

## Absence rules

The output leans on omitempty: **absent means not applicable, never zero**.

When `meta.output.map_mesh_loaded` is false, the line-of-sight stats drop out entirely:
`spotted_accuracy_pct`, `spray_accuracy_pct`, `time_to_damage_ms` from `players[].stats`,
the raw counters `spotted_shots`, `spotted_hits`, `time_to_damage_samples`,
plus `counter_strafe`, `spray_weapons`, and `time_to_damage_ms` inside `duel_pairs`.
The one exception: `crosshair_placement` and `crosshair_samples` are always present and just read 0 without a mesh.
`spray_patterns` (recoil deviation) needs no mesh and survives.

Per-kill fields appear only on kills they apply to: `traded`, `traded_by`, `possible_traders`,
`assister`, `flash_assister`, `killer_scoped`, `picked_up`, `collateral`.
`killer` is `null` when `kind` is not `player` (bomb, world, suicide), and those kills also
lack `killer_side`, `killer_position`, `distance`, `killer_speed`, `killer_speed_ratio`.
`damages[].attacker_position` / `victim_position` are set only when the positions stream is on.
A c4 detonation writes one `damages[]` entry per victim (`damage_type: "bomb"`) with no
`attacker`; the shockwave reaches each victim staggered by distance, so their `t_ms` land
after `round_end_ms`, at the moment the damage actually applied.
`meta.c4_wave_speed` is present only on post-rework demos (`build_num >= 14168`) and absent
on older demos that hurt every victim on the same tick.

Time convention: every round-relative `t_ms` counts from freeze end. The round goes live at t = 0.
`round_start_ms` is the freeze start, so it is negative; `freeze_end_ms` is always 0;
`round_end_ms` and `post_round_ms` bracket the live round and the exit window.
The positions stream keeps about the last 5 seconds of freezetime as negative timestamps.

## Positions stream

`streams.positions` is an object keyed by steam_id string. Each value is that player's
time-ordered array of columnar tuples, so the 17-char steam_id appears once per player, not once per sample.
The per-row column order is declared in `meta.output.positions_fields`; don't hardcode it.

The `flags` column packs eight booleans: alive=1, airborne=2, scoped=4, ducking=8,
has_defuse_kit=16, buyzone=32, walking=64, bomb_zone=128.
Velocity is derived from position deltas (CS2 doesn't network it) and is a 0 vector when unknown.
`active_weapon` is always resolved, falling back to `defuse_kit` / `c4` / last-known on ticks where the engine reports none.

The last column, `hold_frames`, is run-length encoding: the row's exact state persists for that many
**additional** sample periods after its `t_ms`. Hold the player static, no interpolation. 0 is a normal single frame.
The sample period is `1 / positions_sample_hz`.

`ground_items[].positions` uses the same pattern with its own column list
(`meta.output.ground_item_positions_fields`: t_ms, x, y, z, hold_frames), so a resting item collapses to one tuple.

Each `ground_items[]` entry is one on-the-ground stint, not just its position track. `dropped_at_ms` /
`picked_up_at_ms` bracket the stint (`picked_up_at_ms` absent means it's still down at round end);
`is_initial` marks round-start spawn state (`dropped_at_ms == 0`) rather than a real mid-round drop;
`last_owner` is who dropped it; `on_death` marks a drop forced by that owner dying. C4 rides in this
same stream: the bomb is one physical object that changes hands like any other dropped item.

## Smoke voxel clouds

`grenades.smokes[].voxels` is the smoke's networked volumetric occupancy, present only when the
`grenade_paths` stream is on **and** the demo carries the voxel stream. Older CS2 demos don't;
render a fallback circle when it's absent.

The grid is 32x32x32 voxels, anchored at the detonation:

- `origin` = detonation position minus 192 per axis (the grid's world min corner)
- `cell` = 12 game units per voxel edge
- voxel (x, y, z) spans `[origin + v*cell, origin + (v+1)*cell]` on each axis
- linear index = `x + y*32 + z*1024`

Every occupancy list is run-length encoded over the sorted linear indices as flat
`[start, len, start, len, ...]` pairs; one run covers `start` through `start+len-1`.

`samples[0]` carries `occupied`: the full set at the detonation keyframe.
Every later sample carries `add` and `del`: the change against the previous sample's reconstructed set.
Samples land only when the shape changed, at least 1 second apart, and stop once the cloud starts fading.
Hold each shape until the next sample.

```
set = {}
for s in voxels.samples:
    for (start, len) in pairs(s.occupied): set += {start .. start+len-1}
    for (start, len) in pairs(s.add):      set += {start .. start+len-1}
    for (start, len) in pairs(s.del):      set -= {start .. start+len-1}
    # set is the occupancy from s.t_ms until the next sample
    # world box of index i: x=i%32, y=(i/32)%32, z=i/1024, min corner origin + (x,y,z)*cell
```

## Flash blindness curve

`grenades.flashes[].flashed[]` is per victim: `blind_ms` (calibrated effective blind time),
`flash_duration_ms` (the raw networked flash effect duration), and `max_alpha` (peak whiteout,
0..255). Drive the on-screen whiteout fade off `flash_duration_ms`, not `blind_ms`.

The blindness starts at the grenade's `detonate_ms`. With `left = flash_duration_ms - (t_ms - detonate_ms)`
(all ms) and `A = max_alpha`, the client renders the whiteout alpha at render time `t_ms` as:

```
left <= 0          : alpha = 0
left  > 3000       : alpha = A                     (held at full whiteout)
0 < left <= 3000   : alpha = A * (left/3000)^2     (quadratic fade over the last 3s)
```

The first ~94ms is a linear 0..A build-up ramp, negligible for all but the shortest flashes.

## Field notes

**round.bomb** exists once any plant was started; a fake plant is enough, so the completed-plant
fields (`site`, `planter`, `plant_ms`, `plant_position`) are all omitempty. `defused` / `exploded` are the outcome,
`defuse_ms` / `defuse_started_ms` / `has_kit` describe the successful defuse.
`plant_attempts` and `defuse_attempts` list every start, with `aborted` marking fakes and cancels.

**meta.c4_wave_speed** is the planted-c4 shockwave expansion rate in game units per second
(constant `3000`), emitted only on post-rework demos so consumers can animate the expanding
ring against the staggered `damage_type: "bomb"` hits without reverse-engineering it.

**round.economy** has one block per side (`CT` / `T`): `equipment_value` and `buy_type`,
one of `eco` / `semi_eco` / `semi_buy` / `full_buy`.

**round.players[].loadout** is the freeze-time-end inventory snapshot: `weapons` / `grenades` / `equipment`,
grouped with counts and values, plus `total_value`. The sibling `equipment_value` is a different capture:
buy-window close, capped at death.

**round.pickups** lists real transfers between players: a grab whose `original_owner` is set and differs
from the holder. `from_enemy` flags a cross-team grab. Fires during the live round and during the
buy/freeze window preceding it, so a teammate handing off a gun before go-live is captured too (negative
`t_ms`, same freeze convention as everywhere else). A self-match (`original_owner` == holder) never
appears here: CS2 stamps `original_owner` to the buyer on many fresh purchases, not just leaving it unset,
so a self-match can mean a fresh buy, the automatic per-round knife/pistol reissue, or (rarely) a genuine
self drop-then-regrab, and those can't be told apart from `original_owner` alone. All three surface only
in `streams.inventory`'s `buy`/`pickup` phase tags below, not as a `round.pickups` entry, since fabricating
a re-grab entry here would risk mislabeling the far more common fresh-buy case.

**streams.inventory** is a per-player loadout change log, not a per-tick dump: an entry is written
when a player's holdings actually change. Snapshots fire on the events `pickup` (a real transfer, mid-round
or during the buy/freeze window; see round.pickups) and `buy` (a fresh acquisition, mid-round or during the
buy/freeze window: a purchase, the automatic knife/pistol reissue, or a self-match that can't be
distinguished from either, see round.pickups), plus the three phase checkpoints `first_contact`,
`bomb_plant` and `round_end`; the `phase` field carries which one. A per-player fingerprint drops unchanged
snapshots, so a checkpoint that repeats an already-logged loadout emits nothing. CS2 GOTV demos carry no
drop event, so a dropped gun is not tagged at the drop instant; it surfaces at the next checkpoint (or,
once someone grabs it, on their `pickup`). The freeze-time-end loadout lives separately in
`round.players[].loadout`.

**players[].counter_strafe** lives at the player top level, **not** inside `players[].stats`:
`shots`, `stopped`, `stopped_rate_pct`, `avg_speed`. Mesh-gated, see above.

**Rank fields** (`rank`, `rank_type`, `competitive_wins`, `rank_if_win` / `rank_if_loss` / `rank_if_tie`,
`crosshair_code`) are Valve matchmaking/Premier only; 0 or absent everywhere else.

**players[].spray_patterns** is per weapon variant (base / scoped / no-silencer) recoil-pattern deviation;
`bullets[]` compares ideal vs actual compensation per shot index, in degrees.
`streams.shots[].recoil_index` is the shot's index into the current spray; map it against this table.
Both are base/pattern-space, not real GOTV recoil. Don't reimplement recoil math on top.

**stats.match_lifecycle** logs `disconnect` / `connect` / `bot_connect` / `bot_taken_over` for playing-team
players: `kind`, `steam_id`, `name`, `round`, `t_ms`. Its `t_ms` is absolute match time, not round-relative.

## Schema

[schema.json](../schema.json) is the authoritative machine-readable schema, generated from the model types.
Regenerate it with `demolens schema -o schema.json`.
