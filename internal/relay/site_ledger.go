package relay

import (
	"sort"
	"sync"

	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// Surface-site ledger (ADR 0066 decision 5): every player's flags and wrecks,
// kept by the server beside the dock ledger so they outlive their owner's
// session and a restart. The identity of a site is (Owner, ID): the owner's
// key fingerprint and that owner's own site id. Every verb takes the owner and
// can reach only that owner's records, which is what makes rename and remove
// owner-only; the serve layer passes the connection's own fingerprint.
//
// BodyID is the catalog body id (what the wire and the ghosts use). A World
// stores the body's EnglishName, so SiteRecordOf / PartnerSitesFor convert.

// SiteRecord is one ledger entry.
type SiteRecord struct {
	Owner, OwnerHandle string
	ID                 uint64
	BodyID, System     string
	LatDeg, LonDeg     float64
	Kind               int // sim.SiteKind
	Name               string
	CraftID            uint64
	PlantedAtNano      int64
	UpdatedAtNano      int64
}

type siteKey struct {
	owner string
	id    uint64
}

// SiteLedger is the server's site store. Safe for concurrent use.
type SiteLedger struct {
	mu   sync.Mutex
	recs map[siteKey]SiteRecord
}

func NewSiteLedger() *SiteLedger { return &SiteLedger{recs: map[siteKey]SiteRecord{}} }

// Plant adds r, or replaces the entry with the same (Owner, ID) when r is at
// least as new. It reports whether the ledger changed.
func (l *SiteLedger) Plant(r SiteRecord) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	k := siteKey{r.Owner, r.ID}
	if old, ok := l.recs[k]; ok && (r.UpdatedAtNano < old.UpdatedAtNano || old == r) {
		return false
	}
	l.recs[k] = r
	return true
}

// Rename renames owner's site id at stamp. False for a site that is not
// owner's (the wrong fingerprint reaches nothing), an empty name, or a stamp
// older than the record's.
func (l *SiteLedger) Rename(owner string, id uint64, name string, stamp int64) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	k := siteKey{owner, id}
	r, ok := l.recs[k]
	if !ok || name == "" || stamp < r.UpdatedAtNano {
		return false
	}
	r.Name, r.UpdatedAtNano = name, stamp
	l.recs[k] = r
	return true
}

// Remove deletes owner's site id. False when owner has no such site.
func (l *SiteLedger) Remove(owner string, id uint64) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	k := siteKey{owner, id}
	if _, ok := l.recs[k]; !ok {
		return false
	}
	delete(l.recs, k)
	return true
}

// DropSitesOf removes every site owner planted (the purge) and returns how
// many went.
func (l *SiteLedger) DropSitesOf(owner string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := 0
	for k := range l.recs {
		if k.owner == owner {
			delete(l.recs, k)
			n++
		}
	}
	return n
}

// Clear empties the ledger (--reset-fleet).
func (l *SiteLedger) Clear() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.recs = map[siteKey]SiteRecord{}
}

// Snapshot is every record, ordered by owner then id so a flush is stable.
func (l *SiteLedger) Snapshot() []SiteRecord {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]SiteRecord, 0, len(l.recs))
	for _, r := range l.recs {
		out = append(out, r)
	}
	sort.Slice(out, func(a, b int) bool {
		if out[a].Owner != out[b].Owner {
			return out[a].Owner < out[b].Owner
		}
		return out[a].ID < out[b].ID
	})
	return out
}

// Seed installs records read from the session directory at start.
func (l *SiteLedger) Seed(recs []SiteRecord) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, r := range recs {
		l.recs[siteKey{r.Owner, r.ID}] = r
	}
}

func (l *SiteLedger) get(owner string, id uint64) (SiteRecord, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	r, ok := l.recs[siteKey{owner, id}]
	return r, ok
}

