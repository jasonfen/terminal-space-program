// Package sim - surface sites (ADR 0066): flags where a vessel touched down,
// wrecks where one crashed. A site is World state saved with the game and a
// target kind of its own (TargetSite). One-line reason for a new file: a new
// production concept, not a slice of an existing one.

package sim

import (
	"fmt"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// SiteKind says what a Site marks. Only flag and wreck are planted today;
// point and zone are named so the save vocabulary does not shift later.
type SiteKind int

const (
	SiteFlag SiteKind = iota
	SiteWreck
	SitePoint
	SiteZone
)

// Site is a named point on a body's surface (ADR 0066 decision 1). Owner is
// empty for your own sites; a partner's arrive in a later slice.
type Site struct {
	ID            uint64
	Body, System  string // body EnglishName and System name
	LatDeg        float64
	LonDeg        float64
	Kind          SiteKind
	Name          string
	CraftID       uint64 // the vessel that planted it
	Owner         string
	PlantedAtNano int64 // wall clock; decision 5 reconciles by it
	UpdatedAtNano int64
	// LZCount, LZNames and Layout (ADR 0068): zero is one LZ in the honeycomb.
	// LZCount and LZNames are saved and in the ledger (schema 24, meta 7);
	// LZNames[n-1] is LZ n's name, "" or absent is "LZ-n". Layout is shipped
	// zones only, never saved.
	LZCount int
	LZNames []string
	Layout  LZLayout
}

// PartnerSite is another player's site read from the server ledger (ADR 0066
// decision 5). Site.Owner is their key fingerprint and Site.Body is already
// resolved to this catalog's EnglishName; Handle is their display name.
// Transient: rebuilt each session refresh, never saved.
type PartnerSite struct {
	Site
	Handle string
}

// targetSite resolves the target's site: one of yours (GhostOwner empty) or a
// partner's, keyed by owner and that owner's own site id.
func (w *World) targetSite() (Site, bool) { return w.landingSiteOf(w.Target, w.ActiveCraft()) }

// siteOfTarget is the raw site lookup for any aim (ADR 0067 decision 1). Only
// landingSiteOf and claimLZ call it: every surface-point reader goes through
// landingSiteOf, which resolves the LZ (ADR 0068).
func (w *World) siteOfTarget(t Target) (Site, bool) {
	if t.GhostOwner == "" {
		return w.siteByID(t.SiteID)
	}
	for _, p := range w.PartnerSites {
		if p.Owner == t.GhostOwner && p.ID == t.SiteID {
			return p.Site, true
		}
	}
	return Site{}, false
}

// TargetSiteResolves reports whether the aimed site still exists (a partner's
// may vanish, or a solo save may hold a partner aim).
func (w *World) TargetSiteResolves() bool { _, ok := w.targetSite(); return ok }

// SetTargetPartnerSite aims at a partner's site (a PARTNERS row). An unknown
// pair clears the target.
func (w *World) SetTargetPartnerSite(owner string, id uint64) { w.SetTargetPartnerSiteLZ(owner, id, 0) }

// SetTargetPartnerSiteLZ is SetTargetPartnerSite with an LZ chosen (0 = auto).
func (w *World) SetTargetPartnerSiteLZ(owner string, id uint64, lz int) {
	for _, p := range w.PartnerSites {
		if p.Owner == owner && p.ID == id {
			w.Target = Target{Kind: TargetSite, SiteID: id, GhostOwner: owner, LZ: lz}
			w.mirrorTargetToActiveCraft()
			w.reconcileNavMode()
			return
		}
	}
	w.ClearTarget()
}

func (w *World) siteByID(id uint64) (Site, bool) {
	for _, s := range w.Sites {
		if s.ID == id {
			return s, true
		}
	}
	return shippedZoneByID(id)
}

// SetTargetSite aims at a site of this World. An unknown id clears the target,
// as SetTargetBody does for a bad index.
func (w *World) SetTargetSite(id uint64) { w.SetTargetSiteLZ(id, 0) }

// SetTargetSiteLZ is SetTargetSite with an LZ chosen (0 = auto, ADR 0068
// decision 6).
func (w *World) SetTargetSiteLZ(id uint64, lz int) {
	if _, ok := w.siteByID(id); !ok {
		w.ClearTarget()
		return
	}
	w.Target = Target{Kind: TargetSite, SiteID: id, LZ: lz}
	w.mirrorTargetToActiveCraft()
	w.reconcileNavMode()
}

// targetSiteState is the targeted site's surface state in its body's frame
// (R, V) plus the body, from the landed integrator's own formula. ok=false for
// a stale id or a site on a body of another system.
func (w *World) targetSiteState() (r, v orbital.Vec3, primary bodies.CelestialBody, ok bool) {
	s, found := w.targetSite()
	if !found {
		return
	}
	sys := w.System()
	if s.System != sys.Name {
		return
	}
	for _, b := range sys.Bodies {
		if b.EnglishName == s.Body {
			r, v = surfacePointState(b, s.LatDeg, s.LonDeg, w.Clock.SimTime)
			return r, v, b, true
		}
	}
	return
}

// plantSite appends a site for c at (lat, lon). The name is the vessel's plus
// suffix, numbered on repeat by a never-reused counter (ADR 0059 decision 7):
// "Lander-1 site", then "Lander-1 site 2".
func (w *World) plantSite(c *spacecraft.Spacecraft, kind SiteKind, suffix string, lat, lon float64) {
	base := c.Name + " " + suffix
	if w.NameCounters == nil {
		w.NameCounters = map[string]uint64{}
	}
	n := w.NameCounters[base] + 1
	w.NameCounters[base] = n
	name := base
	if n > 1 {
		name = fmt.Sprintf("%s %d", base, n)
	}
	sys := w.System()
	if c.SystemIdx >= 0 && c.SystemIdx < len(w.Systems) {
		sys = w.Systems[c.SystemIdx]
	}
	w.NextSiteID++
	now := time.Now().UnixNano()
	w.Sites = append(w.Sites, Site{
		ID: w.NextSiteID, Body: c.Primary.EnglishName, System: sys.Name,
		LatDeg: lat, LonDeg: lon, Kind: kind, Name: name, CraftID: c.ID,
		PlantedAtNano: now, UpdatedAtNano: now,
	})
}

// RenameSite renames one of your own sites. False for an unknown id or an
// empty name.
func (w *World) RenameSite(id uint64, name string) bool {
	if name == "" {
		return false
	}
	for i := range w.Sites {
		if w.Sites[i].ID == id {
			w.Sites[i].Name = name
			w.Sites[i].UpdatedAtNano = time.Now().UnixNano()
			return true
		}
	}
	return false
}

// AddLZ gives one of your own flags another LZ, up to SiteMaxLZs (decision 2).
// False for a partner's site, a shipped zone, a wreck, or a full site.
func (w *World) AddLZ(id uint64) bool {
	for i := range w.Sites {
		s := &w.Sites[i]
		if s.ID != id || s.Kind == SiteWreck || s.LZs() >= SiteMaxLZs {
			continue
		}
		s.LZCount = s.LZs() + 1
		s.UpdatedAtNano = time.Now().UnixNano()
		return true
	}
	return false
}

// RemoveLZ takes the last LZ of one of your own flags away unless it is the
// only one, or a vessel stands on it or holds a claim on it (decisions 2, 12).
func (w *World) RemoveLZ(id uint64) bool {
	for i := range w.Sites {
		s := &w.Sites[i]
		if s.ID != id || s.Kind == SiteWreck || s.LZs() <= 1 || w.lzTaken(*s, s.LZs(), nil) {
			continue
		}
		n := s.LZs()
		s.LZCount = n - 1
		s.LZNames = s.LZNames[:min(len(s.LZNames), n-1)]
		s.UpdatedAtNano = time.Now().UnixNano()
		if w.Target.Kind == TargetSite && w.Target.GhostOwner == "" && w.Target.SiteID == id && w.Target.LZ == n {
			w.Target.LZ = 0 // the LZ chosen on the card is gone: back to auto
			w.mirrorTargetToActiveCraft()
		}
		return true
	}
	return false
}

// RemoveSite deletes one of your own sites; a target on it clears to
// TargetNone (ADR 0066 decision 7).
func (w *World) RemoveSite(id uint64) bool {
	for i := range w.Sites {
		if w.Sites[i].ID != id {
			continue
		}
		w.Sites = append(w.Sites[:i], w.Sites[i+1:]...)
		if w.Target.Kind == TargetSite && w.Target.GhostOwner == "" && w.Target.SiteID == id {
			w.ClearTarget()
		}
		return true
	}
	return false
}

// SiteWorldPos is a site's inertial position (its body's centre plus the
// co-rotating surface point) and the body it sits on. ok=false for a site on
// a body of another system or an unknown body, which the map does not draw.
func (w *World) SiteWorldPos(s Site) (pos orbital.Vec3, body bodies.CelestialBody, ok bool) {
	sys := w.System()
	if s.System != sys.Name {
		return
	}
	for _, b := range sys.Bodies {
		if b.EnglishName == s.Body {
			r, _ := surfacePointState(b, s.LatDeg, s.LonDeg, w.Clock.SimTime)
			return w.BodyPosition(b).Add(r), b, true
		}
	}
	return
}
