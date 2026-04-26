# v0.6 implementation plan — terminal-space-program

## Context

`docs/v0.6-plan.md` defines the v0.6 cycle as seven slices spanning
planner UX (event-relative maneuver nodes), physics fidelity (finite-
burn-aware planner, moon → parent escape), input modernisation
(mouse), gameplay (missions), and architectural prep (multiplayer
design-doc). Eight scoping questions are already resolved in the doc.

This plan is the implementation companion: per-slice code anchors,
specific function/struct additions, and the sequencing graph that
keeps shared helpers landing in the right order. It is intentionally
overview-level (not per-slice deep-dive) so the user can scope-review
all of v0.6 in one read; per-slice deep plans can spin out from
individual sections as each release is started.

Total estimated work: ~1450 LOC + tests across seven releases.

---

## Slice plans

### v0.6.0 — burn-at-next scheduler (~250 LOC)

**Files modified:**

- `internal/sim/maneuver.go` — `ManeuverNode` struct + dispatch
- `internal/orbital/kepler.go` (or new `events.go`) — event-time helpers
- `internal/save/save.go` — `Node.Event` field + schema bump
- `internal/tui/screens/maneuver.go` — `m` form gains `fire at` field
- New tests in `sim/maneuver_test.go`, `orbital/kepler_test.go`,
  `save/save_test.go`

**Approach:**

1. Add `TriggerEvent` enum (`Absolute`, `NextPeri`, `NextApo`,
   `NextAN`, `NextDN`) and a new `Event TriggerEvent` field to
   `ManeuverNode` at `sim/maneuver.go:34`. Default zero value =
   `Absolute` so existing call sites stay correct.
2. Add event-time helpers in `internal/orbital`:
   - `TimeToTrueAnomaly(currentNu, targetNu, a, mu) float64` — generic
     time-to-anomaly via mean motion `n = √(μ/a³)` and Kepler's
     equation. Apo (ν=π) and peri (ν=0) call this with their fixed
     targets.
   - `TimeToNodeCrossing(state, mu, ascending bool) float64` — uses
     `ElementsFromState` (already at `kepler.go:65–163`) to get Ω,
     then time-to-true-anomaly at `Ω - ω` (AN) or `Ω + π - ω` (DN).
3. Lazy-freeze resolver: at `sim/maneuver.go:625` (top of
   `executeDueNodes` walk), if `node.Event != Absolute` and
   `node.TriggerTime` is zero/unset, compute and set `TriggerTime`
   from the live craft state, then re-sort `w.Nodes`.
4. `m` form changes (`internal/tui/screens/maneuver.go:27,33`):
   - Add `fireAtIdx int` field next to `modeIdx`.
   - Extend `focus` cycle from 0/1/2 to 0/1/2/3 (mode / fireAt / Δv /
     duration), updating modulo-4 logic at line 104–110.
   - Render fireAt as a cycle field using the same pattern as mode
     (line 113), with ←/→ to cycle the 5 values.
   - `BurnExecutedMsg` (line 38) gains `Event TriggerEvent` field;
     `app.go` emits it through to `World.PlanNode`.
