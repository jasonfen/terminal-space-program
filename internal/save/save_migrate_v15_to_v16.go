// Schema v15 -> v16 (ADR 0058 decision 13): Stage gains lit_with_above and
// Craft gains booster_throttle. A save written before them has neither key,
// which decodes to "serial, boosters follow the core": exactly the truth for
// every stage saved before this ADR. The mark is deliberately not re-derived
// from the catalog, so an SLS in a v15 save keeps flying as it was saved. The
// bump exists so a v15 binary refuses a v16 envelope instead of dropping the
// keys.

package save

// migrateV15PayloadToV16 is an identity transform.
func migrateV15PayloadToV16(p *Payload) {}
