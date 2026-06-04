# 0011 — Spawn-time docked composites: the Nose Payload Plan + Dock Seam

<!-- llm-parse: adr=0011 status=accepted date=2026-06-04 cycle=v0.14 slice=spawn-docked-composite -->

**Status:** accepted (2026-06-04, `/grill-with-docs` on the
"custom CSM+LM splits in orbit" playtest report).
**Extends:** [`docs/adr/0007-surface-staging-and-decouple-plan.md`](0007-surface-staging-and-decouple-plan.md)
(the bottom-up `DecouplePlan` this slice mirrors as a *top-release*
counterpart) and
[`docs/adr/0009-transposition-and-active-engine.md`](0009-transposition-and-active-engine.md)
(this realizes 0009's "top-release / nose-payload" forward hook — the
post-transposition composite, now reachable at spawn instead of only by
flying the flip).

## Context

A player wanting to repeat-test the lunar-operations arc — CSM in Moon
orbit → LM descent → ascent → dock → trans-Earth return → re-entry —
built a **custom** vessel in the spawn configurator by picking the CSM
and the Lander, spawned into Moon orbit, and **Staged**. The descent and
ascent stages separated *in orbit*, not on the surface as intended.

The cause is structural, not a numeric bug. A custom build becomes a
single **linear** Vessel via `NewFromStages`, which sets
`DecouplePlan: nil` — so every **Staging** press single-pops one Stage
(`staging.go`, `groupSize = 1`). For a `[CSM, Descent, Ascent]` stack
that means: drop CSM, then drop Descent *alone in orbit*, stranding it
from the Ascent. There is no key sequence on a linear custom build that
produces the intended arc.

The intended arc is not a linear stage chain at all. Per ADR 0009 the
real Apollo lunar configuration is a **docking composite**: the
**Service Module** is the firing core (LOI/TEI engine), the **Lunar
Module** rides as a docked **nose payload** released by **Undock** (not
Staging), and the LM only splits its descent stage from its ascent stage
*later, on the surface*, via **Surface Staging** (ADR 0007). A linear
bottom-first stack with a single hardwired firing engine **cannot
represent this** — exactly the wrong-engine / can't-detach-as-a-unit wall
ADR 0009 documented for the Apollo Stack.

Crucially, every piece needed to *fly* that arc already shipped in v0.12:

- `DockCrafts` concatenates `[core.Stages…, payload.Stages…]` so the
  core's bottom is `Stages[0]` (the firing engine) and records each half
  as a `DockedComponent` carrying its full per-Stage breakdown.
- `Undock` reconstitutes a multi-stage component — the LM comes back as a
  coherent `[Descent, Ascent]` craft with live fuel — from
  `DockedComponent.Stages`.
- `DockedComponent.Stages` **persists** through save/load
  (`save.go`), so a docked composite round-trips.
- The Apollo Stack already carries the SM/CM split and a one-key
  **Transposition** (`D`) producing the `[SM, CM, Descent, Ascent]`
  composite.

The **only** missing capability is the one the player hit: `SpawnSpec`
can place a single *linear* craft only — there is no way to spawn a
**pre-docked composite**. The full Apollo arc is otherwise reachable only
by flying the Saturn V ascent + TLI + transposition flip every iteration,
which defeats repeat play-testing of the lunar-operations portion alone.

## Decision

### 1. The Nose Payload Plan — a top-release counterpart to the Decouple Plan

A custom build or `Loadout` may declare a **Nose Payload Plan**: a list
of group sizes naming how many contiguous **top** Stages form a docked
**nose payload** rather than linear firing-core Stages. Where the
bottom-up `DecouplePlan` releases bottom Stages via **Staging**, the
Nose Payload Plan pre-assembles the top group at spawn and hands its
release to **Undock**. The two are duals: `DecouplePlan` is consumed
upward by Staging during flight; the Nose Payload Plan is consumed once,
at spawn, to build the composite.

The plan is **list-shaped** (`[]int`) though v0.14 supports exactly one
entry — a single nose payload (the LM, itself possibly multi-stage). This
covers the Apollo arc today while leaving multiple distinct payloads
(core + LM + a separate probe) an **additive** extension with no save
migration. Absent ⇒ a plain linear Vessel, the historical custom-build
behaviour, unchanged.

### 2. Spawn assembles the composite by reusing DockCrafts

At spawn, when a Nose Payload Plan is present, the builder splits the
flat stack at the seam, constructs the core and the payload as separate
co-located Vessels, and **`DockCrafts`** them into a ready composite. The
two are spawned at the same inertial state, so `DockCrafts`'s centroid /
momentum merge is an identity — the value is its existing, tested
production of the concatenated `Stages` plus the `DockedComponent`
snapshots. The result is an ordinary docked-composite Vessel: SM at
`Stages[0]` firing, LM an `Undock`-able nose payload, **already in the
post-Transposition shape with no flip to fly.**

Because the spawned composite is an ordinary `DockedComponents` craft
that already persists (ADR 0009), there is **no `SchemaVersion` bump**.
The Nose Payload Plan is a spawn-time / Loadout concept, not new
persisted Vessel state — once assembled, the composite is indistinguish-
able from one a player built by docking in flight.