5. Save: add `Event int` to `save.Node` (`save/save.go:80`); zero =
   Absolute. Bump `SchemaVersion` (line 28) from 1 → 2 and **relax
   the strict version check** at `save.go:162` to `if f.Version >
   SchemaVersion` so v1 saves still load (missing `Event` → zero =
   Absolute via JSON's permissive unmarshal). Same bump rides through
   to v0.6.5's `Missions` field.

**Reuse:** `orbital.ElementsFromState` (`kepler.go:65–163`) for the
elements extraction; `Periapsis`/`Apoapsis` helpers
(`kepler.go:166–170`) inside the elements struct; existing modulo-3
focus pattern in `maneuver.go:104–110`.

**Tests:** event-time helper unit tests (peri/apo/AN/DN against known
orbits); resolver test (plant a NextPeri node, advance Tick, verify
TriggerTime resolves and burn fires); v1 save round-trip test
(unmarshal old fixture into v2 schema).

---

### v0.6.1 — predicted post-burn orbit HUD (~150 LOC)

**Files modified:**

- `internal/orbital/kepler.go` — new public helper
- `internal/tui/screens/orbit.go` — new HUD block
- `internal/sim/predict.go` or `maneuver.go` — multi-node post-burn
  state chaining (only if needed)

**Approach:**

1. New `OrbitReadout(state StateVector, mu float64) OrbitReadout`
   helper in `internal/orbital`: returns `{ApoAlt, PeriAlt,
   AscNodeAngle, DescNodeAngle}`. Wraps `ElementsFromState` +
   `Apoapsis`/`Periapsis` + Ω (already extracted by elements call).
   Same helper used by v0.6.0's resolver and this HUD readout.
2. Compute predicted post-burn state by chaining
   `PostBurnState` (`sim/maneuver.go:675`) calls — feed each node's
   output as the next node's initial state. The HUD reflects the
   orbit *after the last planted node fires* (clean semantics; no
   per-node ambiguity).
3. HUD render: append a new section after the planned-nodes block
   (`screens/orbit.go:432`). Use the existing v0.5.13 divider pattern
   (`section()` with `─` rule, four labelled rows for AP/PE/AN/DN).
   Section header: "PROJECTED ORBIT". Hide the section entirely
   when `len(World.Nodes) == 0`.

**Reuse:** `PostBurnState` (`sim/maneuver.go:675–696`) for chained
state propagation; v0.6.0's `OrbitReadout` helper; v0.5.13's
`section()` divider pattern (`screens/orbit.go:412`).

**Tests:** orbit-readout unit tests (known elliptical orbit →
expected AP/PE/AN/DN); HUD render snapshot test if we have one.

---

### v0.6.2 — finite-burn-aware iterative planner (~250 LOC)

**Files modified:**

- New `internal/planner/finiteburn.go` — Newton iterator
- `internal/planner/transfer.go` — optional flag on
  `PlanIntraPrimaryHohmann` to route through the iterator
- New `internal/planner/finiteburn_test.go`

**Approach:**

1. New `IterateForTarget(initial TransferNode, mu float64, vesselMass,
   thrust, isp float64, predicate func(StateVector) bool, maxIter
   int) (TransferNode, error)` in
   `internal/planner/finiteburn.go`.
2. Each iteration:
   - Builds an `accelFn(r, v, t) Vec3` closure that adds gravity
     (via `physics.GravityOnly(mu)`) plus thrust (in node's burn
     mode direction, magnitude `thrust/(mass at t)` falling with
     mass flow `dm/dt = -thrust/(Isp·g0)`).
   - Calls `physics.StepRK4` (`physics/rk4.go:13–46`) for `Duration /
     N` sub-steps to integrate the candidate Δv burn.
   - Evaluates `predicate(finalState)` — typical predicate: "apoapsis
     within 0.1% of target" using `orbital.ElementsFromState(...).Apoapsis()`.
   - Newton-corrects Δv (or duration, depending on what's free).
3. Hook into `PlanIntraPrimaryHohmann` (`planner/transfer.go:154`):
   add an optional `finiteBurnIter bool` parameter (or a separate
   `PlanIntraPrimaryHohmannFinite` wrapper). When true, the impulsive
   plan is run through `IterateForTarget` to get a Δv that delivers
   the target apoapsis under finite-burn deformation.

**Reuse:** `physics.StepRK4` and `physics.GravityOnly`
(`physics/rk4.go:13,50`); `spacecraft.DirectionUnit` for burn-mode
directions; existing `TransferNode`/`TransferPlan` types
(`planner/transfer.go:25–41`).

**Decoupling:** the iterator never touches `World` state. It runs
purely on `StateVector` + closures, so the planner can iterate
candidates without affecting the live craft or active burn.

**Tests:** iterator convergence test (give a low-TWR loadout, target
apo, verify post-burn state matches); divergence handling (max-iter
cap, error path).

---

### v0.6.3 — moon → parent escape transfer (~300 LOC)

**Files modified:**

- `internal/sim/maneuver.go` — new dispatch branch in `PlanTransfer`
- New `internal/planner/transfer.go` `PlanMoonEscape` function
- New `internal/planner/moon_escape_test.go`

**Approach:**

1. Dispatch: in `sim/maneuver.go` `PlanTransfer`, insert new branch
   *before* the heliocentric fallthrough at line 159, mirroring
   v0.5.7's dispatch at line 122. Condition: `target.ID ==
   w.Craft.Primary.ParentID`. Calls new `PlanMoonEscape(...)`.
2. New `PlanMoonEscape(muMoon, rPark, moonSoiRadius, muParent, ...)`
   in `planner/transfer.go`:
   - Two-impulse plan. Burn 1: prograde at moon-orbit periapsis,
     Δv sized so apolune ≥ moon SOI radius. Use existing
     `EscapeBurnDeltaV` math (already imported in `sim/maneuver.go`).
   - Burn 2: zero-Δv placeholder node in parent frame at the
     SOI-exit moment. Marks the frame transition for the HUD; the
     player plants their own circularization manually.
   - Phasing: lift logic from `PlanIntraPrimaryHohmann`
     (`planner/transfer.go:154`) — wait until the moon's orbital
     phase puts the post-escape trajectory dropping where the player
     wants. v0.6.3 uses a simple "next periapsis" model, leaning on
     v0.6.0's `NextPeri` trigger.
   - Returns a standard `TransferPlan{Departure, Arrival, TransferDt}`.
3. Composes with v0.6.2 via the same flag — long escape burns on
   low-TWR loadouts route through the iterative planner.

**Reuse:** `EscapeBurnDeltaV` (`planner/transfer.go`); phasing logic
from `PlanIntraPrimaryHohmann`; v0.6.0's `NextPeri` trigger event.

**Tests:** Luna → Earth escape plan (verify Δv matches manual v∞
calc); SOI-exit moment in parent-frame node lines up with
forward-integrated state; phasing test (escape moment varies
appropriately with starting craft anomaly).

---

### v0.6.4 — mouse selection (~200 LOC)

**Files modified:**

- `cmd/terminal-space-program/main.go` — enable mouse capture
- `internal/tui/app.go` — `tea.MouseMsg` dispatch
- `internal/tui/widgets/canvas.go` — pixel-tag widening + reverse
  projection
- `internal/tui/screens/orbit.go`, `porkchop.go`, `maneuver.go` —
  per-screen mouse handlers + `LoadNode` for pre-population
- New tests in `widgets/canvas_test.go` for `Unproject` round-trip

**Approach:**

1. Enable mouse: `cmd/terminal-space-program/main.go:25` — add
   `tea.WithMouseAllMotion()` alongside `tea.WithAltScreen()`.
2. App dispatch: `internal/tui/app.go:88` — add `case tea.MouseMsg:`
   block routing clicks to the active screen's mouse handler.
3. Canvas reverse-projection (`widgets/canvas.go`):
   - Widen the pixel-tag map from `map[[2]int]lipgloss.Color` to
     `map[[2]int]CellTag` where `CellTag {Color, BodyID, NodeID, IsVessel}`.
   - Existing `FillColoredDisk`/`RingColoredOutline` plotters add
     `BodyID`; the vessel arrow glyph plotter adds `IsVessel`; node
     glyph plotter (new — currently nodes aren't drawn on canvas)
     adds `NodeID`.
   - Add `Unproject(px, py int) (world Vec3)` — inverse of `Project`
     at `canvas.go:237–245`.
   - Add `HitAt(px, py int) CellTag` — direct map lookup; nearest-
     neighbour search within ±2 cells if exact miss.
4. Per-screen handlers:
   - **Orbit canvas** (`screens/orbit.go`): click on tagged cell with
     BodyID → focus that body; on vessel → focus craft; on node →
     open `m` pre-populated via new `Maneuver.LoadNode(*ManeuverNode)`
     method (sets `modeIdx`, `dvInput`, `durInput`, `fireAtIdx` from
     the node). Click on empty cell → `Unproject` to world coords,
     project onto live craft orbit (find nearest ν via existing
     `orbital` math), open `m` with `fire at` set to the projected
     ν (or `Absolute` with `T+ = (resolved time)` if none of NextPeri/
     NextApo/AN/DN match).
   - **Porkchop** (`screens/porkchop.go:73–105`): click on grid cell
     → reverse the render layout (lines 157–168) to map cell coord
     to `(depDayIdx, tofIdx)`, set `selDep`/`selTof`, validate
     feasibility, open `m` pre-populated with the cell's transfer
     parameters via `LoadNode`.
5. New `Maneuver.LoadNode(*ManeuverNode)` method
   (`screens/maneuver.go`) for pre-population. Set focus to mode
   field when opened via mouse click.

**Reuse:** existing pixel-tag infrastructure (`canvas.go:41,49`);
existing porkchop cell layout (`screens/porkchop.go:157–168`);
existing `BurnExecutedMsg` flow.

**Tests:** `Unproject(Project(v))` round-trip; `HitAt` returns
correct tag for known body positions; click-to-load-node flow
populates the form correctly.

---

### v0.6.5 — mission scaffold (~250 LOC + JSON)

**Files modified:**

- New `internal/missions/missions.go`, `missions_test.go`
- New `internal/missions/missions.json`
- `internal/save/save.go` — add `Payload.Missions`
- `internal/sim/world.go` and `tick.go` — per-tick evaluator hook
- `internal/tui/screens/orbit.go` — HUD status line

**Approach:**

1. New `internal/missions/` package:
   - `type Mission struct { ID, Name string; State MissionState;
     Predicate func(*sim.World) bool }` — `State` is `Pending |
     InProgress | Passed | Failed`.
   - JSON-loaded mission catalog in `missions.json`. Predicates are
     hard-coded Go closures keyed by mission ID (data-driven
     parameters: target altitudes, target body IDs, etc.).
   - 2–3 starters: "Circularize at 1000 km LEO ±5%" (`e ≤ 0.005 over
     a full revolution`), "Luna orbit insertion" (`craft.Primary ==
     Luna AND apo within Luna SOI`), "Mars SOI flyby" (`any tick
     where craft.Primary == Mars`).
   - `Evaluate(world *World)` — runs each active mission's predicate;
     transitions state.
2. Save: add `Missions []Mission` to `Payload`
   (`save/save.go:41–50`) — rides v0.6.0's v1→v2 schema bump. JSON
   permissive unmarshal handles missing field (v1 saves load with
   empty slice).
3. World hook: `Tick` (`sim/world.go:159`) calls
   `world.Missions.Evaluate(world)` once per tick. Cheap predicates
   keep this lightweight.
4. HUD: one-line status under the active-burn block in
   `screens/orbit.go` showing the current mission's name + state.

**Reuse:** v0.6.0's `OrbitReadout` for "circularize" predicate (e and
altitude reads); existing `World` accessors for craft.Primary and
state.

**Tests:** each starter mission's predicate (positive and negative
cases); save round-trip with missions present; tick evaluator
transitions state correctly.

---

### v0.6.6 — multiplayer design-doc spike (~600–1000 words)

**Files added:**

- `docs/multiplayer-design.md`

**Sections:**

- **Context** — why this doc, what it's for.
- **Transport** — WebSocket vs QUIC vs custom UDP; latency / NAT
  trade-offs.
- **Authority model** — host-authoritative vs lockstep; implications
  for time-warp coordination (the open question — warp is a global
  multiplier, hard to desync).
- **Persistence** — how shared sessions fit the v0.4 save schema.
  Host-authoritative snapshot vs per-player envelope, conflict
  resolution on rejoin, whether a `session` block sits inside or
  alongside `Payload`.
- **Out of scope** — implementation roadmap, target release.
  Constraints only, no schedule.

No code changes. Pure design doc.

---

## Sequencing & dependencies

```
v0.6.0 (scheduler) ──┬──> v0.6.1 (predicted HUD) — shares OrbitReadout helper
                     ├──> v0.6.2 (finite-burn iter) ──> v0.6.3 (moon escape, optional via flag)
                     ├──> v0.6.3 (moon escape) — uses NextPeri trigger
                     └──> v0.6.4 (mouse) — needs LoadNode + Event in BurnExecutedMsg
v0.6.5 (missions)    ─── independent (rides v0.6.0's schema bump)
v0.6.6 (MP doc)      ─── independent
```

**Recommended ship order:** 0 → 1 → 2 → 3 → 4 → 5 → 6.

The two non-obvious shared artifacts:
1. **Orbital-elements helper** (`OrbitReadout` + event-time math)
   ships in v0.6.0 and gets a second consumer in v0.6.1.
2. **Schema v1→v2 bump** lands in v0.6.0 (Event field), gets
   reused in v0.6.5 (Missions field). The relax-strict-version-check
   work happens once.

---

## Critical files to modify

| File | Slices | Change summary |
|---|---|---|
| `internal/sim/maneuver.go` | 0, 3 | `ManeuverNode.Event` field, resolver in `executeDueNodes`, `PlanMoonEscape` dispatch |
| `internal/orbital/kepler.go` | 0, 1, 2 | Event-time helpers, `OrbitReadout` |
| `internal/save/save.go` | 0, 5 | Schema v1→v2, relax version check, `Node.Event`, `Payload.Missions` |
| `internal/tui/screens/maneuver.go` | 0, 4 | `fire at` cycle field, `LoadNode` |
| `internal/tui/screens/orbit.go` | 1, 4, 5 | "PROJECTED ORBIT" block, mouse handler, mission HUD line |
| `internal/tui/widgets/canvas.go` | 4 | `CellTag` widening, `Unproject`, `HitAt` |
| `internal/tui/screens/porkchop.go` | 4 | Mouse handler, click-to-grid mapping |
| `internal/tui/app.go` | 4 | `tea.MouseMsg` dispatch |
| `cmd/terminal-space-program/main.go` | 4 | `tea.WithMouseAllMotion()` |
| `internal/planner/finiteburn.go` (new) | 2 | `IterateForTarget` |
| `internal/planner/transfer.go` | 2, 3 | Iteration flag, `PlanMoonEscape` |
| `internal/missions/` (new pkg) | 5 | Whole package |
| `internal/sim/world.go`, `tick.go` | 5 | Mission evaluator hook |
| `docs/multiplayer-design.md` (new) | 6 | The doc |

---

## Verification

Per slice:

- **v0.6.0:** `go test ./internal/orbital/... ./internal/sim/... ./internal/save/...` — event-time helpers, resolver, schema bump. Manual: open `m`, cycle `fire at` to NextPeri, plant a 100 m/s prograde, verify HUD shows the node firing at the next periapsis.
- **v0.6.1:** `go test ./internal/orbital/...` for OrbitReadout. Manual: plant a Hohmann transfer with `H`, verify HUD's "PROJECTED ORBIT" block matches the predicted dashed trajectory's apo/peri.
- **v0.6.2:** `go test ./internal/planner/...`. Manual: switch the default vessel to a low-TWR loadout (revive ICPS via test-only flag if needed), run intra-primary auto-plant with the iterator flag on, verify delivered apo within 0.1% of target.
- **v0.6.3:** `go test ./internal/planner/...`. Manual: insert craft into Luna orbit (via save edit or test fixture), run `PlanTransfer` with target=Earth, verify two-burn plan, advance through the escape burn, watch SOI transition into Earth frame, verify perigee altitude is "what you got."
- **v0.6.4:** `go test ./internal/tui/widgets/...` for Unproject round-trip. Manual: click on Mars in orbit canvas → focus snaps; click porkchop cell → planner opens with that transfer; click on orbit ellipse → planner opens with new node staged at that ν.
- **v0.6.5:** `go test ./internal/missions/... ./internal/save/...`. Manual: launch from LEO, circularize at 1000 km, watch mission state transition to Passed in HUD.
- **v0.6.6:** read-through review. No code to test.

**Cross-slice:** end-to-end Earth → Luna mission with mouse: click Luna → focus, `H` → plant Hohmann, click on orbit at periapsis → adjust burn, verify HUD's projected orbit reflects the change, mission "Luna orbit insertion" passes when LOI fires.

**Build matrix:** existing `go test ./...` must pass on each PR; GoReleaser matrix (linux/darwin/windows × amd64/arm64) sanity-checks per release tag.

---

## Open questions for next cycle

These surface during v0.6 work and are flagged in `docs/v0.6-plan.md`:

1. v0.5 `Body` schema for v0.7 config-file loader — how does the
   v0.5 hierarchy expose itself to an external loader.
2. Moon-escape placeholder arrival node in HUD — how does the SOI-
   exit moment surface? May need a small HUD design pass during
   v0.6.3.
3. Whether v0.6.2's iterator gets a caller-facing UX toggle ("plan
   with finite-burn iteration" in the `m` form) or stays internal-
   only. Internal-only by default; revisit if low-TWR loadouts
   become a regular use case.
