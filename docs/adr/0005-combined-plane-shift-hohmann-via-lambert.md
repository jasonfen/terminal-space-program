# 0005 — Combined plane-shift + Hohmann via a fused Lambert solve

<!-- llm-parse: adr=0005 status=accepted date=2026-05-28 cycle=v0.12 slice=v0.12.x -->

**Status:** accepted (2026-05-28, `/grill-with-docs` for v0.12
Slice 5 — "combined plane-shift + Hohmann").

## Context

The intra-primary `[H]` auto-plant (`PlanTransfer` →
`PlanIntraPrimaryHohmann`, the LEO→Luna path) is a textbook
circular-coplanar two-impulse solver: it feeds the craft's
instantaneous `|R|` in as a *circular* parking radius
(`vDepCirc = √(µ/r)`) and has **no plane-change term at all**. So an
eccentric or inclined parking orbit gets a silently-wrong transfer.
The v0.10.1 `HohmannDepartureWarning` guard made the symptom visible
(advising the player to do `I` plane-match → circularize → `H`) but
did not change the math. The real fix was spec-committed as the
L-tier `plane-shift-hohmann` backlog item.

The heliocentric path is **out of scope** — it already uses Lambert
(via the porkchop), and its own departure-direction approximation
(pure prograde by radius-sign) is left untouched here.

## Decision

Replace the rendezvous branch of `PlanIntraPrimaryHohmann` with a
**fused single-rev Lambert solve**. Concretely:

1. **Fused Lambert, not analytic.** Seed the departure window + TOF
   from the existing analytic phasing (synodic wait + half-ellipse
   TOF), then do one Lambert solve from the craft's *actual* state to
   the target's actual position at arrival. The returned 3D departure
   velocity inherently carries eccentricity + the raise + the plane
   change together — so "eccentric-aware departure" is not a separate
   deliverable, it falls out of the solve. No node-vs-periapsis
   conflict (a Lambert arc connects two points regardless of nodes).

2. **`BurnVector` burn mode (new).** The fused departure Δv is an
   arbitrary 3D vector, expressible by no existing `BurnMode` (all are
   derived: prograde / normal / radial / target-relative / plane-
   change). Add a plant-only `BurnVector` mode that stores a unit
   direction captured in the inertial frame at plant-time
   (`ManeuverNode.BurnDirUnit Vec3`, omitempty). It reuses the
   existing `ThrustAccelFnFixedDir` integrator and the v0.10.0 slew
   system (the craft slews to the captured direction, lead-compensated
   like any planted node).

3. **Dual-strategy, auto-pick cheaper.** Compute *both* the combined
   fused-Lambert transfer (plane folded into the departure) and a
   **split** alternative (near-coplanar raise + a `BurnPlaneChange`
   node at the transfer apoapsis, where the craft is slowest and the
   plane change is cheapest). Plant whichever has lower total Δv;
   surface both numbers in the HUD so the player sees the trade. For
   large departure inclinations (an equatorial LEO sits ~18–28° off
   Luna's plane) the split usually wins; for small ones the combined
   does.

4. **Predictor fidelity, in-slice.** Because the Lambert solve yields
   the encounter time + target, the predicted-trajectory renderer
   concentrates sample density (and fine body-position refresh) in the
   known encounter window, so the dashed line actually draws the
   capture arc. (Pre-existing limitation: the v0.10.3 sub-step cap
   stopped the integrator *skipping* the SOI, but the evenly-spread
   sample budget still put too few points inside Luna's brief pass.)

5. **Guard retirement.** `HohmannDepartureWarning` is re-pointed to
   fire only on genuine Lambert non-convergence / degenerate geometry;
   the obsolete "match plane `[I]` + circularize, then `[H]`" advisory
   text is removed. The `I` / `PlanPlaneMatch` / `PlanInclinationChange`
   tools stay as standalone manual maneuvers — no longer prerequisite
   to `[H]`, not deprecated.

## Considered options

- **Analytic combined-plane Hohmann** (closed-form
  `√(v₁²+vₜ₁²−2v₁vₜ₁cos Δi)`). Rejected: exact only when the departure
  burn is on the node line between the two planes, which conflicts
  with the eccentric-departure-at-periapsis goal. Can't honour both.
- **Compose: auto plane-match + eccentric Hohmann.** Rejected as the
  primary path: it automates the `I`-then-`H` dance correctly but is
  always two burns and can't express the combined-vs-split trade. (Its
  three-node form survives as the *split* leg of the dual-strategy.)
- **Porkchop-style search** for the window. Rejected: heavy for an
  auto-plant; the porkchop screen already covers deliberate
  window-shopping (heliocentric).
- **Extend `BurnVector` to the heliocentric path now.** Rejected for
  scope: nearly-free marginal code, but adds heliocentric regression
  surface (Mars transfers, porkchop plant) to an already-XL slice.
  Deferred to its own slice + playtest.

## Consequences

- New persisted field `ManeuverNode.BurnDirUnit` (+ `ActiveBurn`
  mirror) and a new `BurnMode`. Additive/omitempty, following the
  `CurrentAttitudeDir` schema-v6 precedent — old saves have no
  `BurnVector` nodes, so no migration.
- The slice is **XL** (fused Lambert + intra-primary phasing + a new
  burn mode + slew interaction + dual-strategy planner + a predictor-
  fidelity rewrite). The natural split point, if it runs long, is to
  ship the combined-only planner first and add the split-strategy
  comparison second.
- `BurnVector` is a general primitive; a future slice can route the
  heliocentric Lambert departure through it to retire that path's
  prograde-fudge.
