// Schema v16 -> v17 (ADR 0059 decision 10): Payload gains name_counters and
// Craft gains batch_id, batch_lead, batch_clamp_exempt_until_unix_nano and
// raise. A save written before them has none of the keys, which decodes to
// "no counters (primed from the slate on first use), no batch, no order":
// exactly the truth for every vessel saved before this ADR. The bump exists so
// a v16 binary refuses a v17 envelope instead of dropping a raise order.

package save

// migrateV16PayloadToV17 is an identity transform.
func migrateV16PayloadToV17(p *Payload) {}
