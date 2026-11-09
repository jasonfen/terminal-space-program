// Schema v22 -> v23 (ADR 0067 P22): each vessel remembers its own navball
// frame (Craft.nav_mode). A v22 save held one frame for the whole game in the
// envelope's nav_mode; it moves onto the active vessel, the others start on
// ORBIT, and the sim reconciles a frame the vessel cannot offer on arrival.
// One-line reason for a new file: one migration per file, as v21 -> v22.

package save

func migrateV22PayloadToV23(p *Payload) {
	if i := p.ActiveCraftIdx; i >= 0 && i < len(p.Crafts) {
		p.Crafts[i].NavMode = p.NavMode
	}
	p.NavMode = 0
}
