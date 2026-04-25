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
func CatalogHash() (string, error) {
	systems, err := LoadAll()
	if err != nil {
		return "", fmt.Errorf("load systems for catalog hash: %w", err)
	}
	b, err := json.Marshal(systems)
	if err != nil {
		return "", fmt.Errorf("marshal canonical systems: %w", err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
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
