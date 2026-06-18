package bodies

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// CatalogHash returns a sha256 hex of the canonical JSON encoding of
// every loaded system, sorted by name (Sol first). Stamped into save
// files so a load can reject saves built against a stale universe —
// e.g. after a v0.5 systems-JSON edit adds Luna and rewrites Body
// indices, the old save's body references no longer line up.
//
// Hash is over re-marshalled structs, not raw file bytes, so cosmetic
// JSON whitespace edits don't churn the hash. Only semantic catalog
// changes (new bodies, mass / element edits) bump it.
//
// The body Texture spec (ADR 0024) is cosmetic, not semantic, so it is
// zeroed in a hash-specific view before hashing — adding or editing a
// texture must never reject an existing save. `json:"-"` can't be used
// on the field itself because that would also block *loading* it; the
// exclusion lives here, at the hash boundary.
func CatalogHash() (string, error) {
	systems, err := LoadAll()
	if err != nil {
		return "", fmt.Errorf("load systems for catalog hash: %w", err)
	}
	b, err := json.Marshal(hashView(systems))
	if err != nil {
		return "", fmt.Errorf("marshal canonical systems: %w", err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

// hashView returns a shallow copy of the loaded systems with every
// body's cosmetic Texture zeroed, so CatalogHash tracks only semantic
// catalog shape. The originals are untouched (the Bodies slices are
// copied, not aliased) so the live catalog keeps its textures.
func hashView(systems []System) []System {
	out := make([]System, len(systems))
	for i, s := range systems {
		bodies := make([]CelestialBody, len(s.Bodies))
		copy(bodies, s.Bodies)
		for j := range bodies {
			bodies[j].Texture = nil
		}
		s.Bodies = bodies
		out[i] = s
	}
	return out
}

// LookupByID searches every system for a body with the given ID and
// returns it by value. Used by the save/load layer to rehydrate the
// craft's primary across system boundaries — v0.1 craft is locked to
// Sol, but the save schema doesn't bake that assumption in.
func LookupByID(systems []System, id string) (CelestialBody, bool) {
	for _, s := range systems {
		for _, b := range s.Bodies {
			if b.ID == id {
				return b, true
			}
		}
	}
	return CelestialBody{}, false
}
