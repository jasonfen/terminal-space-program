# Vessel is the canonical noun for a player-controlled craft

The Go type is `spacecraft.Spacecraft` and field names use the short form
`Craft` (`World.Crafts`, `ActiveCraftIdx`, `FocusCraft`, `TargetCraft`).
Three names for one concept were in active use across types, fields, and
comments. We canonicalized prose, docs, and player-facing strings on
**Vessel** — KSP's term and the one with the strongest player-familiarity
prior — while leaving the existing Go identifiers in place; they should
drift toward `Vessel` opportunistically rather than being renamed in one
sweep.

## Considered Options

- **Craft** — the dominant field-level form (`Crafts`, `FocusCraft`,
  `TargetCraft`). Recommended by the grilling agent because it would
  require the *least* code churn — every field name already matches.
  Rejected because "craft" is a generic English noun and a verb, which
  reads ambiguously in prose ("craft a maneuver" vs "the craft is
  burning"). Player familiarity also favored Vessel.
- **Spacecraft** — the Go type name and the most formal/aerospace-correct
  option. Rejected because it reads as a literal English noun in
  player-facing strings rather than as the game-specific noun a player
  thinks in.
- **Vessel** — KSP's term. Inherits genre familiarity at the cost of
  importing brand language and being the *outlier* relative to the
  current type-and-field naming. Picked anyway because player intuition
  beats code-internal consistency for the player-facing vocabulary
  layer; the code can drift to match the docs over time.

## Consequences

- Prose, docs, ADRs, issues, and player-facing strings say "Vessel."
- Go identifiers stay as `Spacecraft` / `Craft*` for now; no big-bang
  rename. New code may use `Vessel` where it makes sense without
  triggering a sweep.
- `CONTEXT.md` records `Spacecraft` and `Craft` as legacy aliases under
  the Vessel entry so future readers don't waste time hunting for a
  hidden semantic distinction.
