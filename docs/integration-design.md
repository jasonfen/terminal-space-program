# Integration design

*Written 2026-04-25 to consolidate the v0.4.2 / v0.4.3 / v0.4.4 patch
sequence and stop chasing one-off bug reports about warp-vs-SOI-vs-
thrust interactions. Lands as the design contract that v0.5.0
implementation work will follow.*

The v0.3.x line shipped Verlet sub-stepping with a per-20-tick SOI
throttle. Subsequent reports surfaced enough edge cases (high-warp
mid-tick SOI crossings; Verlet eccentricity drift on circular orbits;
heliocentric transfers skipping planet SOIs under warp lock) that the
sensible move is to write the integration model down once, settle on
the events it must respect, and implement against the design.

---

## 1. Integration modes

Three primitive step routines live in `internal/physics`:

| Mode | Function | Order | Force model | Cost |
|------|----------|-------|-------------|------|
| **Verlet** | `physics.StepVerlet` | 2nd | gravity only | 1 force eval / step |
| **RK4 thrust** | `World.stepThrust` (calls `physics.StepRK4ThrustState`) | 4th | gravity + thrust | 4 force evals / step |
| **Kepler** | `physics.KeplerStep` (v0.4.3) | exact (Newton tol) | gravity, no thrust | 1 Kepler solve / step |

Verlet is symplectic — energy is bounded, but second-order accuracy
means *eccentricity* random-walks at coarse step sizes (the v0.4.3
report). RK4 is non-symplectic — energy drifts secularly, but it
handles non-conservative thrust correctly. KeplerStep is exact for
bound elliptic two-body motion but knows nothing about thrust or
foreign SOIs.

Each mode covers a slice of the `(thrust, dt/period, bound?)` space.
The integrator's job is selecting the right slice every tick.

## 2. Events the integrator must respect

A "tick" advances `Clock.SimTime` by `simDelta = warp × baseStep`. The
craft's state must be correct at the end of `simDelta`. Events that
*can* occur inside that interval and must not be skipped:

