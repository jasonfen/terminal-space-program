# 0004 — Crashed / Soft-Landed vessel lifecycle

<!-- llm-parse: adr=0004 status=accepted date=2026-05-26 cycle=v0.11 slice=v0.11.4 -->

**Status:** accepted (2026-05-26, `/grill-with-docs` for v0.11.4).
**Supersedes:** the v0.8.5 placeholder semantics in
`internal/physics/surface.go` and the "intended outcomes, not yet
differentiated" caveat in `CONTEXT.md`'s **Touchdown / Crash**
glossary entry.

## Context

Since v0.8.5, `physics.ClampToSurface` has been the placeholder
handler for any sub-step where a Vessel's `|R|` drops below the
Primary's mean radius. It projects R back to the surface along r̂
and zeros V. It does **not** set `Landed=true` — the post-clamp
Vessel sits with V=0, `Landed=false`, and gravity pulls it back below
the surface each subsequent tick; `ClampToSurface` re-fires
indefinitely.

`CONTEXT.md` codifies this as the **Surface Contact** glossary entry
and pre-declares the eventual outcomes — **Touchdown** (controlled,
becomes Landed) and **Crash** (destructive) — without the code to
back them. The v0.8.5 comment is explicit: "real 'crashed' status
with destruction / structural damage is deferred to v0.9+."

v0.11.4 closes that gap. Two real consumers are arriving in the same
slice: a Lander vessel kind (Apollo-LM-style) and a Falcon 9 first
stage with retro-burn-to-touchdown recovery. Without the differentiated
lifecycle, neither is playable as designed — the booster can't return
to the pad; the lander can't earn a Landed state through controlled
descent.

A second, smaller motivation: the v0.11.0 ViewLaunch route handler
fires on every active-vessel `Landed=false→true` transition. Today
that's only Launchpad spawn (the only `Landed=true` site). If
soft-land also sets `Landed=true`, the handler would fire again on
touchdown — rip the player into ViewLaunch with empty trail and
fresh T+, mid-landing. We need the route handler to disambiguate
"fresh launchpad spawn" from "post-flight soft land."

## Decision

Adopt a four-flag lifecycle on `Spacecraft`, all `omitempty` so no
save-schema bump is required:

| Flag | Set by | Cleared by | Meaning |
|---|---|---|---|
| `Landed` | Launchpad spawn; soft-land predicate | Engine ignition (Manual Burn or planted node fire) | Vessel co-rotates with the ground; integrator runs `integrateLanded` bypass |
| `Crashed` | Crash predicate | End-flight removal (Vessel leaves the world) | Vessel is destroyed/disabled; integration skipped; rendered dimmed |
| `OnPad` | Launchpad spawn | First `Landed=false` transition (first liftoff) | Distinguishes a Vessel awaiting first ignition from any other Landed-true Vessel |
| `CanSoftLand` | Catalog (Vessel kind) | Never (catalog property) | Vessel is designed to soft-land — only the Lander and Falcon 9 catalog parts set it |

**Predicate** (runs in the integrator wrapper at the
`physics.ClampToSurface` call site):

```
v_impact = |c.State.V|  // pre-clamp velocity
nose_alignment = c.CurrentAttitudeDir · local_up  // unit-vector dot product

if c.CanSoftLand && v_impact < V_CRIT && nose_alignment > NOSE_TOL:
    c.Landed   = true
    c.LandedLatDeg, c.LandedLonDeg = current sub-craft point
else:
    c.Crashed  = true
```

Constants: `V_CRIT = 10 m/s`, `NOSE_TOL = 0.7` (≈ 45° from
local-vertical). Both are starting values, retunable based on
playtest.

**Route-handler gate.** ViewLaunch's auto-route on
`Landed=false→true` adds an `OnPad` precondition:

```
if active.OnPad && active.Landed && !w.wasActiveLanded:
    routeToLaunchView()
```

Soft-lands set `Landed=true` but leave `OnPad=false` (it was cleared
on the original liftoff), so the route handler doesn't fire on
touchdown.

**Storage of touchdown position.** Soft-landed Vessels carry
`LandedLatDeg / LandedLonDeg` separately from
`LaunchLatDeg / LaunchLonDeg`. `integrateLanded` reads
`LandedLatDeg / LandedLonDeg` if non-zero, else falls back to
`LaunchLatDeg / LaunchLonDeg`. `Launch*` keeps its original
spawn-site meaning (useful for downrange-from-launch HUD reads even
after a return-and-relaunch cycle).

**End-flight action.** Crashed vessels gain an `[E] end flight`
key — confirm prompt (`y/n`), removes the Vessel from `World.Crafts`.
Auto-switches Active to the next vessel in the slate; falls back to
`Active=nil` if the wreckage was the only Vessel.

## Alternatives considered

### Crash detection predicate

- **(a) Pure velocity threshold.** `v_impact > V_CRIT → Crashed`,
  else Landed. Rejected: a non-Lander capsule that happens to graze
  the surface at 5 m/s would soft-land — semantically wrong; only
  vessels *designed* to land should land.
- **(b) Velocity + attitude.** Same as (a) but with nose-alignment
  check. Better, but still allows any vessel to land if it touches
  gently and pointed up. The "designed to land" qualifier is
  missing.
- **(d) Vessel-type gating alone.** Only Landers/Falcon-9s can
  soft-land; any contact for them is Landed; everything else is
  Crashed. Rejected: a Falcon 9 stage hitting the surface at 500 m/s
  is a crash, not a landing. Velocity check still needed.
