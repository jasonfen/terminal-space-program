// ADR 0049 decision 8 (#453): schema v10 -> v11 migration. Craft gains
// HeadingTrim, the player-commanded launch heading ApplyHeadingTrim
// rotates the thrust direction toward before PitchTrim tilts it.
// HeadingTrim stores a SIGNED OFFSET from due east (the same "offset
// from natural, zero means no trim" shape as PitchTrim, not an
// absolute compass bearing — see spacecraft.Spacecraft.HeadingTrim's
// doc comment), so the due-east default the ADR specifies IS Go's
// float64 zero value. A pre-v11 payload carries no heading_trim key at
// all, and json.Unmarshal already leaves an absent field at that same
// zero value, so the wire shape itself needs no transform: this is an
// identity pass, the same shape TestMigrateV9PayloadToV10IsIdentity
// pins for the prior bump. It stays its own named function (rather
// than a bare version-number bump with an implicit no-op) to match
// that precedent — a payload's migration history stays a straight
// line of named functions keyed off f.Version — and so a future
// migration that DOES need a real per-craft loop here has an obvious
// place to add one rather than reaching for a first loop from scratch.
package save

// migrateV10PayloadToV11 is an identity transform — see the package
// comment above for why HeadingTrim needs no field migration despite
// being new on the wire in v11.
func migrateV10PayloadToV11(p *Payload) {}
