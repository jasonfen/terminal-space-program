// v0.16: schema v7 → v8 migration (ADR 0015). Pre-v8 saves predate the
// per-Vessel System binding: a Vessel had no SystemIdx because spacecraft
// were effectively Sol-only (the v0.1 cap). v8 binds every Vessel to one
// System for its lifetime via Craft.SystemIdx, an index into the
// name-sorted-Sol-first loaded systems.
//
// The migration derives each craft's SystemIdx from *which loaded System
// contains its PrimaryID* — not blindly 0 and not the world-level viewed
// SystemIdx — so normal Sol craft AND any craft a buggy interim Lumen
// build spawned onto a Lumen body both migrate to the right System. An
// unknown PrimaryID falls back to Sol (index 0), matching the old
// System-agnostic rehydration that treated all craft as live regardless.
//
// This needs the loaded systems (to map PrimaryID → System), so unlike the
// purely-positional v6→v7 pass it runs in Load after bodies.LoadAll, not on
// the payload alone.

package save

import "github.com/jasonfen/terminal-space-program/internal/bodies"

// migrateV7PayloadToV8 stamps each craft's SystemIdx from its PrimaryID
// against the loaded systems. Covers both the v5+ Crafts slice and the
// pre-v5 singular Craft pointer (worldFromPayload promotes the latter into
// the slate, so its SystemIdx must be set too).
func migrateV7PayloadToV8(p *Payload, systems []bodies.System) {
	for i := range p.Crafts {
		p.Crafts[i].SystemIdx = systemIdxForPrimary(systems, p.Crafts[i].PrimaryID)
	}
	if p.Craft != nil {
		p.Craft.SystemIdx = systemIdxForPrimary(systems, p.Craft.PrimaryID)
	}
}

// systemIdxForPrimary returns the index of the loaded System whose Bodies
// contain primaryID, or 0 (Sol) when no System owns it. Sol is always
// index 0 (LoadAll sorts by name, Sol first), so the fallback is the safe
// "treat as Sol" default the pre-v8 loader implied.
func systemIdxForPrimary(systems []bodies.System, primaryID string) int {
	for i := range systems {
		for j := range systems[i].Bodies {
			if systems[i].Bodies[j].ID == primaryID {
				return i
			}
		}
	}
	return 0
}
