// v0.21: schema v8 → v9 migration (ADR 0025). Pre-v9 saves model a
// mission as a single typed predicate (the old missions.Mission shape:
// type + params + a flat status). v0.21 inverts the vocabulary — a Mission
// is now an ordered list of Objectives plus campaign metadata (program tag,
// requires/unlocks edges, per-objective status) — so the on-disk shape
// changes fundamentally.
//
// Field-migrating the old single-predicate progress into the new nested
// shape would carry little value (the old catalog was four standalone
// predicates with no ladder), so this is a re-seed migration: drop the old
// mission state entirely. With Payload.Missions left nil, worldFromPayload
// reseeds from the current catalog (embedded + user overlay), so the player
// picks up the new tutorial→challenge ladder fresh. Ladder progression and
// per-objective status persist going forward under v9.

package save

// migrateV8PayloadToV9 drops the pre-v0.21 single-predicate mission
// progress so the load path reseeds the new Mission/Objective ladder from
// the catalog. Operates on the payload alone, so it runs before
// rehydration like the positional v6→v7 pass.
func migrateV8PayloadToV9(p *Payload) {
	p.Missions = nil
}