### 3. The Dock Seam marks the boundary in the configurator

In the spawn configurator's stack list, the player marks the contiguous
top group as the nose payload with a **Dock Seam** — the editor-side
expression of the Nose Payload Plan. The single bottom-first stack editor
is kept (no two-pane redesign); the seam is one extra marker on the
existing list. Below the seam fires; above it Undocks.

### 4. A pre-seamed CSM+LM configurator module

A `BuildModule`-style **CSM+LM** catalog pick drops the four Apollo
Stages `[SM, CM, Descent, Ascent]` with the Dock Seam pre-set between
`CM` and `Descent`, reusing the exact SM/CM/LM hardware the Apollo Stack
already defines. Picking it, choosing Moon orbit, and spawning lands the
player in the assembled composite ready to `U`-release the LM — the
one-pick repeat-test loop, without leaving the configurator surface.

## Alternatives considered

### Scope of the fix
- **"Just tell me the key sequence" (no code).** Route the player to
  spawn CSM + Lander as two craft and manually dock to transpose.
  Rejected: the player asked for custom builds to support this generally,
  and two-spawns-plus-a-manual-rendezvous is exactly the friction
  repeat-testing wants gone.
- **Apollo-only spawn affordance** ("spawn the Apollo Stack already in
  Moon orbit, post-transposition"). Narrower, but bespoke to one mission
  and doesn't generalize the configurator. Rejected for the general seam.
- **General nose-payload seam in the configurator. Chosen.** Any
  player-built core + nose payload works; the Apollo case is then just a
  pre-seamed module on top of the primitive.

### Configurator UX for the boundary
- **Two-pane "Core" + "Payload" editor.** Clearest mental model but a
  larger screen/UI change and more spawn-time state. Rejected as
  heavier than the arc needs.
- **Composite-only catalog modules** (no general seam). Smallest change,
  nails Apollo, but only as-curated. Rejected: the player asked for
  generality.
- **Dock Seam marker on the single stack list. Chosen.** Smallest UI
  delta that is still fully general, and the literal inverse of
  `DecouplePlan`.

### Seam multiplicity / storage
- **Single split-index int.** Simplest, but multiple distinct payloads
  later would force a data-model/wire change. Rejected.
- **Full multi-payload now.** Most flexible, more configurator + spawn
  complexity than the arc needs. Rejected for v0.14.
- **One payload, `[]int` list-shaped storage. Chosen.** Covers Apollo,
  no dead-end, no future migration.

### Spawn-assembly mechanism
- **Bespoke composite builder** that constructs `Stages` +
  `DockedComponents` directly, skipping `DockCrafts`. Avoids the no-op
  centroid merge but duplicates the snapshot/concatenation logic that is
  the single source of truth for docked composites. Rejected.
- **Reuse `DockCrafts` on two co-located craft. Chosen.** One code path
  for composites whether assembled at spawn or by flying a dock; the
  identity merge is cheap.

### Release semantics of the payload
- **Extend `DecouplePlan` with negative/top-release entries.** Rejected
  by ADR 0009 already: the LM-as-docked-component is what lets it Undock
  as a coherent unit *and* retain its own internal grouping for Surface
  Staging. Top-release belongs to **docking**, not the Staging plan.

## Consequences

**Positive.**
- The full lunar-operations arc becomes repeat-testable from a single
  spawn — no Saturn V ascent, TLI, or flip per iteration.
- The Nose Payload Plan is a reusable primitive: any future stack that
  carries an Undockable top payload (sample-return probe, tug + cargo)
  can declare one, and the list shape already admits several.
- Realizes ADR 0009's top-release forward hook with **no new flight
  semantics and no `SchemaVersion` bump** — it rides `DockCrafts`,
  `Undock`, and the existing `DockedComponents` persistence.

**Negative / trade-offs to live with.**
- `SpawnSpec` and `Loadout` each gain a `NosePayloadPlan` field, and the
  spawn path branches on it — a new shape for `SpawnCraft` to carry.
- The Dock Seam adds configurator state and a render/edit affordance to
  the stack list — modest UI surface on a screen that ADR 0010 just
  worked to keep slim.
- A custom core with no firing engine below the seam (e.g. seam placed so
  the core is the engineless CM alone) spawns a composite that cannot
  thrust. Treated as a sandbox footgun, not a hard validation error;
  a soft check may warn.
- "One payload now" means a build wanting two distinct nose payloads
  must wait for the additive extension; the list shape keeps that cheap
  but it is not in v0.14.

**Forward hooks.**
- **Multiple nose payloads.** The `[]int` plan already admits more than
  one entry; the configurator seam UX and the spawn split loop generalize
  to N payloads when a stack needs it (e.g. dual-probe deploy).
- **Spawn-list composite Loadouts.** The Nose Payload Plan living on
  `Loadout` means a top-level "Apollo CSM+LM (lunar ops)" spawn entry —
  skipping the custom editor entirely — is a later additive option if the
  configurator module proves too many keystrokes for repeat testing.
- **Core-without-engine validation.** A soft "this core can't fire"
  warning in the configurator is deferred to the seam UX polish.
