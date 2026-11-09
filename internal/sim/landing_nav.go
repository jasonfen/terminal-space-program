// Package sim - the landing computer's site (ADR 0067 decisions 3, 4, 6). One-
// line reason for a new file: the navigation error and the aim offset are a
// concept of their own (a second picture of where the site is), not a slice of
// the autopilot's guidance law in landing_autopilot.go (already 790 lines).

package sim

import (
	"encoding/binary"
	"hash/fnv"
	"math"
	"math/rand"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/render"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
	"github.com/jasonfen/terminal-space-program/internal/tui/readout"
)

// vesselLinked reports the vessel's own CommNet connection, the one COMMS shows
// (P21). Unlike CanCommandCraft there is no crewed bypass, and a site's
// COVERAGE does not count.
func (w *World) vesselLinked(c *spacecraft.Spacecraft) bool {
	if w.CommGraph == nil {
		w.RecomputeCommGraph()
	}
	return w.CommGraph.HasConnection(c.ID)
}

// apSiteKey is the id of the surface point the vessel is aimed at, for the
// error's seed: a site id or a landed vessel's id.
func (w *World) apSiteKey(c *spacecraft.Spacecraft) uint64 {
	t := c.Target
	if w.ActiveCraft() == c {
		t = w.Target
	}
	if t.Kind == TargetSite {
		return t.SiteID
	}
	return t.CraftID
}

// navSeed is hash(vessel id, site id, burn tick) for the deterministic draw:
// every peer that computes it agrees, and a reload cannot re-roll it.
func navSeed(vesselID, siteID uint64, tick time.Time) int64 {
	h := fnv.New64a()
	var b [24]byte
	binary.LittleEndian.PutUint64(b[0:], vesselID)
	binary.LittleEndian.PutUint64(b[8:], siteID)
	binary.LittleEndian.PutUint64(b[16:], uint64(tick.UnixNano()))
	h.Write(b[:])
	return int64(h.Sum64())
}

// navDraw is one (along, across) normal draw at the given 1-sigmas.
func navDraw(seed int64, sigmaAlong, sigmaAcross float64) (along, across float64) {
	r := rand.New(rand.NewSource(seed))
	return r.NormFloat64() * sigmaAlong, r.NormFloat64() * sigmaAcross
}

// drawNavError draws the computer's-site error for this landing attempt (decision
// 3): modern-grade for a modern vessel or an Apollo-era one linked right now
// (P21), the era's own no-link sigma otherwise. force redraws (N re-solving);
// otherwise an offset already drawn for this site is kept.
func (w *World) drawNavError(c *spacecraft.Spacecraft, force bool) {
	ap := c.Landing
	if ap == nil {
		ap = &spacecraft.LandingAutopilot{}
		c.Landing = ap
	}
	key, rt := w.apSiteKey(c), w.apRuntimeFor(c)
	if ap.NavSeeded && !force && (!rt.navKnown || rt.navSite == key) {
		return
	}
	sa, sc, apollo := c.NavSigma()
	ap.NavFixed = !apollo || w.vesselLinked(c)
	if ap.NavFixed {
		sa, sc = NavSigmaModernAlongM, NavSigmaModernAcrossM
	}
	if w.navErrorOff {
		sa, sc = 0, 0
	}
	ap.NavAlongM, ap.NavAcrossM = navDraw(navSeed(c.ID, key, w.Clock.SimTime), sa, sc)
	ap.NavSeeded = true
	rt.navKnown, rt.navSite = true, key
}

// updateNavOnLink is P20: an Apollo-era vessel drawn with no link gets the
// ground's update on the first link between the burn and the brake. The site
// moves to the modern-grade draw, once, and the flash says how far.
func (w *World) updateNavOnLink(c *spacecraft.Spacecraft) {
	ap := c.Landing
	if ap.Phase != spacecraft.PhaseCoast || !ap.NavSeeded || ap.NavFixed || !w.vesselLinked(c) {
		return
	}
	oldA, oldC := ap.NavAlongM, ap.NavAcrossM
	ap.NavAlongM, ap.NavAcrossM = navDraw(navSeed(c.ID, w.apSiteKey(c), w.Clock.SimTime), NavSigmaModernAlongM, NavSigmaModernAcrossM)
	ap.NavFixed = true
	w.PendingLandingEvents = append(w.PendingLandingEvents, LandingEvent{
		CraftName: c.Name, Update: true, DistanceM: math.Hypot(ap.NavAlongM-oldA, ap.NavAcrossM-oldC),
	})
	w.recordRowFor(c, "ground update: site moved "+readout.Distance(math.Hypot(ap.NavAlongM-oldA, ap.NavAcrossM-oldC)))
}