1. **SOI crossing** (entering or leaving a body's sphere of influence).
   Frame must rebase to the new primary; subsequent integration uses
   that primary's μ.
2. **Burn fire time** (planted `ManeuverNode.TriggerTime`). The Δv
   applies to the craft's state at that exact moment.
3. **Burn end time** (`ActiveBurn.EndTime`). Thrust stops cleanly so
   post-burn integration switches back to free-flight modes.
4. **Periapsis / apoapsis** (v0.6 burn-at-next scheduler). Not
   integration-critical but the planner UX needs them.
5. **Collision** with a body surface. Out of scope for v0.4.x; flagged
   in the HUD via the peri-below-surface warning, not enforced.

Events 1–3 are the integration-correctness ones. Event 4 is a planner-
layer concern that the integrator can expose hooks for. Event 5 is
deferred.

## 3. Mode selection (gate matrix)

For a tick with no active burn and no fire-time inside `simDelta`:

| Condition | Mode |
|-----------|------|
| craft hyperbolic (e ≥ 1) | Verlet sub-stepped, per-sub-step SOI |
| craft bound, apo < 0.95·primary SOI, no foreign SOI within reach | KeplerStep |
| craft bound, but apo near or beyond primary SOI / foreign SOI within reach | Verlet sub-stepped |
| warp == 1× | Verlet sub-stepped (cosmetic — Kepler would work, but matches paused/realtime behavior) |

If a burn fires inside `simDelta`:

| Pre-fire interval | Mode |
|-------------------|------|
| Free flight up to `TriggerTime` | KeplerStep or Verlet by the row above |
| `[TriggerTime, EndTime]` | RK4 thrust at `min(dt, period/100)` sub-steps |
| Post-`EndTime` interval | KeplerStep or Verlet by the row above |

This naturally relaxes the v0.3.x rule that "warp clamps to 10× during
active burn" — only the **burn segment** needs the warp clamp; the
coast before/after can stay at the user's selected warp.

## 4. Foreign-SOI proximity check

The v0.4.3 gap: when craft is heliocentric (Sun primary), KeplerStep
runs unconditionally because Sun has no enclosing SOI. An Earth→Mars
transfer ellipse passes through Mars's SOI without entry being
detected. Three approaches considered:

- **A. Conservative gate** — reject KeplerStep if any other body's SOI
  is within `max(r, apo)` of the craft. Hack. Loses warp benefit on
  most heliocentric trajectories (every interplanetary transfer
  trips it).
- **B. Sub-divided KeplerStep** — cap each analytic step at
  `min(simDelta, smallest_SOI_radius / craft_speed)`, run a Verlet-
  style SOI test between caps. Pragmatic v0.4.4 fix.
- **C. Event-driven step** — analytically predict time-to-next-SOI-
  intersection (ray-cast the Kepler trajectory against each body's
  SOI), step exactly up to that event, switch to Verlet for the
  crossing, then resume. Right answer; v0.5.0 work.

**Decision**: ship B as v0.4.4 to close the immediate gap, fold the
event-driven version into v0.5.0 alongside body hierarchy. The
sub-divided version still uses Kepler propagation for the bulk of
each tick — only the SOI-test cadence is tighter — so the eccentricity-
drift fix from v0.4.3 stays intact.

## 5. Predictor / live integrator coherence

`PredictedSegments` draws the dashed trajectory the player sees on the
canvas; `integrateSpacecraft` advances the actual craft state. The two
must agree, otherwise the predicted line and the live craft drift
apart visibly under warp.

State of play:

- **v0.3.x**: predictor SOI-aware per sub-step; live integrator only
  via per-20-tick `maybeSwitchPrimary`. Live drifted off prediction
  at high warp.
- **v0.4.2**: live integrator gained per-sub-step SOI. Now both used
  Verlet end-to-end; agreement restored.
- **v0.4.3**: live integrator gained KeplerStep on warp. Predictor
  still Verlet. Agreement broken in the *opposite* direction —
  prediction less accurate than live.

**Decision**: the predictor uses the same mode-selection logic as the
live integrator. Both share an internal `stepCraft(state, primary,
dt) → (state, primary)` primitive that picks Verlet vs KeplerStep vs
RK4-thrust per the gate matrix in §3, and both use the same event-
driven outer loop from §6.

This is the largest single refactor implied by this doc. It pays back
not just in coherence — every accuracy improvement (e.g. KeplerStep,
event-driven crossings) lands in both surfaces simultaneously, which
is what was missed in the v0.3 → v0.4.3 sequence.

## 6. Event-driven outer loop (v0.5.0 target)

```
remaining = simDelta
while remaining > 0:
    event_dt = min(
        time_to_soi_crossing(),  // analytic ray-cast for Kepler; verlet bisection for non-Kepler
        time_to_node_fire(),
        time_to_active_burn_end(),
        remaining,
    )
    mode = select_mode(state, primary, active_burn, warp)
    state, primary = step(mode, state, primary, event_dt)
    remaining -= event_dt
    handle_event(state, primary, ...)  // rebase / fire node / end burn
```

`time_to_soi_crossing` is the open piece. For Kepler, the craft's
trajectory is a known conic and SOI boundaries are spheres around
moving bodies — we approximate body motion as linear over `event_dt`
(adequate when `event_dt « body period`) and ray-cast. For Verlet/RK4
sub-stepping we keep the per-sub-step `FindPrimary` test and bisect
the crossing.

## 7. Save / load

KeplerStep is stateless — orbital elements are derived from `(r, v)`
each step. No save format changes needed. The v0.4.0 envelope already
captures everything the new modes need.

## 8. Phased rollout

- **v0.4.4** — Foreign-SOI proximity check (approach B). Closes the
  heliocentric-warp gap reported 2026-04-25. ~30 LOC + a transfer-
  through-Mars-SOI-under-warp test. *Tactical patch.*
- **v0.5.0** — Body hierarchy (already planned) + event-driven outer
  loop + predictor / live integrator unification (this doc). The two
  stacks naturally compose: hierarchical SOI lookup is exactly what
  `time_to_soi_crossing` needs anyway. ~250–400 LOC + integration
  tests covering the gate matrix in §3. *Architectural milestone.*
- **v0.6.0** — Event types extended for the burn-at-next scheduler
  (next periapsis / apoapsis / AN / DN / SOI exit / fuel < X).
  Reuses §6 machinery — these are just additional `time_to_X`
  callbacks. ~100 LOC + UX work in the maneuver planner.

## 9. Decisions locked in

1. **Three integrator modes**, gate matrix per §3.
2. **Event-driven outer loop** in v0.5.0; v0.4.4 patches the immediate
   foreign-SOI gap with sub-divided KeplerStep but keeps the existing
   tick-bounded loop.
3. **Predictor and live integrator share the step machinery** in
   v0.5.0. v0.4.x asymmetries are tolerated as the cost of staged
   delivery.
4. **Warp clamp scoped to burn segments** in v0.5.0 (not whole tick).
5. **No save schema changes** — KeplerStep is stateless.

## 10. Open questions

- **Predictor sample density at high warp.** `PredictedSegments` uses a
  fixed 96 samples over the predicted horizon. At 10000× warp the
  predicted horizon can cover ~50 sim-days; for a 90-minute LEO that's
  800 orbits compressed into 96 samples — the dashed line collapses to
  a smear. Adaptive sampling (denser near "interesting" events from
  §6) is a v0.5.x polish item, flagged here so it doesn't get
  forgotten.
- **`time_to_soi_crossing` for moving bodies.** Linear-extrapolation
  body motion within `event_dt` is fine when `event_dt` is a fraction
  of the body's period. For inter-system transfers where `event_dt`
  approaches a planet's period, we'd need either (a) iterating the
  ray-cast or (b) splitting into shorter sub-events. Decide during
  v0.5.0 implementation.
- **Behavior at warp == 1×.** Currently §3 picks Verlet at warp = 1×
  for "matches paused/realtime behavior" reasons. Could just as well
  use Kepler — but warp = 1× is the only mode where the user can
  observe per-tick state changes, and we want the simplest visible
  semantics. Revisit if it complicates the unified step machinery.

---

*This doc is the contract: v0.4.4 ships the tactical sub-division
patch, v0.5.0 lands the architectural rework. Bug reports between
those releases should be triaged against this design rather than
patched in isolation.*
