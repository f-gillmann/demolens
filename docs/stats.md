# How the stats are computed

Every number in the JSON output, and how each one is computed.

Two sources feed the output. The **parser** (`internal/parser`) walks the demo event stream and writes the raw per-round and per-player data,
plus the few stats that need live game state: counter-strafe, spotted accuracy, time-to-damage, crosshair placement, spray.

The **metrics** layer (`internal/metrics`) runs after and derives whatever falls out of that data on its own.
Ratios, KAST, ratings, trades, clutches, openings, the matrices, utility averages.

The stats split into two kinds and the difference matters:

- **Deterministic** stats (kills, accuracy, headshots, etc.) are exact counts and ratios, straight from the demo.
- **Estimated** stats (spotted accuracy, counter-strafe, spray, time-to-damage, crosshair) rebuild visibility, recoil, and velocity from demo geometry and timing, because the game never stores them. They are approximations.

## Conventions

Times are in microseconds.
Anything round-relative is measured from freezetime end.
Teams get pinned as A/B at first sighting and stay put when sides swap; CT/T is tracked per round instead.
A "live round" is freezetime end through round end.
Kills and deaths past that point are exit kills/deaths and don't touch K/D.

## Raw counts

Summed per round, then totalled for the match (`aggregate.go`).
Kills and deaths come off kill events; post-round ones land in ExitKills/ExitDeaths.
Assists and flash assists also come off the kill event, headshots off its headshot flag.
Damage is PlayerHurt health damage, capped at the victim's remaining HP.
ShotsFired counts gun shots, gated to the live round so it matches the live-only damage timeline behind accuracy.

## Basic ratios (`ratios.go`)

K/D is kills over deaths, or just kills when deaths is zero.
ADR is damage per round. KPR/DPR/APR are kills, deaths, assists per round.
HS% is headshot *kills* over kills. That tracks the in-game scoreboard, not the separate head-accuracy stat below.

With $K$ kills, $D$ deaths, $A$ assists, and $R$ rounds:

$$\text{K/D} = \frac{K}{D} \qquad \text{ADR} = \frac{\text{damage}}{R} \qquad \text{KPR} = \frac{K}{R} \qquad \text{DPR} = \frac{D}{R} \qquad \text{APR} = \frac{A}{R}$$

$$\text{HS\%} = \frac{\text{headshot kills}}{K}$$

## KAST (`kast.go`)

Share of rounds where you got a kill, an assist, survived, or got traded.
Any one of those credits the round, and KAST is credited rounds over total.
"Traded" here means your killer died to a teammate inside the trade window.

$$\text{KAST} = \frac{\text{rounds with a kill, assist, survival, or trade}}{R}$$

## HLTV ratings (`hltv.go`)

Rating 1.0 is the published formula exactly: kill rating, survival rating, a multi-kill term, normalized by the standard averages.
With $k_n$ the number of rounds holding exactly $n$ kills:

$$\text{KillRating} = \frac{K/R}{0.679} \qquad \text{SurvivalRating} = \frac{(R - D)/R}{0.317}$$

$$\text{MultiKillRating} = \frac{(k_1 + 4k_2 + 9k_3 + 16k_4 + 25k_5)/R}{1.277}$$

$$\text{Rating 1.0} = \frac{\text{KillRating} + 0.7\,\text{SurvivalRating} + \text{MultiKillRating}}{2.7}$$

