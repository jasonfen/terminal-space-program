# 0008 — Parachutes for atmospheric descent + recovery

<!-- llm-parse: adr=0008 status=accepted date=2026-05-30 cycle=v0.12 slice=parachutes -->

**Status:** accepted (2026-05-30, `/grill-with-docs` for v0.12
Slice 3).
**Extends:** [`docs/adr/0004-crashed-landed-lifecycle.md`](0004-crashed-landed-lifecycle.md)
— ADR 0004's "Forward hooks" explicitly banked this: a deployed
parachute is *"a non-engine path to `CanSoftLand` semantics — a
deployed parachute on a vessel without `CanSoftLand` can still qualify
for the Touchdown predicate via aerodynamic deceleration."* This ADR
cashes that hook. Also extends the v0.8.4 exponential-ρ(h) drag model
(`internal/physics/drag.go`) and the v0.9.1+ staging machinery
(`internal/sim/staging.go`, the Stage / `space` action) that ADR 0007
generalised.

## Context

Capsule-class vessels — historically the dominant parachute users —
have no controlled path to the surface today. The Crashed/Landed
lifecycle (ADR 0004) gates a soft **Touchdown** on `CanSoftLand`, a
per-Stage catalog capability carried only by stages designed to land
*under thrust* (the LM-derived `lander` descent stage, the Falcon 9
`f9-s1`). A re-entering capsule — the Apollo `csm` core that survives
the Apollo Stack decouple chain — has `canSoftLand: false` and so
**Crashes on contact regardless of how gently it arrives**, because it
has no engine route to bleed off descent velocity.

The real-world answer is aerodynamic: a parachute trades a deploy
event for a massive drag-area increase, dropping terminal velocity
below survivable. The simulator already has every substrate piece —
the exponential-density drag model resolves a per-craft **Ballistic
Coefficient** (`EffectiveBallisticCoefficient()`, reading
`Stages[0].BallisticCoefficient`), and the surface-arrival predicate
(`classifySurfaceArrival`, `internal/sim/lifecycle.go`) already
classifies a contact as Landed-or-Crashed on a velocity + capability +
attitude test. What's missing is (a) a way to dramatically raise BC on
a player action, (b) a runtime deploy state, and (c) a second,
non-engine route into the Landed branch of the predicate.

This is the L item of the v0.12 cycle and the natural split candidate
(see `docs/v0.12-plan.md` §Slice 3); it ships as its own tag.

## Decision

