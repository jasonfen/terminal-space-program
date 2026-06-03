# The HUD is a slim core-telemetry column plus declutter-able canvas chips

The orbit-screen HUD is a vertical stack of up to ~12 conditional blocks
(`VESSEL`, `PROPELLANT`, `ATTITUDE`, `LAUNCH`, `TARGET`, `STAGES`,
`NODES`, `CAPTURE PREVIEW`, `PROJECTED ORBIT`, …) with **no height
budget**. The canvas is bounded to `totalRows-4`, but the HUD renders as
tall as its content needs, so with a target set, a launch in progress, a
7-stage Apollo stack, and planted nodes, the HUD runs well below the
canvas and the terminal scrolls to the bottom of the bar — hiding the
title and the orbit view (`orbit.go:846` only computed a mouse-offset; it
never prevented the overflow). The decision: the HUD becomes a **slim,
always-visible column of irreducible vessel telemetry** (name, primary,
fuel %, Δv budget, throttle, velocity), and every other block becomes a
compact **chip** — a 2–4 row overlay composited onto a canvas corner like
the existing navball panel. A chip renders iff it is *enabled in
Settings* **and** *contextually relevant* **and** *not declutter-hidden*.
Variable-length lists stay fixed-height: the **Nodes** chip shows the next
node plus a `(+N → [m])` count with the full list on the maneuver screen;
the **Stages** chip shows pips (`●●●○○`) plus active N/M, with per-stage
detail remaining a spawn-time concern. A new **Settings screen** (a menu
entry) toggles each chip's default visibility — persisted to a global
`settings.json` in `$XDG_CONFIG_HOME/terminal-space-program/` alongside
`theme.json` — and **F2** momentarily hides all overlays for a clean view.
Defaults are all-chips-on, so no information is lost versus today; the win
is that every block is bounded-height and the frame never scrolls.

## Considered Options

- **Internal scrollable HUD pane.** Keep every block, cap the HUD at the
  canvas height, let the player scroll it independently. Rejected: it
  turns a glanceable instrument panel into something you have to scroll
  to read, and adds keybindings to an already-crowded 59-key surface.
- **Denser blocks only, no hiding.** Compress every block (more
  two-column, abbreviations) so the worst case fits. Rejected: there is
  still a hard content ceiling past which it cannot fit, and density
  hurts readability for the common case to serve the rare one.
- **Pure phase-driven placement, no toggles.** Let flight phase decide
  what shows and where, with no player control. Rejected as the *whole*
  answer: it removes recourse when the auto-rules show or hide against a
  player's taste. Retained as the *default-selection* heuristic underneath
  the Settings overrides (chips are contextually gated by phase/state).
- **Full-width canvas, pinned core chips.** No persistent column; even
  vessel telemetry is a chip, marked "pinned" to survive declutter.
  Rejected: core numbers would permanently occlude a corner of the orbit,
  and numeric reads are cleaner in a column than floating on the canvas —
  and the declutter key must never be able to hide fuel/Δv mid-burn.
- **Persist prefs in the save file (schema v6→v7).** Rejected: UI
  visibility is an application preference, not game state. It belongs in
  global config (the `theme.json` precedent), applies across all games,
  and avoids a save-schema bump + migration.

## Consequences

- **A Settings screen and global UI preferences are new architecture.**
  There is no settings screen today and the save persists no UI prefs.
  `settings.json` is loaded from `$XDG_CONFIG_HOME/terminal-space-program/`
  (or `~/.config/...`); a missing file means defaults (all chips on),
  preserving current behaviour. It becomes the natural home for future
  prefs (units, etc.). Kept separate from `theme.json` — colour vs.
  visibility are distinct concerns.
- **One new keybinding (F2 = declutter).** Chosen because it is free and
  matches the KSP "toggle UI" convention; momentary hide-all is an instant
  gesture a menu trip would ruin. All *other* visibility control lives in
  the Settings screen, adding zero per-chip keys.
- **Chips are compact overlays, not panels.** They reuse the navball's
  canvas-compositing path (`composeNavballOverlay`) but are bounded to a
  few rows so several can coexist without the occlusion a 34×19 panel
  would cause. The navball stays the one large panel and is itself
  declutter-hideable.
- **The maneuver screen becomes the canonical full Nodes list.** The HUD
  no longer enumerates nodes; it summarises. This is deliberate — the full
  enumerable/editable list already lives there (`[m]`).
- **Core telemetry is fixed, not configurable.** The slim column's
  contents are not user-toggleable; only chips are. This keeps an
  irreducible always-on read and a simple mental model.
- Scope is a feature slice (new screen + config persistence + a placement
  refactor of the orbit HUD + one keybinding). It goes on a feature branch
  with a PR; the slice contract belongs in the cycle plan when sliced.
