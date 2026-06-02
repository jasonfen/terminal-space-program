# 0009 — Transposition: splitting the CSM and letting a non-bottom stage fire

<!-- llm-parse: adr=0009 status=proposed date=2026-06-02 cycle=v0.12 slice=apollo-transposition -->

**Status:** proposed (2026-06-02, `/grill-with-docs` on the Apollo
Stack flyability problem).
**Extends:** [`docs/adr/0007-surface-staging-and-decouple-plan.md`](0007-surface-staging-and-decouple-plan.md)
(the bottom-up `DecouplePlan` this slice adds a *top-release* / docked
nose-payload counterpart to) and
[`docs/adr/0008-parachutes-atmospheric-descent-recovery.md`](0008-parachutes-atmospheric-descent-recovery.md)
(the parachute Capsule recovery model the Command Module reuses).

## Context

The Apollo Stack loadout cannot complete a lunar mission as modelled,
and a session of diagnosis (`internal/sim/apollo_ascent_probe_test.go`,
PR #71) established that the cause is **structural, not a fuel/power
shortfall.** Two independent findings:

1. **The hardware is sized correctly.** The first three stages are
   byte-identical to the real Saturn V. At a real Saturn-V ascent
   efficiency (~1700 m/s gravity+drag+steering loss) the S-IVB reaches
   a 200 km park with ~3487 m/s — clearing the ~3133 m/s TLI by ~+350.
   The in-game shortfall (−172 m/s at the probe's ~2327 m/s loss) is
   **excess ascent loss**, not an under-fuelled stage; the player is
   hand-flying an ascent that the real mission flew on a closed-loop
   guidance computer. Fuel bumps were verified near-useless (+8 t →
   +23 m/s, the rocket-equation tax).

2. **The wrong engine fires after TLI.** The firing engine is welded to
   `Stages[0]` (the bottom stage) via `SyncFields`
   (`internal/spacecraft/stage.go:251`, mirroring Thrust/Isp/RCS from
   `Stages[0]`; `consumeFuel`/`BurnFuel` debit `Stages[0].FuelMass`).
   In the linear chain `[S-IC, S-II, S-IVB, Descent, Ascent, CSM]`,
   once the S-IVB drops the **LM Descent** is `Stages[0]` and fires.
   Lunar-orbit insertion (LOI, ~840 m/s) therefore runs on the lander's
   descent engine — which can't complete it (718 m/s available) and
   eats landing propellant. The CSM/SPS (the correct LOI engine) is
   pinned on top as the surviving core and cannot fire until the LM is
   gone. The real mission resolves this with **transposition and
   docking**: after a full solo TLI, the CSM separates, flips, docks to
   the LM nose-first, and pulls it off the spent S-IVB — making the CSM
   the active engine with the LM as a releasable nose payload.

A linear, bottom-first stack with a single hardwired firing engine
structurally **cannot represent transposition**: the return vehicle
must survive to fire last (TEI), so it can never also be the
mid-mission firing core under bottom-up semantics. No stage reordering
of the *static* loadout escapes this — it is transposition or nothing.

A third fidelity fact shapes the fix: the real **CSM is two vehicles**
— a Service Module (SPS engine + all propellant; does LOI, corrections,
TEI; never recovered) and a Command Module (engineless crew capsule;
heat shield + parachutes; the only piece that splashes down). The game
fuses them into one `CSM` stage.

## Decision

### 1. Split `CSM` into Service Module (SM) and Command Module (CM)

The Apollo Stack's surviving core becomes two stages: an **SM**
(propulsive — SPS engine, the LOI/TEI fuel) below a **CM** (passive
capsule — no main engine, parachute recovery, reusing the ADR 0008
Capsule model). The CM is the true surviving core; the SM is jettisoned
before re-entry. See `CONTEXT.md` (`Service Module`, `Command Module`).

### 2. Transposition makes a non-bottom stage the firing engine

**Transposition** is the post-TLI restructure that makes the SM the
firing core with the LM as a releasable **nose payload**, mirroring the
real flip. It happens during trans-lunar coast with **no Burn pending**
— the S-IVB does the *full* TLI solo first, then is discarded — which
is why it cannot be folded into TLI as one continuous push (the flip
costs coast time and would forfeit the periapsis burn position).

The canonical in-game mechanic is the **manual docking flip** the
player can already fly: drop the LM as a free craft, slew 180°, RCS-dock
to it. `DockCrafts` (`internal/sim/docking.go:299–302`) concatenates
`[CSM.Stages…, LM.Stages…]`, so the CSM lands at `Stages[0]` and becomes
the firing engine, with the LM as docked upper stages that **Undock**
later for descent. This already works end-to-end; transposition is
**not new flight semantics, it is the docking machinery already in
use.**

### 3. A one-shot transposition bypass key

Because re-flying the rendezvous every test/play iteration is tedious, a
**bypass key performs transposition in one keystroke**: jettison the
S-IVB (spawn passive), reorder the active craft to `[SM, CM, Descent,
Ascent]` with the SM at `Stages[0]`, and register the LM as a docked
component so the existing **Undock** detaches it for landing. It
reproduces the *end-state* of the manual flip — the composite with the
SM firing — skipping only the hand-flown approach. The manual docking
flip remains the canonical mechanic; the key is a convenience/cheat over
the same resulting configuration.

### 4. Per-stage Δv rebalance is a separate, parallel workstream

Transposition fixes *which engine fires*; it does not by itself close
the ~172 m/s TLI gap or guarantee each stage meets its maneuver budget.
The stack is to be **trimmed to hit each stage's maneuver capability**
(S-IVB clears TLI solo at the in-game ascent floor; SM does
LOI+corrections+TEI; Descent does descent; Ascent does
ascent+rendezvous; CM recovers). That rebalance is tracked separately
and is **out of scope for this ADR**, which records only the
structural/architectural decision.

The eval loop is `internal/sim/apollo_lunar_budget_test.go`
(`TestApolloLunarBudgetProbe`), which flies the real ascent and computes
each stage's post-transposition margin. Its headline finding sharpens
the rebalance: **transposition alone leaves only the S-IVB short.** The
SM is *not* short once the SPS burns are phased correctly (LOI at the
heavy LM-attached mass, TEI at the light bare-CSM mass), and DPS/APS are
*over*-fuelled because the Descent no longer double-duties as the LOI
engine.

**Locked trim target (requirements: LOI 900, descent 2000, ascent+rdv
1850, TEI 1000; "realistic reserve + SPS shave"):**

| Stage | Fuel kg | → | Capability | Margin |
|---|---|---|---|---|
| DPS (Descent) | 9500 | **6310** | ~2500 m/s | +500 (real abort reserve) |
| APS (Ascent) | 1800 | **1269** | ~2200 m/s | +350 |
| SPS (CSM/SM) | 18400 | **16000** | TEI | +187 (real reserve) |
| S-IC / S-II / S-IVB | — | untouched | faithful Saturn V | TLI **+277** |

Payload above the S-IVB drops 45300→39179 kg (−6.1 t), all from the
genuinely over-provisioned LM and SPS surplus — **no mass cut to the
faithful Saturn-V lower stack.** Every stage closes with a *realistic*
margin. The over-provisioned SPS funds the S-IVB's TLI margin, so real
LM abort reserves and a comfortable TLI do **not** conflict.

## Considered options

- **DPS-does-LOI trim (rejected).** Keep the single fused CSM and the
  linear order; size the Descent stage for LOI + descent (~2840 m/s),
  CSM = TEI only. Cheapest — pure numbers, no new machinery — but
  diverges from Apollo (the SPS did LOI, not the DPS) and the player
  asked for the faithful architecture. Kept on record as the fallback
  if transposition proves too costly.
- **Active-engine index instead of reorder (rejected for now).** Add an
  explicit `ActiveStageIdx` so any stage can fire without moving it to
  `Stages[0]`. Smaller change, but it does not match the *physical*
  flip (the LM genuinely moves to the CSM's nose), and it leaks a new
  invariant through every `Stages[0]` assumption (engine-bell/flame
  render, staging drop, maneuver integration). The reorder approach
  keeps `Stages[0]` meaning "firing engine" intact post-flip.
- **S-IVB does LOI too (rejected).** Keep the S-IVB through trans-lunar
  coast and fire it for capture. Unphysical (the real S-IVB was
  discarded) and needs ~3973 m/s from a stage that barely makes 3133.
- **Leave it as a skill-tight challenge (rejected).** Accept that the
  mission is only flyable with a perfect ascent *and* the manual flip.
  Rejected because the wrong-engine wall makes LOI impossible on the
  Descent stage regardless of piloting — it is not merely tight, it is
  structurally unfinishable without the flip.

## Consequences

- **A non-bottom stage can become the firing engine** for the first
  time. The reorder + `SyncFields` keeps the `Stages[0]`-is-firing
  invariant, but staging-order assumptions elsewhere (engine-bell/flame
  render keyed to `Stages[0]`, the bottom-up `DecouplePlan`) must be
  audited against a post-transposition stack.
- **Top-release / nose-payload** is the conceptual inverse of ADR
  0007's bottom-up `DecouplePlan`: the LM is released from *above* the
  surviving core. Implemented by reusing docking's **Undock** (the LM
  is a docked component), not by extending `DecouplePlan`.
- **The Apollo Stack gains a stage** (CSM → SM + CM), changing its
  stage count and `DecouplePlan`. The CONTEXT.md `Decouple Plan` entry's
  "leaving the CSM as the surviving core" is now stale and flagged.
- **No save-schema bump is anticipated** if the bypass key produces a
  standard docked-composite craft (already persisted), but the docked-
  component snapshots for the LM must round-trip through save/load.
- **The bypass key is a cheat over a real mechanic**, so it can ship
  ungated without inventing a flight path that doesn't otherwise exist —
  the manual flip produces the identical configuration.