Rating 2.0 is proprietary, so what we compute is an approximation from a [public writeup](https://dave.xn--tckwe/posts/reverse-engineering-hltv-rating/). It's a ballpark, not the real thing.

$$\text{Impact} = 2.13\,\text{KPR} + 0.42\,\text{APR} - 0.41$$

$$\text{Rating 2.0} = 0.0073\,\text{KAST} + 0.3591\,\text{KPR} - 0.5329\,\text{DPR} + 0.2372\,\text{Impact} + 0.0032\,\text{ADR} + 0.1587$$

## Accuracy

### Overall

Bullet hits on enemies over shots fired.
Hits come from the damage timeline, deduped by shot time,
so a bullet that penetrates two people only counts once.
Both sides include post-round fire so they line up.

$$\text{Accuracy} = \frac{\text{bullet hits on enemies}}{\text{shots fired}}$$

### Head accuracy

Head hits over all hits on enemies, AWP excluded by the usual convention.
Same per-shot dedupe. This is every bullet that hit a head, not headshot kills.

$$\text{Head accuracy} = \frac{\text{head hits}}{\text{hits on enemies}} \quad (\text{AWP excluded})$$

## Aim stats

All of these hinge on whether you could actually see an enemy, and the demo won't tell you that.
The in-engine "spotted" flag lags real visibility by up to half a second,
so we reconstruct sight geometrically against a collision mesh of the map (see `maps.md`).
The enemy has to be on screen (inside the view frustum),
with a clear raycast to some part of their body, and not behind smoke.
`seesTarget` and `shooterHasVision` handle it.

### Spotted accuracy

Hits over shots while an enemy was in your view.
Denominator: gun shots fired with an enemy on screen and visible,
or one you saw in the last 500ms (you keep firing at someone who just ducked behind cover).
Numerator: hits where the enemy was actually visible at that instant,
which drops wallbangs and through-smoke hits.
The denominator uses recently-seen, the numerator uses actually-visible.
That split keeps the ratio steady.

$$\text{Spotted accuracy} = \frac{\text{hits with the enemy visible}}{\text{shots with an enemy in view}}$$

### Counter-strafe

The share of rifle shots you fired while basically stopped.
A shot counts if an enemy was in vision and you weren't fully crouched.
It's "good" when your speed was under 40% of the weapon's max. Rifles only.
CS2 leaves velocity out of the demo, so we derive speed from how far you moved between frames,
and 40% is the threshold we settled on (the figure usually quoted is 34%).
The raw good/total counts are exposed too.

$$\text{Counter-strafe} = \frac{\text{stopped shots}}{\text{measured rifle shots}}, \quad \text{stopped when } v < 0.40\,v_{\max}$$

### Spray accuracy

Within bursts of 3+ rifle shots, how many bullets hit.
A burst is consecutive shots from one auto rifle with sub-300ms gaps.
A bullet enters the ratio if you were aiming at a visible enemy,
using a tighter cone than the counter-strafe gate.
It's a hit if a rifle damage on an enemy lands on the same tick.

$$\text{Spray accuracy} = \frac{\text{spray bullets that hit}}{\text{spray bullets fired}}$$

### Time to damage

From seeing an enemy to your first damage on them.
The clock starts when they enter your view frustum with clear los and no smoke,
survives brief look-aways, and stops on your first gun damage. One sample per sighting.
Plays where you saw someone but held fire (trigger discipline) get dropped.
A fixed cutoff can't separate a slow player's normal duel from a fast player holding an angle,
so instead we drop each player's *own* long outliers (anything past 2.2x their median),
clamp the rest, and average.

For per-sighting samples $t_i$ (ms from first seeing the enemy to first damage) with player median $m$:

$$\text{TTD} = \operatorname{mean}\big\{\, \min(t_i,\ 1300) \;:\; t_i \le 2.2\,m \,\big\}$$

### Crosshair placement

The median angle your view swung between the enemy first appearing on screen and your first hit.

For duels $i$ with view-move angle $\theta_i$ (degrees):

$$\text{Crosshair placement} = \operatorname{median}_i\ \theta_i$$

## Trades (`trades.go`)

A teammate trades your death by killing your killer within 4 seconds. The funnel has three steps.
An *opportunity* is a living teammate within 550 units of the killer, or already fighting them.
An *attempt* is that teammate damaging any enemy in the window.
A *success* is them landing the kill.
Counted both ways: trade-kills for the avenger, traded-deaths for the victim.

## Opening duels (`opening.go`)

First kill of the round.
Killer banks a won opening, victim a lost one (flagged traded if the death was traded).
Reported overall and by side.

## Clutches (`clutch.go`)

A clutch begins when a side is down to its last player with at least one enemy still up.
Won if they take the round, saved if they lose it but live, otherwise lost.
Kills made during the clutch are counted. A 1v1 counts for both sides.

## Breakdowns (`breakdown.go`)

Multi-kills are rounds with exactly n kills, 1 through 5.
No-scope and wallbang come off the kill flags.
A collateral is 2+ victims on a single bullet.
Weapon stats break down kills, headshots, and damage per weapon.
The duel matrix is head-to-head kill counts for every killer-victim pair;
the flash matrix is per flasher-flashed pair with total blind time.

## Utility (`utility.go`, `aggregate.go`)

Raw counts (nades thrown, people flashed, HE and molotov damage, utility value used and held) sum per round, then average.
Someone has to be blinded 1.1s or more to count as "fully flashed", and teammates here includes yourself.
A flash leads to a kill when someone you fully blinded dies while still blind, to your team.
demoinfocs reports blind durations a touch long, so we scale them by 0.85;
that pulls both flashed counts and blind duration into line at once.
