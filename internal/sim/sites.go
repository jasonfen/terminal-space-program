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
}

func (w *World) siteByID(id uint64) (Site, bool) {
	for _, s := range w.Sites {
		if s.ID == id {
			return s, true
		}
	}
	return Site{}, false
}

// SetTargetSite aims at a site of this World. An unknown id clears the target,
// as SetTargetBody does for a bad index.
func (w *World) SetTargetSite(id uint64) {
	if _, ok := w.siteByID(id); !ok {
		w.ClearTarget()
		return
	}
	w.Target = Target{Kind: TargetSite, SiteID: id}
	w.mirrorTargetToActiveCraft()
	w.reconcileNavMode()
}

// targetSiteState is the targeted site's surface state in its body's frame
// (R, V) plus the body, from the landed integrator's own formula. ok=false for
// a stale id or a site on a body of another system.
func (w *World) targetSiteState() (r, v orbital.Vec3, primary bodies.CelestialBody, ok bool) {
	s, found := w.siteByID(w.Target.SiteID)
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

// RemoveSite deletes one of your own sites; a target on it clears to
// TargetNone (ADR 0066 decision 7).
func (w *World) RemoveSite(id uint64) bool {
	for i := range w.Sites {
		if w.Sites[i].ID != id {
			continue
		}
		w.Sites = append(w.Sites[:i], w.Sites[i+1:]...)
		if w.Target.Kind == TargetSite && w.Target.SiteID == id {
			w.ClearTarget()
		}
		return true
	}
	return false
}
