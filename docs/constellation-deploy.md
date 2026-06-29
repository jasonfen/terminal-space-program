# Constellation deployment

How to drop a ring of evenly-spaced comsats from a single carrier, and the
phasing-orbit math that lets you do it on almost no fuel. This is an advanced
flight technique; if you're new, read the [controls & flight guide](controls.md)
first.

## The one mechanic that matters

When you press `Y` (Deploy), the top payload is released into the carrier's
**exact current orbit** — same position, same velocity, plus a tiny 75 m / 0.15
m/s nudge so it doesn't auto-redock. There is no separate "insertion orbit"
handed to the satellite, and nothing spaces the satellites for you. Every comsat
comes out wherever the carrier happens to be at the instant you deploy.

Two consequences drive everything below:

1. **The satellites never have to change orbit.** A comsat with ~130 m/s of its
   own is fine — that budget is for final trim and station-keeping, not for
   covering the gap between where you dropped it and where it belongs. If you
   find yourself wanting the comsats to cover a large orbit difference, you
   deployed from the wrong place. Bring the *carrier* to the final orbit first.
2. **The carrier gets lighter at every drop.** Deploy recomputes the carrier's
   mass to exclude the released payload (dry mass + whatever fuel/monoprop that
   comsat carried) the moment you press `Y`. So each successive phasing burn
   costs *less* propellant than the last — see [What it costs in
   fuel](#what-it-costs-in-fuel).

## Deploy from the target orbit, not a transfer orbit

The recipe in one line: **put the carrier in the final circular orbit, then
space the constellation by phasing the carrier — not the satellites.**

1. Circularize the *carrier* into the target orbit. Spend the carrier's delta-v
   here (a tug carries far more than any single comsat).
2. Deploy comsat #1 with `Y`. It sits exactly in the target orbit.
3. Put the carrier into a **phasing orbit** to drift one slot, coast, then
   re-circularize at the same point and deploy comsat #2.
4. Repeat until all *N* are out.

Spacing comes entirely from the carrier drifting between drops. The satellites
just sit where they're put.

## Spacing by phasing

To lay *N* satellites `360°/N` apart, keep one apsis pinned to the deploy point
and change the other so the carrier's period becomes a resonant multiple of the
target period. The simplest version drifts one full slot per revolution:

```
T_phase = T_target × (N ± 1) / N
```

- `(N+1)` → keep periapsis at the deploy point, **raise apoapsis** (carrier
  slower; the already-deployed sats pull ahead). Preferred in a low orbit.
- `(N−1)` → keep apoapsis at the deploy point, **lower periapsis** (carrier
  faster). Avoid in a low orbit — you're dipping toward atmosphere/terrain.

After one phasing revolution the carrier is back at the exact deploy point and
the previous satellite has drifted `360°/N`. Re-circularize, deploy the next,
done.

## Stretching it over multiple revolutions

The single-rev phasing orbit is quite eccentric for small *N*, which makes the
burn expensive. You can spread one slot's worth of drift over **m** revolutions
instead of one, trading time for fuel. The general formula is:

```
T_phase = T_target × (mN ± 1) / (mN)
```

with `m = 1` recovering the single-rev case. As `m` grows, `T_phase → T_target`,
so the apoapsis bump and the burn both shrink. After `m` revolutions the carrier
returns to the deploy point having let the constellation drift exactly one slot
(`m` full laps + `1/N`).

**The trade:** delta-v scales as `1/m`, time scales as `m`. Doubling the
revolutions per slot halves the phasing fuel.

### The numbers

Let `v_c = √(μ/r)` be the circular speed in the target orbit (radius `r`,
gravitational parameter `μ`). The phasing orbit shares the deploy point as one
apsis, so its semi-major axis is `a_phase = r · (T_phase/T)^(2/3)`, and the
speed at the deploy point is:

```
v_apsis = √( μ · (2/r − 1/a_phase) )
```

You burn twice per slot — into the phasing orbit and back out — each of equal
magnitude (same apsis, same speed), so:

```
Δv_slot     = 2 · |v_apsis − v_c|
Δv_campaign = (N − 1) · Δv_slot          (N sats ⇒ N−1 phasing cycles)
time        ≈ (N − 1) · m · T_target
```

For the small offsets that high `m` produces, this collapses to a handy
approximation (`ε = 1/(mN)`):

```
Δv_burn  ≈ v_c / (3·m·N)
Δv_slot  ≈ 2·v_c / (3·m·N)
Δv_campaign ≈ (2·v_c / 3m) · (N−1)/N    ≈ 2·v_c/(3m) for large N
```

## What it costs in fuel

The delta-v above is pure orbital mechanics — **mass-independent**. What it
*costs* in propellant follows the rocket equation against the carrier's current
mass:

```
fuel_burn = m_now · (1 − e^(−Δv_slot / (Isp · g₀)))      (g₀ = 9.80665 m/s²)
```

Because the carrier sheds a comsat's full wet mass at each `Y`, `m_now` drops
deploy by deploy, so you can't just multiply one figure by `N−1`. Walk the mass
down:

```
m_now ← m_now − (comsat dry + comsat fuel)   after each deploy
fuel  ← fuel + m_now · (1 − e^(−Δv_slot/(Isp·g₀)))   for each phasing cycle
```

Net effect: the last phasing cycle is the cheapest, and the whole campaign is
somewhat cheaper than `(N−1) ×` the first cycle's fuel.

## Worked example

Six comsats (`N = 6`), 360°/6 = 60° apart, in an orbit where `v_c ≈ 2300 m/s`:

| m | T_phase factor | Δv per burn | Total phasing Δv | Total revs |
|---|----------------|-------------|------------------|------------|
| 1 | ×7/6  (1.167)  | ~128 m/s    | ~1.28 km/s       | 5          |
| 2 | ×13/12 (1.083) | ~64 m/s     | ~0.64 km/s       | 10         |
| 4 | ×25/24 (1.042) | ~32 m/s     | ~0.32 km/s       | 20         |

Going from `m = 1` to `m = 4` takes the carrier from spending most of a typical
tug's budget to about a quarter of it — at the cost of 4× the warp time.

## Flying it in-game

The **ORBIT** chip and the **PROJECTED ORBIT** chip both show the orbital period
(`2π√(a³/μ)`) under the time-to-apoapsis / time-to-periapsis lines — that's your
tuning instrument. As of **v0.24.4** the period reads down to the second
(`6h04m21s`) rather than rounding to the minute, so you can tune a phasing orbit
to roughly 1-second precision. The placement error that leaves is
`Δφ ≈ 360°·m·δ/T` per slot — a fraction of a degree for any sane `m`, well below
what a comsat's own trim budget cares about.

1. Circularize the carrier into the target orbit; note its period `T` on the
   ORBIT chip.
2. Deploy comsat #1 (`Y`).
3. Pick `m`, then plant a prograde burn and adjust it until the **PROJECTED
   ORBIT** period reads `T × (mN±1)/(mN)`. Commit the burn.
4. Warp `m` full revolutions back to the deploy point.
5. Plant the reverse burn to circularize (PROJECTED period back to `T`), commit,
   deploy comsat #2.
6. Repeat for the rest.

## Practical notes

- **There's a floor.** At high `m` the burn gets too small to plant cleanly and
  the total warp time balloons. When the per-burn delta-v drops near a comsat's
  own ~130 m/s, stop fussing over carrier precision — deploy a little roughly and
  let each comsat trim its own final position with its onboard budget.
- **Coarse carrier + comsat cleanup** is usually the cheapest overall plan: a
  low-`m` (cheap, fast) carrier phasing that gets each sat *close*, finished off
  by the sat's own station-keeping delta-v.
- **Need finer than a second?** Tune the **projected apoapsis** instead of the
  period — it reads to 0.1 km, and at a typical comsat orbit ~1 km of apoapsis is
  ~1 second of period (`dT/dr_apo = ¾·T/a`), so it's an even sharper lever. You'll
  rarely need it now that the period shows seconds.
- There is currently **no automated constellation/phasing helper** — the whole
  flow above is manual.
