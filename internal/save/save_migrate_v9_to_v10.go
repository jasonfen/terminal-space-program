// #294 review finding 6: schema v9 → v10 migration. Target.Kind's
// persisted vocabulary widened to include TargetGhost (an already-live
// value on the wire — see save.go's Target / Node / ActiveBurn
// GhostOwner / TargetGhostOwner fields, all additive/omitempty), so a
// v9-declaring envelope written by a pre-fix binary and a v10 envelope
// written by this one are byte-for-byte identical wherever a ghost
// isn't targeted, and differ only by the version number and the new
// ghost_owner / target_ghost_owner keys when one is. Nothing on the
// wire shape needs transforming — the whole point of the bump is that
// Load's version gate now refuses an OLDER binary reading a v10
// envelope, rather than letting it silently drop the unfamiliar keys
// and reconstruct a permanently-unresolvable ghost target with no code
// path to explain that to the player (the repo rule: bump the version +
// add a migration whenever persisted state shape changes).

package save

// migrateV9PayloadToV10 is an identity transform — see the package
// comment above for why a real field migration isn't needed here.
// Kept as its own named pass (rather than skipped entirely) to match
// every other version bump's precedent: a payload's migration history
// stays a straight line of named functions keyed off f.Version, not a
// version bump with an implicit no-op hidden in the gate check alone.
func migrateV9PayloadToV10(p *Payload) {}