A **Parachute** is a per-Stage *capability* plus a per-Vessel runtime
*deploy state*. A deployed chute raises the vessel's effective
**Ballistic Coefficient** enough that terminal velocity falls below
`V_CRIT`, and qualifies the vessel for **Touchdown** even without
`CanSoftLand`. The whole subsystem is additive and `omitempty` — **no
`SchemaVersion` bump** (same posture as ADR 0004's lifecycle flags and
ADR 0007's `DecouplePlan`).

### 1. Data model — capability vs state

Mirror the existing `CanSoftLand` split exactly:

| Concern | Where | Notes |
|---|---|---|
| **Capability** `HasParachute bool` | per-`Stage` (catalog) | Set in `StageCatalog`; `omitempty`. `SyncFields` re-derives a `Spacecraft.HasParachute` mirror from `Stages[0]` on every staging / dock / load — identical to `CanSoftLand` (`stage.go:260`). Rides the hardware across decouple. |
| **Runtime state** `ChuteState` | on `Spacecraft` | Lives alongside `Landed` / `Crashed` (the other surface-lifecycle runtime flags). `omitempty` (zero value = `ChuteStowed`). |

`ChuteState` is a small enum:

```
ChuteStowed   = iota  // zero value; capability present but not staged
ChuteArmed            // staged; waiting for enough air to inflate
ChuteDeployed         // inflated; BC bump active
```

There is **no torn / failure state** (see Alternatives → tear model).

### 2. State machine

```
STOWED ──(Stage action)──▶ ARMED ──(q ≥ ChuteDeployQMin)──▶ DEPLOYED
```

All transitions one-way; `DEPLOYED` is terminal (no re-stow / cut-away).

- **Arm** is folded into the existing **Stage** (`space`) action — it
  is "just another staging action," KSP-style, **not a new keybinding**.
  Because the parachute always rides the *surviving top stage* (the
  capsule), and `space` pops the *bottom* stage, arming becomes the
  **final staging action**: once the vessel is reduced to its bare
  chute-bearing stage, the next `space` press arms the chute instead of
  the single-stage no-op flash. Arming is allowed in any conditions,
  including vacuum — "arm on the way down and forget it."
- **Auto-deploy** fires the first tick the vessel's dynamic pressure
  `q = 0.5 · ρ · |v_rel|²` reaches `ChuteDeployQMin`. `ρ` and `v_rel`
  are exactly the quantities `DragAccel` already computes; deploy
  resolves to `ChuteDeployed` and the BC bump engages. If `q` is
  already over the floor when the player arms (deep in thick air), the
  same tick carries `ARMED → DEPLOYED`.

### 3. Ballistic-coefficient bump (the drag model)

`EffectiveBallisticCoefficient()` short-circuits when deployed:

```go
func (s *Spacecraft) EffectiveBallisticCoefficient() float64 {
    if s.ChuteState == ChuteDeployed {
        return ChuteDeployedBC          // canopy swamps the capsule's own drag
    }
    // ... existing Stages[0] → legacy field → DefaultBallisticCoefficient chain
}
```

**Absolute replace, not a multiplier.** The deployed value is a fixed
const, not `stageBC × N`. The canopy area physically dominates the
capsule's own drag, so the base BC is irrelevant; an absolute value
makes terminal velocity **predictable and mass-independent** (the mass
term is already inside `BC = C_D·A/m`, so fixing BC fixes `v_term`
across every capsule). Starting value `ChuteDeployedBC = 0.3 m²/kg`,
from the terminal-velocity relation of the existing drag model
(`drag = gravity`):

> `v_term = √(2g / (ρ · BC))` → at Earth ρ₀ ≈ 1.2, g ≈ 9.81 →
> `v_term ≈ 7.4 m/s`, comfortably under `V_CRIT = 10 m/s`.

Retunable from playtest, like `V_CRIT` / `NOSE_TOL`.

### 4. Touchdown predicate — the second non-engine route

`classifySurfaceArrival` gains a parallel route. Two clean branches,
each with its own gates:

```
engine route (unchanged):  CanSoftLand   && |V_inertial| < V_CRIT && cosNose > NOSE_TOL
chute route (new):         ChuteDeployed && |v_rel|      < V_CRIT          // nose waived
```

The chute route **waives the `NOSE_TOL` nose-alignment gate.** Under a
canopy the chute is the stabiliser, the player is not actively flying
attitude, and demanding a specific nose angle for a *passive* descent
is the artificial part. The engine route keeps its nose check
untouched (a Falcon 9 thrusting in 60° off-vertical is still a crash).

**Velocity frame — amended after Slice 3 verification.** The chute
route measures **air-relative** speed `v_rel = |V − ω×r|`, *not*
inertial `|V|` like the engine route. This deviates from this ADR's
original frozen spec (which reused the engine route's inertial `v`) and
was caught by flying the actual descent: a parachute nulls the vessel's
velocity *relative to the co-rotating atmosphere*, so on a fast-rotating
body a perfectly-descending capsule still carries the surface
co-rotation velocity in the inertial frame (~465 m/s at Earth's
equator). The inertial `V_CRIT` test crashed every Earth splashdown
despite an ideal canopy descent (`|v_rel|` converged to ~7.6 m/s while
`|V_inertial|` held ~426 m/s all the way down). The engine route keeps
inertial `|V|` — its shipped tests pin that, a powered lander on the
slow-rotating Moon sees a negligible `ω×r`, and the parachute is the
*first* Earth-landing feature to exercise the fast-rotation regime
ADR 0004's inertial predicate never hit. `ω` is `physics.AtmosphereOmega`
— the same spin vector the drag model uses, so the chute route's frame
agrees with the air the canopy is actually braking against.

### 5. Thresholds — global consts

`ChuteDeployQMin` (Pa, ~1000 starting) and `ChuteDeployedBC` (m²/kg,
0.3 starting) are package-level consts beside `CrashVCritMps` /
`CrashNoseTol`, **not** per-craft catalog fields. One chute spec to
start; per-craft rating is a banked refinement (see Forward hooks).
The deploy gate is expressed in dynamic pressure `q`, which is
body-agnostic — no body-specific deploy-altitude constant.

### 6. Loadouts

- Flag the `csm` `StageCatalog` entry `hasParachute: true` — the
  Apollo arc earns a real Earth splashdown (the CSM survives to
  re-entry, deploys, soft-lands).
- Add a minimal directly-spawnable **re-entry capsule** loadout — a
  single command-module-class stage carrying `HasParachute` (and *not*
  `CanSoftLand`). This is the clean isolated test vehicle the plan's
  "capsule-class vessel" language points at; the CSM is only reachable
  via the full Apollo Stack → orbit → four decouples, which is too slow
  to iterate on.

### 7. Render

- **HUD readout (load-bearing):** chute state (`STOWED / ARMED /
  DEPLOYED`) + descent rate, so the player can watch `v_term` settle
  under `V_CRIT`. Descent happens in OrbitView and ADR 0004 deferred a
  dedicated `ViewLanding`, so the HUD is the player's only window onto
  the chute.
- **ViewLaunch canopy sprite:** a synthetic braille canopy painted
  above the top stage when `DEPLOYED`, reusing the established
  synthetic-geometry seam that already paints the engine bell, landing
  legs, and flame (`CONTEXT.md` "Launch Sprite"). Gives the chute a
  visual identity for the `Shift+V` manual-jump and the test-lob cases.
- OrbitView glyph swap is **deferred** — the OrbitView glyph is the
  craft's identity marker and swapping it muddies which-craft-is-which.

## Alternatives considered

### Tear / over-speed-failure model

- **Tear on over-speed deploy (q > q_max → torn, terminal, no drag).**
  The original handoff framing and the first three grill answers built
  toward this (a `TORN` state, a `ChuteTearQMax` const, an over-speed
  punishment). **Rejected** during the grill. The auto-deploy timing
  made tearing either unreachable or warp-fragile: a chute that
  auto-deploys at the `Cutoff Altitude` edge sees `q ≈ 0` (the air is
  vanishingly thin there) and never tears; pushing the deploy deeper to
  make tearing reachable required a second density/altitude threshold
  and a `SEMI-DEPLOYED` reefed stage to model the inflation transient —
  a much larger state machine on an already-L slice. The designer's
  call: **"take tear checks out and deploy at minimum q."** KSP stock
  chutes are likewise forgiving; the skill the feature rewards is
  managing re-entry *geometry* (don't arrive so steep you're still
  supersonic at chute altitude), not nailing a deploy-speed window.
- **Arm + reefed/semi-deployed stage (full KSP fidelity:
  STOWED→ARMED→SEMI→DEPLOYED with a ramped BC).** Most realistic
  descent profile. Rejected: largest state machine + a ramped-BC model,
  the biggest scope on the split-candidate slice. Bank if a future
  cycle wants descent-profile fidelity.

### Keybinding

- **A new dedicated key (`p` for parachute), single press, no
  confirm.** The initial recommendation. Superseded by the designer's
  "like KSP, it's just another staging action" — fold deploy into the
  existing Stage (`space`) action, no new binding, reuse the staging
  muscle memory and the ADR 0007 staging-sequence machinery.

### Predicate nose-alignment on the chute route

- **Keep `NOSE_TOL` uniform** (chute substitutes only for the
  `CanSoftLand` gate). Rejected: a capsule left at a re-entry attitude
  would Crash despite a perfect chute — a non-obvious failure unrelated
  to the chute.
- **Auto-orient nose-up on deploy** (write `CurrentAttitudeDir` to
  local-up). Rejected: realistic but costs an attitude write that
  interacts with slew / InstantSAS, for no gameplay gain once the gate
  is simply waived.
- **Waive `NOSE_TOL` on the chute route. Chosen.** Cleanest
  player-facing rule and zero blast radius on the engine route.

### BC-bump model

- **Multiplicative (`stageBC × N`) / additive (`stageBC + term`).**
  Rejected: terminal velocity then varies per-capsule with base BC, so
  guaranteeing `v_term < V_CRIT` across loadouts needs per-capsule
  tuning. The chute term dominates anyway, so additive converges on the
  absolute model with extra arithmetic. **Absolute replace chosen.**

### Capability placement

- **Loadout-level / Spacecraft-level `HasParachute`** (mirroring
  v0.11.4's *first* `CanSoftLand` cut). Rejected: ADR 0004 records that
  the loadout-level flag broke the Apollo decouple flow — a capability
  that can't ride a stage into a freshly-spawned slate craft. **Per-
  Stage capability + Spacecraft runtime state chosen**, matching how
  `CanSoftLand` / `BallisticCoefficient` already work.

### Save migration

- **Bump `SchemaVersion`.** Rejected, same reasoning as ADR 0004 /
  0007: every new field has a safe `omitempty` default (`ChuteStowed`
  zero value, `HasParachute` false), so a pre-Slice-3 save loads with a
  stowed, capability-less chute — correct for any vessel saved before
  this slice. **No bump.**

## Consequences

**Positive.**

- The Apollo `csm` becomes recoverable — the marquee Apollo arc gets a
  real Earth splashdown, closing the loop the LM landing opened.
- A directly-spawnable capsule makes the whole subsystem testable in
  isolation (one spawn, a de-orbit, a `space` press) — matching the
  playtest-in-flying-the-game-language workflow.
- The "second non-engine route into Landed" that ADR 0004 banked is
  now real and localised to one extra branch in
  `classifySurfaceArrival`.
- `EffectiveBallisticCoefficient()` stays the single BC seam; the chute
  is one short-circuit at the top, no new call sites in the drag path.

**Negative / trade-offs to live with.**

- **Instant inflation.** Dropping the reefed/semi stage means BC jumps
  `0.01 → 0.3` in a single tick, a sharp deceleration spike rather than
  a smooth inflation. Mitigation: drag opposes velocity and cannot
  reverse it within a small `dt`, and final descent is a low-warp
  regime; but a player auto-deploying under very high warp could
  overshoot. Watch in playtest — if it bites, the reefed stage is the
  banked fix.
- **Mass-independent `v_term`.** The absolute-BC model lands an 11,900
  kg CSM and a 1-ton capsule at the same 7.4 m/s — physically a chute
  "sized to the craft." Acceptable game simplification; the per-craft
  `ChuteDeployedBC` refinement (banked) restores mass sensitivity if
  wanted.
- **Two capability flags on the surface predicate.** `CanSoftLand` and
  `HasParachute`/`ChuteDeployed` now both gate Landed, via two
  branches. A future maintainer must keep the engine route's
  nose-alignment check and the chute route's waiver distinct.
- **Two velocity frames on the surface predicate** (Slice 3 verification
  amendment, see Decision §4). The engine route tests inertial `|V|`;
  the chute route tests air-relative `|v_rel| = |V − ω×r|`. The split is
  load-bearing — collapsing it back to one frame re-breaks Earth
  splashdown (inertial) or shipped Falcon-9/Moon soft-lands (air-
  relative). The latent inertial-vs-air-relative tension lives in the
  engine route too (a powered Earth-equator landing arrives at ~465 m/s
  inertial), but it is invisible today: powered landers fly the
  slow-rotating Moon, where `ω×r` is ~4.6 m/s. If a powered atmospheric
  Earth landing ever becomes a real arc, the engine route needs the same
  air-relative treatment — banked, not done here.
- A `Spacecraft.HasParachute` mirror joins the `CanSoftLand` /
  `Landed` / `Crashed` flag cluster; the SyncFields re-derive list
  grows by one.

## Forward hooks

- **Per-craft chute rating.** `ChuteDeployQMin` / `ChuteDeployedBC` as
  per-Stage catalog fields, for a heavy-vs-light or drogue-vs-main
  chute distinction. Restores mass-sensitive `v_term`.
- **Reefed / semi-deployed stage.** A `ChuteSemiDeployed` intermediate
  with a ramped BC, if instant inflation reads wrong in playtest.
- **CM/SM split.** Modelling the Apollo CSM as a two-part stack so the
  Service Module jettisons before re-entry (the chute rides the bare
  Command Module). Larger catalog change; out of scope here.
- **Dedicated `ViewLanding` ViewMode.** Still banked from ADR 0004;
  the chute's descent-rate HUD read is one more signal toward it.
