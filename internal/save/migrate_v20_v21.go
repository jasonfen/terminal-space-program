// Schema v20 -> v21 (ADR 0066 decision 18): Payload.sites, next_site_id and
// Target.site_id. A v20 save has none of them, which decodes to the truth (no
// sites, no site target), so nothing is transformed; the step only keeps the
// id counter ahead of any site a hand-edited or partial payload carries.
// One-line reason for a new file: one migration per file, as v14 -> v15.

package save

func migrateV20PayloadToV21(p *Payload) {
	for _, s := range p.Sites {
		if s.ID > p.NextSiteID {
			p.NextSiteID = s.ID
		}
	}
}
