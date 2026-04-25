# Orbit accuracy under warp — comprehensive solution

## Context

`docs/integration-design.md` (written 2026-04-25, the day this plan is being
made) is the contract for fixing the warp-vs-SOI-vs-thrust edge cases that
have surfaced through the v0.4.x patch line. v0.4.2 fixed mid-tick SOI
crossings; v0.4.3 added a Kepler fast-path on warp to stop Verlet
eccentricity drift; v0.4.4 sub-divided the Kepler chunks so foreign SOIs
(Mars during a heliocentric transfer) aren't skipped. Each was a tactical
patch.

What's still wrong, even after v0.4.4:

1. **Predictor / live integrator asymmetry.** `predict.go:31`
   (`PredictedSegments`) is Verlet-only — it never uses the Kepler fast-path
   that the live integrator now relies on. Under warp, the dashed predicted
   trajectory is *less accurate* than the live craft, which is the opposite
   of what a planning UI should do.
2. **Three near-identical Verlet+SOI loops** exist:
   `integrateSpacecraft`'s Verlet path (`world.go:193`),
   `propagateCraftWithPrimary` (`world.go:506`, used by node previews),
   and `PredictedSegments` itself. They drift independently when bugs are
   fixed in one and not the others.
3. **Whole-tick warp clamping during burns.** `clampedWarp()` at
   `world.go:145` caps warp to 10× whenever any burn fires *anywhere in the
   tick*. The coast segments before/after the burn pay the same penalty.
4. **Predictor body-position snapshotting.** Bodies are evaluated once at
   the start of `PredictedSegments` (`predict.go`), not per sub-step —
   accumulating phase error on long horizons.
5. **No analytic SOI crossing detection.** Today's mechanism is
   "step-and-check": chunk size in `chunkDtCap()` (`world.go:396`) is a
   safety heuristic (`smallestForeignSOI / (4·speed)`), not an event time.

The intended outcome is the v0.5.0 architectural rework
(integration-design §6, §8): **one event-driven outer loop, shared by the
predictor and the live integrator, picking from three integrator modes
(Verlet / RK4-thrust / Kepler) per a fixed gate matrix, with analytic
prediction of the next SOI / node-fire / burn-end event.**

## Recommended approach

A new pure primitive `physics.StepCraft` plus a shared `sim.Run`
event-loop, introduced as a scaffold first and migrated into one caller per
PR so `main` stays green throughout.

### New surfaces

**`internal/physics/step.go`** (new) — pure single-step primitive, no
SOI lookup, no burn state machine, no `*World`:

```go
type Mode int
const (ModeVerlet Mode = iota; ModeRK4; ModeKepler)

type StepInput struct {
    State  StateVector
    Mu, Dt float64
    Mode   Mode
    Thrust ThrustSpec    // zero value = coast
}
type StepOutput struct {
    State    StateVector
    DvUsed   float64     // caller debits ActiveBurn.DVRemaining
    FuelUsed float64     // caller debits Craft.Fuel
    Ok       bool        // false on Kepler Newton failure → caller falls back
}
func StepCraft(in StepInput) StepOutput
```

This is the clean seam for the `ActiveBurn.DVRemaining` mutation that today
lives inside `stepThrust` (`world.go:275`). The physics layer never
touches sim-layer state.

**`internal/sim/integrator.go`** (new) — event-driven outer loop,
called by both live tick and predictor:

```go
type RunContext struct {
    State     physics.StateVector
    Primary   *bodies.Body
    System    *bodies.System
    Burn      *ActiveBurn        // nullable
    Nodes     []ManeuverNode     // sorted by TriggerTime
    StartTime time.Time
    Tuning    StepTuning
}
func Run(ctx RunContext, horizon time.Duration, emit func(Sample)) EndState
func selectMode(ctx RunContext, dt float64) Mode  // §3 gate matrix, pure
func timeToSOICrossing(ctx RunContext, dtMax float64) (float64, bool)
```

**`StepTuning`** centralizes the `period/100` cadence, the 256/1024 sub-step
caps, and the chunk-size formula — today these are duplicated across four
call sites with subtly different limits.

### Why these live where they do

`physics.StepCraft` is pure numerics, no `bodies` import → keeps the
existing dependency direction. `sim.Run` needs `bodies.System` for
`FindPrimary` and `ActiveBurn` for the burn lifecycle, so it has to live in
`sim`. Pulling `bodies` into `physics` would invert the layering for no
benefit.

