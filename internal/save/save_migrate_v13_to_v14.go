// Schema v13 -> v14: Craft gains RendezvousPlan (the last Rendezvous Burn
// planted from the picker, kept after the node fires). Older saves simply have
// no plan, so nothing needs transforming; the bump exists so an older binary
// refuses a v14 envelope instead of silently dropping the key (the repo rule:
// bump the version + add a migration whenever persisted state shape changes).

package save

// migrateV13PayloadToV14 is an identity transform.
func migrateV13PayloadToV14(p *Payload) {}
