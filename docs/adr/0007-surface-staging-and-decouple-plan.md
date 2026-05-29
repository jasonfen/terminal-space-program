# 0007 — Surface staging + explicit decouple plan (2-stage Lander)

<!-- llm-parse: adr=0007 status=accepted date=2026-05-29 cycle=v0.12 slice=lander-2-stage -->

**Status:** accepted (2026-05-29, `/grill-with-docs` for v0.12
Slice 2).
**Extends:** [`docs/adr/0004-crashed-landed-lifecycle.md`](0004-crashed-landed-lifecycle.md)
(the Crashed/Landed lifecycle this slice adds a transition to) and the
v0.9.1 staging machinery in `internal/sim/staging.go`.

## Context

The real Apollo Lunar Module was two vehicles: an ascent stage that
launched *from* a descent stage left behind on the lunar surface.
v0.11.5 modelled the Lander as a single catalog Stage with
`CanSoftLand=true`. v0.12 Slice 2 splits it into a descent + ascent
pair and introduces the **surface staging** moment — decoupling the
ascent stage from the descent stage *while Landed* — as a gameplay
extension of the ADR 0004 lifecycle.

Two structural facts in the existing code shape the design:

1. **`StageActive` already drops `Stages[0]` (bottom) and keeps the
   upper chain as the active core** — which is exactly the
   descent→ascent topology (descent is the bottom/firing stage during
   powered descent; ascent is the upper core that returns to orbit).
   The chain-manipulation logic generalises; only the *spawn placement*
   of the jettisoned stage diverges between orbit and surface.
2. **`checkDocking` (internal/sim/docking.go) does not exclude Landed
   craft.** It fuses any same-primary pair within `DockingDistM` (50 m)
   and `DockingVMS` (0.1 m/s). Two co-located Landed craft — the shed
   descent stage and the parked ascent stage, both pinned to the same
   lat/lon with identical `V = ω×R` — would re-fuse on the next tick.
   The orbital staging fix (60 m + 0.5 m/s retrograde nudge) does not
   work on the surface: `integrateLanded` re-pins R/V from the stored
   lat/lon every tick, so any inertial offset is overwritten.

A second, larger problem surfaced once we decided to split the LM
inside the **Apollo Stack** too (not just the standalone Lander). The
Apollo Stack `[S-IC, S-II, S-IVB, LM, CSM]` separates the LM from the
CSM via a single bottom-pop. If the LM becomes two stages
`[…, descent, ascent, CSM]`, a single bottom-pop drops *only the
descent stage* — stranding it and leaving the ascent fused to the CSM.
Extracting a 2-stage LM as one unit needs machinery that did not exist.

## Decision

### 1. The abandoned descent stage is a Landed *passive* Vessel

Surface staging leaves the descent stage as a **Landed** passive
`Spacecraft` (Throttle=0) at the touchdown lat/lon, co-rotating via
`integrateLanded` like any landed craft. It is **not Crashed** (that
state means a destructive surface arrival — dimmed render, End-Flight
removal — semantically wrong for intact, intentionally-abandoned
hardware) and **not a new "Abandoned" state** (no distinct behaviour is
needed, and a new flag would cost a `Spacecraft` field for no gain).
Reusing Landed means **no new lifecycle state and no save-schema bump**.
Removal of the abandoned stage as clutter is out of scope — it is the
"general vessel-removal action" already forward-hooked in ADR 0004.

### 2. Re-fuse fix: `checkDocking` skips a pair when both are Landed

A one-line guard in `checkDocking`: skip any pair where both craft are
`Landed`. This is structural (re-evaluated every tick and on load), so
it survives save/reload with no persisted state. It fixes the
immediate post-decouple re-fuse without foreclosing future **moon
bases** — deliberately joining landed craft will be designed as its own
feature (auto-by-proximity vs. an explicit "connect" action is
deferred to that feature; see Forward hooks). A completed orbital dock
is already a single fused craft, so the only co-located-both-Landed
*pair* that exists is a not-yet-separated decouple — exactly what this
guard is for.