### Phased PR sequence

Each PR leaves `main` green. Order matters: shared primitive first,
callers migrated one at a time, deletions only after migration.

| PR | Change | Deletes |
|----|--------|---------|
| **1** | Extract `physics.StepCraft` + `StepTuning`. Pure refactor — every existing loop delegates per-step numerics to `StepCraft` but keeps its own control flow. | nothing |
| **2** | Introduce `sim.Run` with chunked SOI loop (mirrors v0.4.4 `keplerStepWithSOICheck`). Rewrite `integrateSpacecraft` (`world.go:193`) as a ~10-line call to `Run`. | `canKeplerStep` (L316), `keplerStepWithSOICheck` (L345), `chunkDtCap` (L396), `maybeSwitchPrimary` (L432) |
| **3** | Rewire `NodeInertialPosition` and `PostBurnState` (`maneuver.go`) to call `Run`. | `propagateCraftWithPrimary` (`world.go:506`) |
| **4** | Rewire `PredictedSegments` (`predict.go:31`) to call `Run`. Body positions now recomputed per-step. | the Verlet sub-step loop inside `PredictedSegments` |
| **5** | Analytic `timeToSOICrossing` for Kepler segments (bisection on Kepler-step time, **not** closed-form). Ship behind `StepTuning.UseAnalyticSOI` flag so it can A/B against PR 2's chunked baseline via the coherence test. | the chunked SOI loop becomes the fallback path |
| **6** | Scope warp-clamp to burn segments only inside `Run`. Coast segments within the same tick run at full warp. | the whole-tick burn clamp in `clampedWarp` (`world.go:145`) |
| **7** | (optional) Adaptive predictor sampling around events. Only if PR 4 profiling shows uniform 96-sample density looks bad. | nothing |

After PR 4, the three independent Verlet+SOI loops are collapsed into one.
After PR 6, the doc's §3 / §9 #4 "warp clamp scoped to burn segments" is
delivered.

### Analytic SOI crossing (PR 5)

Use **linear body-motion approximation within one Kepler chunk** — matches
integration-design §10 open question. Over Sol's natural chunk window
(`min(simDelta, period/100)`), bodies move <1° angularly; linearization
error is O(ω²·dt²·R), tens of meters, well under SOI scales. Solve
`|r_craft(t) − r_body(t)|² = R²` by **bisection on Kepler-step time
(8–10 iters)**, not closed-form conic-sphere intersection. The closed
form is hundreds of LOC of universal-variable algebra for a benefit
(microsecond convergence) the player can't perceive.

### Critical files to modify

- `internal/physics/step.go` — **new**, pure primitive
- `internal/physics/soi.go` — `FindPrimary` (L32), `Rebase` (L66) stay; called from `Run`
- `internal/sim/integrator.go` — **new**, event loop + mode selection
- `internal/sim/world.go` — `Tick` (L115) and `integrateSpacecraft` (L193) shrink dramatically; `propagateCraftWithPrimary` (L506) deleted in PR 3
- `internal/sim/predict.go` — `PredictedSegments` (L31) becomes a thin sampler over `Run`
- `internal/sim/maneuver.go` — `NodeInertialPosition` (L446) and `PostBurnState` (L467) call `Run`; nothing else changes

### Functions to reuse (no rewrite)

- `physics.StepVerlet` (`verlet.go:14`), `physics.StepRK4` (`rk4.go:13`),
  `physics.KeplerStep` (`kepler_step.go:26`) — `StepCraft` dispatches to
  these unchanged.
- `physics.FindPrimary` (`soi.go:32`), `physics.Rebase` (`soi.go:66`) —
  unchanged; called from `Run`.
- `World.BodyPosition` (`world.go:90`), `World.bodyInertialVelocity`
  (`world.go:461`) — unchanged; called per-step from `Run` for SOI tests.
- `Spacecraft.ThrustAccelFn` (`spacecraft/thrust.go:145`) — wrapped into
  `ThrustSpec` for `StepCraft`.

## Comparison to other orbital simulators

Our proposed v0.5.0 design is mainstream practice, with one place where it
beats KSP1 and one place where it deliberately stays simpler than KSA.