// SiteRecordOf converts one of w's own sites to a ledger record. False when
// the site's body is not in w's catalog (it cannot be named by id).
func SiteRecordOf(w *sim.World, s sim.Site, owner, handle string) (SiteRecord, bool) {
	for _, sys := range w.Systems {
		if sys.Name != s.System {
			continue
		}
		for _, b := range sys.Bodies {
			if b.EnglishName == s.Body {
				return SiteRecord{
					Owner: owner, OwnerHandle: handle, ID: s.ID, BodyID: b.ID, System: s.System,
					LatDeg: s.LatDeg, LonDeg: s.LonDeg, Kind: int(s.Kind), Name: s.Name,
					CraftID: s.CraftID, PlantedAtNano: s.PlantedAtNano, UpdatedAtNano: s.UpdatedAtNano,
				}, true
			}
		}
	}
	return SiteRecord{}, false
}

// siteOf is the inverse: the World form of a record, ok=false when the record's
// body id is not in w's catalog.
func siteOf(w *sim.World, r SiteRecord) (sim.Site, bool) {
	for _, sys := range w.Systems {
		if sys.Name != r.System {
			continue
		}
		for _, b := range sys.Bodies {
			if b.ID == r.BodyID {
				return sim.Site{
					ID: r.ID, Body: b.EnglishName, System: r.System, LatDeg: r.LatDeg, LonDeg: r.LonDeg,
					Kind: sim.SiteKind(r.Kind), Name: r.Name, CraftID: r.CraftID,
					PlantedAtNano: r.PlantedAtNano, UpdatedAtNano: r.UpdatedAtNano,
				}, true
			}
		}
	}
	return sim.Site{}, false
}

// SyncSites keeps owner's World and the ledger agreeing and reports whether
// the ledger changed (the caller then flushes).
//
// connect=true is the two-places reconcile (decision 5), per site by
// UpdatedAtNano: newest wins; a site the World has and the ledger lacks is
// published; one the ledger has for owner and the World lacks is put back.
// connect=false is the steady tick: a site in the World that is new or whose
// stamp moved since known was recorded is planted or renamed in the ledger, and
// one in known that the World no longer has was deleted. known (id to stamp) is
// the caller's memory of what it last agreed on and is rewritten here.
func (l *SiteLedger) SyncSites(w *sim.World, owner, handle string, known map[uint64]int64, connect bool) bool {
	changed := false
	have := make(map[uint64]bool, len(w.Sites))
	for i := range w.Sites {
		s := &w.Sites[i]
		have[s.ID] = true
		rec, inLedger := l.get(owner, s.ID)
		if connect && inLedger && rec.UpdatedAtNano > s.UpdatedAtNano {
			if ws, ok := siteOf(w, rec); ok {
				ws.Owner = ""
				*s = ws
			}
			continue
		}
		if !connect && known[s.ID] == s.UpdatedAtNano && inLedger {
			continue
		}
		if r, ok := SiteRecordOf(w, *s, owner, handle); ok && l.Plant(r) {
			changed = true
		}
	}
	if connect {
		for _, r := range l.recordsOf(owner) {
			if have[r.ID] {
				continue
			}
			if s, ok := siteOf(w, r); ok {
				w.Sites = append(w.Sites, s)
			}
		}
		for _, s := range w.Sites {
			if s.ID > w.NextSiteID {
				w.NextSiteID = s.ID
			}
		}
	} else {
		for id := range known {
			if !have[id] && l.Remove(owner, id) {
				changed = true
			}
		}
	}
	clear(known)
	for _, s := range w.Sites {
		known[s.ID] = s.UpdatedAtNano
	}
	return changed
}

func (l *SiteLedger) recordsOf(owner string) []SiteRecord {
	var out []SiteRecord
	for _, r := range l.Snapshot() {
		if r.Owner == owner {
			out = append(out, r)
		}
	}
	return out
}

// PartnerSitesFor is the ledger as w's viewer sees it: every record that is
// not the viewer's own, converted to w's catalog (a record whose body w does
// not know is left out), handle from the roster else the record's.
func (l *SiteLedger) PartnerSitesFor(w *sim.World, viewer string, handles map[string]string) []sim.PartnerSite {
	var out []sim.PartnerSite
	for _, r := range l.Snapshot() {
		if r.Owner == viewer {
			continue
		}
		s, ok := siteOf(w, r)
		if !ok {
			continue
		}
		s.Owner = r.Owner
		h := handles[r.Owner]
		if h == "" {
			h = r.OwnerHandle
		}
		if h == "" {
			h = r.Owner
		}
		out = append(out, sim.PartnerSite{Site: s, Handle: h})
	}
	return out
}