// computerOffset is where the computer is flying to, from the real site: its
// navigation error plus the pilot's aim offset, along and across the track.
func computerOffset(c *spacecraft.Spacecraft) (along, across float64) {
	if ap := c.Landing; ap != nil {
		return ap.NavAlongM + ap.AimAlongM, ap.NavAcrossM + ap.AimAcrossM
	}
	return 0, 0
}

// computerSitePoint shifts siteR (a primary-relative surface point) by the
// computer's offset, along the same axes apStopErr reads.
func computerSitePoint(c *spacecraft.Spacecraft, siteR orbital.Vec3) orbital.Vec3 {
	al, ac := computerOffset(c)
	n := apTrackNormal(c, c.State)
	if (al == 0 && ac == 0) || n.Norm() == 0 {
		return siteR
	}
	aimHat := siteR.Unit()
	tHat := n.Cross(aimHat).Unit()
	return siteR.Add(tHat.Scale(al)).Add(tHat.Cross(aimHat).Scale(ac))
}

// computerSiteLatLon is the computer's site (decision 4) as a lat/lon: the real
// site shifted by the vessel's offset.
func (w *World) computerSiteLatLon(c *spacecraft.Spacecraft, lat, lon float64) (float64, float64) {
	if al, ac := computerOffset(c); al == 0 && ac == 0 {
		return lat, lon
	}
	t := w.Clock.SimTime
	q := computerSitePoint(c, siteVec(c, lat, lon, t).Scale(c.Primary.RadiusMeters()))
	return render.WorldToBodyFixed(c.Primary, unitV(q), t)
}

// siteShiftRung is one aim-arrow press for a ground range to the computer's
// site: 1 km beyond SiteShiftFarRangeM, 250 m beyond SiteShiftMidRangeM, 25 m
// inside (decision 6); the boundary belongs to the nearer rung.
func siteShiftRung(groundM float64) float64 {
	switch {
	case groundM > SiteShiftFarRangeM:
		return SiteShiftRungFarM
	case groundM > SiteShiftMidRangeM:
		return SiteShiftRungMidM
	}
	return SiteShiftRungNearM
}

// siteShiftStep is one aim press at groundM: the rung, or with alt
// SiteShiftFineM or a tenth of the rung when that is smaller.
func siteShiftStep(groundM float64, fine bool) float64 {
	step := siteShiftRung(groundM)
	if fine {
		step = math.Min(SiteShiftFineM, step/10)
	}
	return step
}

// NudgeLandingAim is an arrow under Site+ auto (decision 6): along +1 long, -1
// short; across +1 right, -1 left. Reports whether the
// autopilot owned the nose (so the key is consumed).
func (w *World) NudgeLandingAim(along, across int, fine bool) bool {
	c := w.ActiveCraft()
	if c == nil || !c.Landing.Owns() {
		return false
	}
	lat, lon, _, ok := w.apTarget(c)
	if !ok {
		return true
	}
	csLat, csLon := w.computerSiteLatLon(c, lat, lon)
	subLat, subLon := render.WorldToBodyFixed(c.Primary, unitV(c.State.R), w.Clock.SimTime)
	step := siteShiftStep(HaversineM(subLat, subLon, csLat, csLon, c.Primary.RadiusMeters()), fine)
	c.Landing.AimAlongM += float64(along) * step
	c.Landing.AimAcrossM += float64(across) * step
	return true
}

// ClearLandingAim is | under Site+ auto: the aim offset goes back to the
// computer's site.
func (w *World) ClearLandingAim() {
	if c := w.ActiveCraft(); c != nil && c.Landing != nil {
		c.Landing.AimAlongM, c.Landing.AimAcrossM = 0, 0
	}
}