**Kerbal Space Program 1.** KSP's solution to the same problem maps almost
exactly onto our integration-design contract:

- **On-rails Keplerian warp.** Above ~5× warp, the active vessel becomes
  "on rails" — purely analytic Kepler propagation, identical to our v0.4.3
  fast-path and our `ModeKepler` selection in §3.
- **Patched-conics solver** with a configurable lookahead
  (`Conic Patch Limit` 5–7 in `settings.cfg`) pre-computes future SOI
  transitions analytically. This is precisely what our PR 5
  `timeToSOICrossing` enables, just iterated forward N hops. Worth
  noting as a future extension: once PR 5 lands, the predictor can chain
  N events instead of stopping at the first one.
- **The bug we're avoiding** is documented: pre-1.2 KSP would skip SOIs
  under warp ("inaccurate encounters from warping through SOIs"); the
  community workaround was "warp slowly through SOIs at 1× speed". KSP1
  patched this by rails-to-physics transition smoothing, but the
  underlying step-and-check design still leaks accuracy at the highest
  warp tiers. Our PR 5 analytic event detection is **stricter than
  KSP1**: we step *to* the crossing time exactly, not past it.
- **No thrust under high warp.** KSP1 forbids on-rails warp while engines
  are firing — full stop. Our design is slightly more permissive: we
  allow warp during burn segments but clamp to 10× (PR 6 scopes that
  clamp to just the burn segment, not the surrounding tick). Equivalent
  numerical fidelity, better UX for short burns inside long-coast plans.

**Kerbal Space Program 2** (cancelled). The public dev posts described
deterministic warp as a multiplayer prerequisite. Same conclusion as our
v0.6 multiplayer design spike (state-of-game.md §3 v0.6) — the v0.5.0
event-driven loop with stateless Kepler is intrinsically deterministic,
which sets up multiplayer well without further work.

**Kitten Space Agency** (HarvesteR's spiritual successor, active 2026).
KSA explicitly does **physics during time warp** — engines, reaction
wheels, and structural forces all integrate at warp speed. They achieve
this on a custom engine (RocketWerkz BRUTAL) with double-precision
throughout and adaptive integration. We are **deliberately not following
KSA here**. Reasons:

- The integration-design §3 gate matrix forbids `Kepler` mode while
  thrust is active; instead we clamp warp to 10× and use RK4. This is
  the simpler, more conservative choice and matches KSP1.
- KSA's burns-under-warp need adaptive-step error control on RK4 with
  formal tolerance bounds. That's a separate work item (~500+ LOC, a
  whole new integrator family) that nothing in the current backlog
  needs.
- If a future user request demands "let me do a 30-day ion burn", the
  cleanest path is an opt-in fourth mode (`ModeAdaptiveRK4`) that
  slots into the same `selectMode` gate matrix. The PR-1 `StepCraft`
  abstraction already accommodates this — we'd add one mode constant
  and one dispatch arm.

**Children of a Dead Earth** (hard-realism reference point). CoaDE
rejects the patched-conic approach entirely — they use adaptive RK4 with
strict error tolerances and continuous collision detection across SOIs.
No drift under any warp factor, but every step costs ~10× more CPU than
Kepler analytic, and the architecture can't take advantage of two-body
closed-form math at all. We deliberately chose patched-conic + Kepler
fast-path for cost (a Kepler step is one Newton iteration; an adaptive
RK4 step is 4–6 force evals plus an error estimate) and scope
(state-of-game.md keeps n-body explicitly out of scope).

**Orbiter** (long-running freeware sim). Uses high-order symplectic
integrators with no on-rails warp at all. Maximum warp is bounded by
integrator step-size limits. Their accuracy is extreme but warp factors
top out around 10000×, where ours can plausibly reach 100000×+ via
analytic Kepler. Different tradeoff for a different audience.

### Where this leaves us