- **(a) + (d) chosen.** `CanSoftLand` is the catalog gate; `V_CRIT`
  + `NOSE_TOL` are the kinematic gates. Both required. Most
  defensible posture: "this vessel is designed to land, and it
  arrived under conditions consistent with landing."
- **(c) Velocity + throttle + attitude.** Requires `Throttle > 0`
  within the last N seconds, proving active deceleration. Rejected:
  forecloses parachute-based descent (a planned v0.12+ feature)
  where no engine fires.

### Route-handler interaction with soft-land

- **(α) Soft-land does NOT auto-route to ViewLaunch.** Add `OnPad`
  flag; route handler gates on `OnPad && Landed-transition`. Soft-
  lands clear `OnPad` on liftoff so post-flight Landed doesn't fire
  the handler. **Chosen.**
- **(β) Soft-land DOES auto-route.** ViewLaunch becomes a
  "ground-scene view" generally. Requires renaming
  `LaunchSessionActive` and adding a parallel
  `LandingSessionActive` with different release semantics (no apo
  gate). Rejected: overloads a session concept that has the wrong
  release semantics for landing; adds state machine surface area.
- **(γ) Separate ViewLanding ViewMode.** Chase-cam + landing
  readouts (descent rate, fuel-to-land, etc.). Rejected: heavier
  slice; punts the "is two ViewModes justified?" question into
  v0.11.4. ViewLaunch with a manual-jump key (Shift+V) covers the
  common case without doubling the ViewMode surface.

### Storage of soft-landed lat/lon

- **Overwrite `LaunchLatDeg / LaunchLonDeg`.** Saves a field but
  loses the original-spawn-site history. The launch-HUD downrange
  read assumes Launch* is the *launch* site, not the most recent
  ground-contact site. Rejected.
- **Add `LandedLatDeg / LandedLonDeg`, fall back to Launch* when
  zero.** Chosen. Both fields persistable as `omitempty`; no
  schema bump.

### End-flight scope

- **Crashed-only.** Chosen. End-flight is the cleanup step for
  destructive impacts; it's not a general vessel-removal feature.
- **Any non-Active vessel.** Broader (abandon a stuck debris booster
  that's still in orbit). Rejected for v0.11.4: this is a separate
  cleanup-tool / sandbox-mode feature that wants its own design
  pass; do not bundle in.

### Save migration

- **Bump `SchemaVersion = 7`, write a `save_migrate_v6_to_v7`.**
  Defensive, makes the lifecycle visible in the schema history.
  Rejected: every new field has a safe `omitempty` default-false
  (existing in-flight Vessels stay in-flight; the lifecycle is
  purely additive). The version bump would only signal "we added
  flags," which the commit history already records.
- **No bump.** Chosen. New fields all `omitempty`; loading a v6
  save populates them with default-false (correct for any Vessel
  saved pre-v0.11.4).

## Consequences

**Positive.**

- The `CONTEXT.md` Touchdown / Crash glossary entry can drop its
  "not yet differentiated in code" caveat — the differentiation
  ships in v0.11.4.
- The Landed glossary entry can drop the "set only by a Launchpad
  spawn" scope cap (it's been pre-declared since v0.8.5 as a future
  removal).
- The route-handler `OnPad` precondition is small and local; future
  Vessel-spawn variants (e.g., **alongside** at a fellow Vessel's
  position) inherit "doesn't auto-route to ViewLaunch" by default,
  which matches what the player wants.
- The Lander and Falcon 9 become playable as designed — landing
  is reachable, recovery is reachable, refuel-and-relaunch is
  reachable.

**Negative / trade-offs to live with.**

- Two flags (`Landed`, `Crashed`) now share the "vessel is on the
  surface" semantic space. A future maintainer must remember that
  the post-clamp Vessel is one of: Landed (controlled), Crashed
  (destroyed), or Surface Contact (the placeholder zero-V state
  that ships as a fallback when neither predicate qualifies — e.g.,
  a non-CanSoftLand vessel that hits gently). The third bucket is
  vestigial post-v0.11.4 (every contact either qualifies as Landed
  or Crashed under the predicate); plan to delete it in v0.12+
  once playtest confirms no qualifying-but-not-Crashed cases.
- `CanSoftLand` is a vessel-level property today. If a future
  Vessel can shed its landing capability mid-flight (parachute
  failure, landing legs damaged), the model needs upgrading. Not
  a v0.11.4 concern; bank.
- `LandedLatDeg / LandedLonDeg` adds two more catalog-looking
  fields to `Spacecraft`. Save files for vessels that have soft-
  landed will be slightly larger; not material.
- The `Crashed` rendering path (dimmed sprite, no flame, no slew)
  duplicates a small amount of the active-vessel rendering. If a
  future cycle wants to render "disabled vessels" as a class
  (Crashed + power-failure + ...), refactor.

**Forward hooks.**

- **Parachutes for atmospheric descent + recovery (v0.12+).** Adds a
  non-engine path to `CanSoftLand` semantics — a deployed parachute
  on a vessel without `CanSoftLand` can still qualify for the
  Touchdown predicate via aerodynamic deceleration. Touches drag,
  spacecraft state, control modes. ADR-worthy when it lands.
- **General vessel-removal action (v0.12+).** Live-vessel cleanup
  (abandon a stuck booster, sandbox tooling). Different UX than
  end-flight; different scope.
- **Dedicated ViewLanding ViewMode.** Only if landing HUD reads
  diverge from launch HUD reads enough that ViewLaunch can't honestly
  serve both. Watch for the signal in playtest.