### 3. Surface staging reuses `StageActive`, branching placement on `Landed`

The chain-pop (pop the bottom unit, rebuild the active craft from the
remainder, `SyncFields`, rename to the new bottom stage) is shared with
orbital staging. Only the jettisoned-craft *construction* branches:

- **Orbital** (`parent.Landed == false`): retrograde 60 m + 0.5 m/s
  nudge, as today.
- **Surface** (`parent.Landed == true`): the jettisoned craft is
  spawned `Landed=true` at the parent's `LandedLatDeg/LandedLonDeg`
  with no inertial offset (the landed integrator re-pins it); the
  active (ascent) craft stays `Landed=true` until the player ignites.

The same `space` staging key drives all cases. After surface-staging a
2-stage Lander `[descent, ascent]`, the count goes 2→1 (allowed by the
existing `ErrStageOnlyOne` guard), leaving the ascent as the
single-stage core.

### 4. The Apollo Stack is split too, via an explicit per-loadout Decouple Plan

A Loadout may declare a **Decouple Plan**: a bottom-up list of group
sizes saying how many contiguous bottom Stages each staging press
releases as a single craft. Absent ⇒ all-ones (one Stage per press =
today's behaviour). The Apollo Stack declares `[1, 1, 1, 2]`: drop
S-IC, S-II, S-IVB individually, then release `descent + ascent`
**together** as a 2-stage LM craft, leaving the CSM as the surviving
core. The standalone Lander needs no plan — its default single-pop
drops descent and leaves ascent.

Storage & consumption:

- `Spacecraft.DecouplePlan []int` — copied from the Loadout in
  `NewFromLoadout`, consumed positionally (each press pops `plan[0]`
  bottom stages as one craft, then advances `plan = plan[1:]`).
- A released multi-stage craft inherits **no** plan, so its internal
  boundaries become ordinary single-Stage separations. The extracted
  2-stage LM therefore surface-stages its descent stage **alone** with
  zero special-casing — no tag-clearing, no consume-on-extract step.
- The LM extraction is an orbital group-pop (post-TLI), so it uses the
  retrograde-nudge placement; `buildJettisonedCraft` generalises to
  build a craft from N stages.

`DecouplePlan` is persisted on the save wire `Craft` as
`decouple_plan,omitempty` (a "schema v6 additive" field, matching the
`PitchTrim` / `CurrentAttitudeDir` precedent). Absent ⇒ nil ⇒
single-pop, so old saves and pre-split craft round-trip unchanged.
**No `SchemaVersion` bump.** Persistence is required (not derived from
the catalog) so a mission saved mid-staging restores the correct
remaining grouping; deriving it from current-stages-vs-catalog was
rejected as fragile.

### 5. Both descent and ascent stages carry `CanSoftLand=true`

The descent stage keeps the v0.11.5 silhouette (legs, hypergolic
flame, squat body) and `CanSoftLand=true`. The ascent stage also
carries `CanSoftLand=true` — a forgiving sandbox choice so a player who
flies the bare ascent stage back down can soft-land it rather than
crash, even though the landing legs physically stayed on the descent
stage. (`SyncFields` derives the vessel's `CanSoftLand` from the bottom
stage, so after surface-staging the ascent craft is soft-land-capable.)

## Alternatives considered

### State of the abandoned descent stage
- **New "Abandoned" state.** Distinct flag enabling bespoke rendering
  and End-Flight gating. Rejected: needs a new `Spacecraft` field
  (likely a schema bump) and new integrator/render branches for
  behaviour identical to Landed-passive.
- **Crashed-style wreck.** Reuses dimmed render + `[E]` removal.
  Rejected: conflates "destroyed in an impact" with "intentionally left
  behind intact" — contradicts the CONTEXT.md Crashed glossary.
- **Reuse Landed (passive). Chosen.** No new state, no schema bump;
  the descent stage is exactly intact hardware co-rotating on the
  surface.

### Preventing the post-decouple re-fuse
- **Separate-first hysteresis** (per-pair "must be observed >50 m apart
  before re-docking"). The most general — keeps proximity auto-dock
  available for landed craft. Rejected *for now*: surviving reload
  needs a stable persisted craft identity, which the codebase lacks;
  adding craft IDs is effectively its own sub-slice.
- **Lateral lat/lon offset on the descent stage.** Physically wrong
  (descent should stay put) and a magic distance tuned against
  `DockingDistM`. Rejected.
- **Per-craft `NoAutoDock` flag.** Adds a field overlapping
  `Role="jettisoned-stage"`. Rejected.
- **Skip auto-dock when both Landed. Chosen.** One structural guard,
  no state, survives reload, defers the moon-base docking-model
  decision to that feature.

### Apollo Stack scope
- **Standalone Lander only** (defer Apollo LM split). Smallest, keeps
  the slice self-contained. Rejected: the iconic descent-stage-on-the-
  Moon moment lives in the Apollo mission.
- **LM as a pre-split sub-craft** (one catalog "stage" that expands
  into two on decouple). Rejected: bespoke, Apollo-specific, surprising.
- **Build decouple-grouping. Chosen.** Splits the LM everywhere; pulls
  the slice to M–L.

### Decouple-grouping representation
- **Per-stage `DecoupleGroup` tag** with contiguous-bottom-group pop.
  Works, but the tag has to be *consumed/cleared on extraction* (else
  surface-staging re-groups descent+ascent and empties the craft) — a
  stateful mutation that reads as magic.
- **Directional "drag stage above" flag.** Same consume-on-extract
  requirement; less legible than a named plan.
- **Explicit per-loadout Decouple Plan (group sizes). Chosen.**
  Declarative, lives on the Loadout, consumed positionally; the
  released sub-craft inherits no plan and falls back to single-pop, so
  there is nothing to clear.

### Ascent soft-land capability
- **`CanSoftLand=false`** (realistic — legs stayed on the descent
  stage). Rejected: a re-descent would crash, surprising in a sandbox.
- **`CanSoftLand=true`. Chosen.** Forgiving; both stages land-capable.

## Consequences

**Positive.**
- The 2-stage Lander and the full Apollo descent-stage-left-behind arc
  become playable through one generalised staging path.
- No new lifecycle state and no `SchemaVersion` bump — the descent-as-
  Landed-passive decision and the additive `decouple_plan` field both
  ride existing machinery.
- The Decouple Plan is a reusable primitive: any future multi-payload
  stack (e.g. dual-satellite deploy) can declare its own grouping.

**Negative / trade-offs to live with.**
- "Skip auto-dock when both Landed" forecloses proximity-based landed
  docking until the moon-base feature revisits it; landed craft cannot
  currently fuse by driving them together.
- `DecouplePlan` adds wire-mapping surface to `save.Craft` (both
  directions) even though it is a single additive field.
- A two-stage Lander a player saves mid-surface-staging (decoupled but
  ascent not yet ignited, both co-located + Landed) relies on the
  both-Landed dock guard at load; correct, but a maintainer touching
  `checkDocking` must preserve that guard.
- `CanSoftLand=true` on a legless ascent stage is a gameplay
  convenience that slightly stretches the "designed to land" framing in
  the CanSoftLand glossary.

**Forward hooks.**
- **Moon bases / landed docking.** Deliberately joining landed craft —
  auto-by-proximity (needs a reload-surviving separate-first hysteresis
  + stable persisted craft IDs) vs. an explicit "connect" action — is
  deferred to its own feature. The both-Landed dock guard is the seam
  that feature will revisit.
- **Stable craft identity.** The hysteresis dead-end exposed that the
  slate has no stable per-craft ID (only Name + shifting indices). A
  persisted monotonic craft ID would unblock proximity landed-docking,
  stable targeting, and mission tracking — worth its own slice.
- **General vessel-removal action** (ADR 0004 hook) would let players
  clear abandoned descent stages as clutter.