The v0.5.0 design is **closely modeled on KSP1's on-rails approach**,
**stricter than KSP1 on SOI crossings** (analytic event detection vs.
step-and-check), **less ambitious than KSA on burns-under-warp**
(deliberate; that's a v0.7+ conversation if it ever comes up), and
**deterministic in a way that sets up the v0.6 multiplayer design spike
without rework**.

The single biggest user-visible win over what KSP1 ships is in §3 PR 5
+ PR 6: under our design, planning a transfer through Mars's SOI at
100000× warp gives a predicted line that exactly tracks the live craft,
including across the SOI flip — KSP1's predicted line under those
conditions visibly drifts and the community's documented mitigation is
"warp slower". We're aiming for "warp does not require a workaround".

## Tests

New file `internal/sim/integrator_test.go`:

- `TestSelectModeGateMatrix` — table of `(thrust, dt/period, bound, foreignSOIProx)` covering all 16 corners of §3.
- `TestRunVerletKeplerAgreeBoundOrbit` — identical circular LEO, 10 orbits, force `ModeVerlet` vs `ModeKepler` via tuning override; `|Δr|/a < 1e-6`.
- `TestRunThrustMatchesTwoImpulse` — finite burn at zero duration matches an instantaneous impulse within RK4 truncation.
- `TestRunPredictorLiveCoherence` — extension of existing `TestIntegrateSpacecraftMatchesPredictorAcrossSOI` (`predict_test.go:121`) to three SOI crossings, using the same `Run` on both sides.
- `TestTimeToSOICrossingBisectionVsChunked` — PR 5 gate: analytic and chunked paths return crossing times within `StepTuning.Tolerance`.

Existing tests stay untouched and become cross-PR canaries:
`TestWarpLockPreservesCircularOrbit` (`predict_test.go:154`),
`TestWarpLockDetectsForeignSOIEntry` (`predict_test.go:195`),
`TestPredictedSegmentsContinuousAtSOIBoundary` (`predict_test.go:13`).

## Risks and mitigations

- **Predictor per-step body recompute** (PR 4 changes the
  snapshotting). 8 bodies × 96 samples × ~50 ns ≈ 38 µs per
  `PredictedSegments` call, recomputed every render frame. At 60 fps that's
  2.3 ms/s — negligible. If profiling later shows otherwise, add a
  per-frame body-position cache keyed by `Clock.SimTime`.
- **`period/100` heuristic fragmentation.** Today: `world.go:218`,
  `world.go:354`, `predict.go:59`, `propagateCraftWithPrimary`. PR 1
  centralizes into `StepTuning`. Single source of truth.
- **Burn state mutation crossing layers.** Resolved by
  `StepOutput.DvUsed/FuelUsed`; `sim.Run` applies them.
- **Sample density at high warp** (integration-design §10). PR 4 ships
  uniform sampling; PR 7 (adaptive) is gated on profiling.
- **Kepler Newton failure** near `e≈1`. `StepCraft` returns `Ok=false`;
  `Run`'s mode-selector falls back to Verlet for that step only.

## Explicitly not doing

- **No closed-form Kepler-conic / SOI-sphere intersection.** PR 5 uses
  bisection. Cost/benefit doesn't justify hundreds of lines of
  universal-variable algebra.
- **No warp-clamp logic inside `StepCraft` or `Run`.** Stays at the `Tick`
  layer in `clampedWarp`. Mixing UX clamps into the numeric layer breaks
  testability.
- **No save-format change.** Locked in integration-design §9 #5.
- **No unifying `clampedWarp`'s 10× burn cap with `StepTuning`.** They
  answer different questions: UX feel vs numeric stability.

## Verification

End-to-end sanity, in this order, after each PR:

1. `go test ./...` — all green.
2. `go test -run TestRunPredictorLiveCoherence -count=20` — coherence test
   stable across runs (catches RNG-flaky timing).
3. **Manual smoke test:** start a fresh save, plant a Hohmann to Mars
   (`P` after selecting Mars), warp to 100000×, watch the dashed
   predicted trajectory. Pre-rework: predicted line drifts off live
   craft as the trajectory crosses Mars's SOI. Post-rework: line and
   craft stay co-incident through the SOI crossing and into the
   capture orbit. Confirm `HUD.Primary` flips to "Mars" exactly when
   the predicted line shows it crossing the SOI.
4. **Cross-mode sanity:** with the `StepTuning.ForceMode` test hook,
   run identical 10-orbit LEO under `ModeVerlet` and `ModeKepler`;
   final `|r|` should match within 1e-6 of `a`.
5. **Burn segment scope (PR 6):** plant a 30-second prograde burn 5
   minutes ahead, set warp to 1000×, confirm the warp display drops
   to 10× only during the 30-second burn window, not the full tick
   containing it.
