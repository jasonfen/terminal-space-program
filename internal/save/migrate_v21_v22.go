// Schema v21 -> v22 (ADR 0067 decision 13): Craft.landing, the Site+ landing
// autopilot. A v21 save has none, which decodes to the truth (not engaged, zero
// aim and navigation offsets), so nothing is transformed; the step drops any
// autopilot a hand-edited v21 envelope carries, since no v21 build wrote one
// and a half-engaged state would fly a vessel nobody armed.
// One-line reason for a new file: one migration per file, as v14 -> v15.

package save

func migrateV21PayloadToV22(p *Payload) {
	for i := range p.Crafts {
		p.Crafts[i].Landing = nil
	}
}
