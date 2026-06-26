# Version history

Newest first. Per-version detail: [state-of-game.md](../designdocs/terminal-space-program/state-of-game.md).

---

**v0.24.1** — Universal antenna + Kern DSN ring (ADR 0027 amendment). Every vessel now carries at least a basic telemetry antenna — **crewed pods included**, where it was previously omitted — so all craft appear on the **CommNet** under a uniform model. Crewed vessels are still never command-gated; for them the antenna is presence-only (they show as a non-forwarding node, the comms chip stays hidden). A second DSN ground-station ring lands on **Kern**, Lumen's home world (**Stdin / Stdout / Stderr**, ~120° apart, matching Earth's Goldstone / Madrid / Canberra) — so a probe launched in Lumen can finally reach the network instead of flying permanently out of contact. No save break.

**v0.24.0** — In-game Vehicle Assembly Building + spawn-form fleet pass (ADRs 0029/0030/0031). A new `Esc → [Build (VAB)]` screen lets you compose fine **components** (engine / tank / command-core / antenna / structure) into stages, view a glyph-row vehicle schematic in a two-column layout, reorder / duplicate / quantity-adjust stages, and read a live Δv / TWR / mass panel; designs save as portable catalog fragments under `~/.config/terminal-space-program/designs/` and appear in the spawn form (`n`) alongside built-ins. The spawn form's CRAFT TYPE picker is now **grouped by category** (Launch Vehicles / Crewed Mission Stacks / Upper Stages / Landers & Capsules / Tugs & Relays / Satellites & Payloads), shows crewed/uncrewed tags, gates launchpad spawn on TWR ≥ 1 physics (no static flag), and **filters by scale class** — Sol shows the real fleet, Lumen shows a full in-universe fleet (`[f]` reveals all). A complete **Lumen fleet** ships: role-for-role counterparts of every Sol loadout with computing-theme names (Vector V, Raster Launch System, Packet 9, Buffer, Spool, Socket Lander, Token Pod, Nudge Tug, Thread Relay, Heartbeat Keeper, Uplink Relay, Relay Node, Endpoint Station, Scalar Probe, Node Carrier ×3, Scan Pack). No save break.

