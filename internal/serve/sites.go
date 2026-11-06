package serve

import (
	"log"

	"github.com/jasonfen/terminal-space-program/internal/relay"
	"github.com/jasonfen/terminal-space-program/internal/sessiondir"
	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// Surface-site ledger wiring (ADR 0066 decision 5), the sibling of dock.go.
// The ledger is relay.SiteLedger; this file is the serve-side glue: seed it at
// start, sync each seat's World with it every refresh, flush it to
// session.json synchronously under persistMu, and hand viewers the others'
// sites as w.PartnerSites.

// syncSites runs one seat's half. The first call for a World (a connect, or a
// Load that swapped the World) is the two-places reconcile; later calls plant,
// rename and remove from the diff. A change is flushed before this returns, so
// nothing writes after the seat's goroutine moves on.
func (m *reportingModel) syncSites(w *sim.World, handles map[string]string) {
	connect := m.sitesFor != w
	if connect {
		m.sitesFor, m.sitesKnown = w, map[uint64]int64{}
	}
	changed := m.srv.sites.SyncSites(w, m.owner, handles[m.owner], m.sitesKnown, connect)
	if changed || m.srv.siteFlushOwed() {
		if err := m.srv.persistSites(); err != nil {
			m.srv.markSiteFlushFailed(err)
		}
	}
	w.PartnerSites = m.srv.sites.PartnerSitesFor(w, m.owner, handles)
}

// persistSites writes the whole ledger under persistMu, the same guard the dock
// ledger takes, so two seats' flushes cannot interleave stale snapshots.
func (s *Server) persistSites() error {
	s.persistMu.Lock()
	defer s.persistMu.Unlock()
	err := s.store.SetSites(siteRecordsToLinks(s.sites.Snapshot()))
	if err == nil {
		s.sitePersistFailed = false
	}
	return err
}

func (s *Server) siteFlushOwed() bool {
	s.persistMu.Lock()
	defer s.persistMu.Unlock()
	return s.sitePersistFailed
}

func (s *Server) markSiteFlushFailed(err error) {
	s.persistMu.Lock()
	defer s.persistMu.Unlock()
	if !s.sitePersistFailed {
		log.Printf("site ledger: could not be written (%v); retrying every tick", err)
	}
	s.sitePersistFailed = true
}

func siteRecordsToLinks(recs []relay.SiteRecord) []sessiondir.SiteLink {
	out := make([]sessiondir.SiteLink, 0, len(recs))
	for _, r := range recs {
		out = append(out, sessiondir.SiteLink(r))
	}
	return out
}

func siteLinksToRecords(links []sessiondir.SiteLink) []relay.SiteRecord {
	out := make([]relay.SiteRecord, 0, len(links))
	for _, l := range links {
		out = append(out, relay.SiteRecord(l))
	}
	return out
}
