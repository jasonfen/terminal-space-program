# 0006 — Intra-primary transfer must arrive coplanar; Projected Orbit propagates analytically

<!-- llm-parse: adr=0006 status=accepted date=2026-05-29 cycle=v0.12 supersedes=0005#3,0005#4 issues=66,67 -->

**Status:** accepted (2026-05-29, `/grill-with-docs` for GH #66 / #67).
Supersedes ADR [0005](0005-combined-plane-shift-hohmann-via-lambert.md)
decisions **3** (dual-strategy split-at-apoapsis) and **4** (predictor
fidelity via sample densification).

## Context

ADR 0005 shipped the dual-strategy intra-primary `[H]` (v0.12.1). Two
of its decisions were wrong, found by diagnosis 2026-05-29 (measured
with exact-Kepler propagation, zero integrator noise) and confirmed in
playtests:

1. **The split does not rendezvous.** Decision 0005#3 placed the
   `BurnPlaneChange` at "the transfer apoapsis, where the plane change
   is cheapest." But a burn changes *velocity*, not *position* — at an
   inclined apoapsis the Vessel is still `sin(i)·r_apo` out of the
   target's plane (for the default 19.4° LEO→Luna, ~124 000 km). The
   plane change rotates the orbit but leaves the Vessel ~100 000 km
   from the target (measured closest approach **100 402 km**, target
   SOI ≈ 66 000 km). The capture node is planted in empty space. The
   cheap Δv (~3 930 m/s) was real but bought a transfer that never
   arrives.

2. **The combined Lambert is unsuitable for inclined targets.** It
   *reaches* the target's position (analytic miss 0 km) but a TOF sweep
   shows it is both expensive (10–14 km/s departure, near the 180°
   Lambert singularity) **and** arrives steeply inclined (21–74° to the
   target's plane) — it would capture into a badly tilted orbit, not
   the ~0° the player wants. So "auto-pick the cheaper of combined/split"
   was choosing between two broken candidates.

3. **The Projected Orbit drifts.** Decision 0005#4 attributed the
   missing capture arc to sparse sampling and prescribed densification.
   The real cause is integrator truncation: the Predictor propagates
   coast legs with **fixed-step Verlet** (120 s sub-step cap), which on
   the e≈0.96 transfer ellipse drifts ~46 000 km by apogee (clean dt²
   convergence: 120 s→46 000 km, 60 s→11 800, 5 s→83). Densifying an
   already-wrong path draws nothing.

## Decision

**A. The split places its plane change on the Line of Nodes.** Constrain
the transfer so apoapsis falls on the Line of Nodes (the craft-plane ∩
target-plane axis) and phase so the target is at that node at arrival.
A plane change there leaves the Vessel *on* the shared line, so the
post-burn orbit is coplanar. Verified by construction: this lands the
Vessel exactly on the target's orbit ring (Δ 0 km), **0.000° relative
inclination**, for the same ~3 930 m/s (3060 raise + 64 node plane
change + 806 capture). The cost was always right; only the geometry was
wrong. A correct Transfer Plan must arrive coplanar (≈0° relative
inclination) so the Capture Burn inserts into a sane orbit — this is
the acceptance bar, not Δv minimisation.

**B. The combined Lambert is retained only for near-coplanar / general
use, not as the inclined-target answer.** It is not auto-picked for
inclined intra-primary targets (it arrives steep). Its near-180°
conditioning is a separate, lower-priority concern (GH #67).

**C. The Projected Orbit propagates coast legs with Kepler Step.** Extend
the existing **Kepler Step** Propagation Mode (already used by the warp
lock, exact for bound two-body arcs) to the Predictor's ballistic coast
legs, falling back to Verlet only where it is actually needed: inside an
atmosphere (drag) and on hyperbolic arcs (e≥1, where `KeplerStep`
returns false). SOI transitions remain the caller's responsibility,
handled by the same per-sub-step `FindPrimary`/`Rebase` the Verlet path
already runs. Sample concentration around the SOI-entry window is folded
in on top, so the now-correct path is also drawn smoothly.

## Considered options

- **Fix the combined's 180° conditioning and use it alone** (drop the
  split). Rejected as the inclined-target answer: even well-conditioned,
  the short-way Lambert from an inclined parking orbit arrives steeply
  inclined and stays expensive. The node-split is both cheaper and
  coplanar.
- **Charge the split's unpaid out-of-plane cost so the auto-pick avoids
  it.** Rejected: a band-aid that hides the non-arriving transfer rather
  than producing an arriving one.
- **Keep densification (0005#4) and just add more samples.** Rejected:
  the integrated path is ~46 000 km off before sampling enters the
  picture; no sample budget recovers a wrong trajectory.
- **Tighten the Verlet sub-step instead of switching to Kepler.**
  Rejected: ~5 s sub-steps (≈24× the per-frame cost) only reach ~83 km
  accuracy; Kepler is exact *and* cheaper.

## Consequences

- The split planner gains a Line-of-Nodes constraint on departure
  point + phasing (new math; the apoapsis-anywhere `splitPlaneChange
  AtApoapsis` path is replaced). Phasing becomes "target at the node at
  arrival" — a 1-D timing solve (the target crosses each node ~twice per
  orbit).
- The Predictor (`internal/sim/predict.go`) gains a Kepler-vs-Verlet
  switch on each coast leg; the live integrator (`canKeplerStep`/
  `keplerStepWithSOICheck`) already encodes the same guards to mirror.
- `CONTEXT.md`: **Line of Nodes** added; the **Transfer Plan** entry
  corrected (the old "both arrive in the target's plane" claim was
  false for the apoapsis split).
- Acceptance is behavioural, not just Δv: a regression test asserts the
  planted intra-primary transfer arrives within the target's SOI at
  ≈0° relative inclination, and the Projected Orbit's coast leg matches
  an exact-Kepler arc.