**v0.23.0** — Deployable payloads (ADR 0028, PR #186). A new `Y` Deploy verb releases the top carried payload as its own craft while keeping the carrier active — distinct from Undock, which switches you to the released piece. `NosePayloadPlan` supports N stacked payloads so one launch can deploy a full constellation; five starter loadouts ship (Relay Comsat, Ground-Station Lander, Science Probe, Comsat Carrier ×3, Survey Pack). Soft-landing a relay-antenna craft auto-joins the CommNet graph as a player-deployed ground station with no extra step. No save break.

**v0.22.3** — CommNet relay-path rendering fix (PR #185). Long relay hops weren't drawn all the way to the destination. A new canvas primitive draws the beam without the too-long-chord guard, so the full relay chain renders at any zoom. No save break.

**v0.22.2** — CommNet range model: combinability + antenna tiers (PR #184). Replaced the flat 20,000 km cap with KSP-style combinability (`√(rangeₐ · range_b)`), so a powerful DSN station extends a weak probe's reach. Three tiers ship: direct-basic (LEO/geo), relay-cislunar (Moon and below), deep-space (Mars-class). No save break.

**v0.22.1** — CommNet ground-station self-occlusion fix. A floating-point normalization error caused DSN stations to flicker in and out of self-occlusion every tick, making nearby probes strobe NO SIGNAL. A small surface tolerance in the line-of-sight test fixes the flicker while leaving genuine coverage gaps unchanged. No save break.

**v0.22.0** — Data-driven parts/loadout catalog + CommNet (ADRs 0026/0027, PR #178). Every stage and loadout moved from Go literals to embedded JSON with user-overlay support; `--list-loadouts` reflects the merged catalog. CommNet adds per-stage command-source (crewed/probe) and antenna (direct/relay) attributes, a per-tick relay-graph, and command-gating ("NO SIGNAL") for unconnected probes. A comms chip and relay-path beam render on the orbit map; crewed vessels are never gated. No save break.

**v0.21.0** — Missions: give the sandbox a purpose (ADR 0025, PRs #171–#177). A data-authored Objective→Mission→Program model ships with two objective families: instantaneous state predicates and semantic-action event matchers. An embedded tutorial→challenge ladder (orient → plan → fly → Luna → Mars) is opt-in from Settings and off by default. The missions screen shows a gated ladder; an in-flight checklist chip tracks the current step. Save v8→v9 (soft — old saves still load, mission progress resets).

**v0.20.0** — Data-driven body textures (ADR 0024, PRs #167–#170). A generic JSON texture engine replaces every per-body Go shader function; all 17 Lumen and 12 Sol bodies are now authored as data using typed Feature Kinds (continents, craters, bands, spots, mask, star). Net −1215 lines; user-overlay systems can now include their own textures. No save break.

**v0.19.0** — Encounter readability (ADR 0023, PR #166). Planted-leg foreign-SOI arcs and in-SOI residence arcs are now sampled analytically from the encounter conic rather than propagated with fixed steps, so predicted paths read cleanly. No save break.

**v0.18.5** — QWERTZ keyboard-layout preset (ADR 0022, PR #165). A layout selector in `[Controls]` remaps the physical Y↔Z swap so QWERTZ users keep every binding under the same finger as QWERTY. Implemented as ingest-normalize + display-translate; the keymap stays authored in QWERTY. No save break.

**v0.18.4** — Encounter-view ergonomics (PRs #162, #163). The dashed post-burn orbit, SOI-pass arc, and SOI ring are snapshotted on the last coasting frame and held steady while a burn fires, so the purple preview no longer vanishes at ignition. Yaw moved from `{`/`}` to `shift+←`/`shift+→` to match the tilt keys. No save break.

**v0.18.3** — Player-owned camera + Local-to-Body Arcs (ADR 0021, PRs #152–#160). Foreign-SOI predicted segments are now drawn body-relative and anchored to the encounter body's current position, collapsing a 47.8× SOI smear to 1.0×. The Camera Contract (fit once per Framing Event, never per frame) ships alongside SOI rings, entry/exit markers, in-SOI continuation, and a split-capture aim fix. No save break.

**v0.18.2** — Encounter-framing zoom fix (PR #146). The v0.18.1 encounter framing re-fit every frame, overwriting manual zoom on the next tick. Canvas scale is now `baseScale × userZoom` so auto-fit owns the base and `+`/`-` persist until the next Framing Event. No save break.

**v0.18.1** — Encounter-centered framing fix (PR #145). Focusing a body with an active SOI pass centered on the body's current position rather than the encounter arc, landing up to 169× too far away. All three framers now center on the predicted encounter and fit to the drawn arc's extent. No save break.

**v0.18.0** — Encounter view: SOI Pass prediction + unified markers (ADRs 0019/0020, PRs #138–#142). Unified single-glyph orbital markers (▲▼◇◆⊕✕Δ) replace the old FillDisk blobs; all craft become `➤` on the orbit map. A live SOI Pass prediction renders an always-on foreign-SOI arc, Perilune marker, and SOI PASS chip; a dual-arc counterfactual appears when a node is planted. No save break.

**v0.17.3** — Coplanar planet→moon transfer fixes (ADR 0018, PR #131). The combined transfer now aims at an in-plane offset so the natural perilune lands at the Capture Orbit radius rather than the body's centre. Capture fires at the analytic hyperbolic perilune; a live TARGET readout shows approach data for body targets. No save break.

**v0.17.2** — SOI-entry prediction fidelity (ADR 0017, PR #128). Both SOI-aware propagators now default to interpolated body positions, bisection-refined crossing times, and a 120 s coast sub-step cap, eliminating the dashed-line wobble on lunar transfers. A predict-on-change cache keeps the dashed line off the per-frame render path. No save break.

**v0.17.1** — SOI-entry prediction diagnostics (PR #127). An env-gated harness quantifies prediction stability and accuracy across three reference transfers; no behaviour change.

**v0.17** — Command-line scenarios (PR #125). `--system`, `--orbit`, `--altitude`, `--inclination`, `--launchpad`/`--launch-site`/`--lat`/`--lon`, `--loadout`, and `--list-*` discovery flags let you boot straight into any scenario without a save. No save-schema change.

**v0.16.1** — Hygiene + correctness (PRs #122–#124). Two-sided guards on the Saturn V ascent test; F1 help gains the `[»Burn]` mouse row; `FlightPhase` vocabulary scaffolded. No save break.

**v0.16** — Auto-Warp + Vessels bound to a System (ADRs 0015/0016). `G` engages Auto-Warp, which runs at maximum permitted rate and ramps back to 1× exactly 30 sim-seconds before the next planted burn. Each Vessel is now bound to its own System at spawn so the Kern Stack flies Lumen while a Sol craft orbits in parallel. Save v7→v8.

**v0.15** — Lumen system + Scale Class (ADR 0014, PRs #104–#107). A 17-body KSP-stock-analog system at ~1/10 linear scale (~3.4 km/s to orbit) with a Scale Class hint on Systems and Loadouts. The Kern Stack (4-stage Apollo-analog) is the scale-matched vehicle for Lumen. Save break: delete your save file to start fresh.

**v0.14** — Spawn-time docked composites (ADR 0011, PR #81). `NosePayloadPlan` splits a custom build at the Dock Seam and assembles a ready docked composite, so a CSM+LM spawns already in post-transposition shape. The `[d]` configurator key cycles the seam; a `csm-lm` module pick drops the Apollo stack pre-seamed. No save schema bump.

**v0.13** — HUD overlay refactor (ADR 0010). The tall block-stack HUD becomes a slim pinned core chip plus compact canvas-corner chips on a full-width orbit map; a Settings screen toggles chip visibility; `F2` declutters. Orbit metrics and the active-burn readout are always-on.

**v0.12** — Plane-aware transfers, Lander, parachutes (ADRs 0005–0008). Numbered craft slots; dual-strategy Hohmann (combined Lambert vs. split coplanar raise); analytic-Kepler predictor fidelity + Line-of-Nodes split rendezvous; 2-stage Lander with surface staging; parachutes with auto-deploy on dynamic pressure.

**v0.11** — Launch chase-cam + landed/crashed lifecycle (ADR 0004). `ViewLaunch` auto-routes on pad launch; `CanSoftLand` gates soft Touchdown vs. Crashed; `[E]` end-flight removes wreckage. Lander silhouette with landing legs, hypergolic flame, and per-stage launch sprites.

**v0.10** — Planner + maneuver tooling. Rate-limited attitude slew, true plane-match and inclination burns, multi-rev porkchop with short/long-branch picker, rendezvous advisory (`K`), and perspective-tilt orbit view.

**v0.9** — The craft fleet grows up. Unified Target slot; KSP-style staging chain (`space`) with Saturn V; ground-launch primitives (launchpad spawn, surface SAS, pitch trim, LAUNCH HUD); rendezvous tooling (TCA/CA/DOCK READY, NavMode cycle `;`); `C` circularize; navball; solar lighting + eclipses.

**v0.8** — Multi-craft polish. RCS/monopropellant, multi-craft slate with per-craft burns, craft types and spawn form, docking + undocking, atmospheric drag, sim-time planet rotation with view-aware projection, adaptive warp clamps, iterate-for-target finite burns.

**v0.7** — Modding + manual flight + textures. External system/theme overlays, manual-flight stick (throttle + attitude keys), inclination-change planner, textured Earth/Moon/Mars/Jupiter.

**v0.6** — Planner UX + missions. Burn-at-next scheduler, projected-orbit HUD, finite-burn-aware iteration, moon→parent escape planner, click-only mouse, mission scaffold.

**v0.5** — Moons + visuals. Body hierarchy, major moons (Luna, Phobos/Deimos, Galilean, Titan, Enceladus), per-body color, vessel trail, HUD polish.

**v0.4** — Persistence. Versioned save/load envelope, mid-course refinement.

**v0.3** — Transfers. Lambert solver, porkchop plot, auto-plant Hohmann transfers.

**v0.2** — Burns. Spacecraft, impulsive burns, finite-burn integrator.

**v0.1** — Foundation. Heliocentric viewer, Verlet integrator, body catalog.
