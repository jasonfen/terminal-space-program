package screens

// Surface marks (ADR 0066 decision 6): flags and wrecks drawn on the body disk
// through the vessel-glyph path. One-line reason for a new file: the `y`
// filter and the one-cell-per-mark grouping are a slice of their own.

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/render"
	"github.com/jasonfen/terminal-space-program/internal/settings"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
	"github.com/jasonfen/terminal-space-program/internal/tui/widgets"
)

const (
	flagGlyph  = '⚑'
	wreckGlyph = '☠'
)

// markShown says whether the `y` card lets a kind of mark draw. A targeted
// mark draws alone whatever the card says (the card filters the map, never
// the target).
func (v *OrbitView) markShown(c settings.Chip, targeted bool) bool {
	return targeted || v.settings.ChipEnabled(c)
}

// siteTargeted matches the aimed site on owner and id: ids are per player.
func (v *OrbitView) siteTargeted(w *sim.World, s sim.Site) bool {
	return w.Target.Kind == sim.TargetSite && w.Target.SiteID == s.ID && w.Target.GhostOwner == s.Owner
}

// siteChip is the card row that governs a site: partners' marks first, then
// the kind.
func siteChip(s sim.Site) settings.Chip {
	switch {
	case s.Owner != "":
		return settings.ChipMarksPartners
	case s.Kind == sim.SiteWreck:
		return settings.ChipMarksWrecks
	}
	return settings.ChipMarksFlags
}

// drawSurfaceMarks draws every site of the current system that the card lets
// through, before any vessel so a vessel on the same cell wins it. Marks
// behind the limb are culled like a vessel's. Marks sharing a cell become one
// Inspect stop named `3 sites · first …`.
func (v *OrbitView) drawSurfaceMarks(w *sim.World, scale float64, canvasReach int) {
	type cell struct{ x, y int }
	groups := map[cell][]sim.Site{}
	var order []cell
	pos := map[cell]sim.Site{} // representative: the targeted one, else the first
	all := append([]sim.Site(nil), w.Sites...)
	if sim.LZCirclesOnMap(scale) {
		// A shipped zone has no glyph of its own (it would cover a small
		// body's disc); it appears only as its circles, once they fit.
		all = append(all, w.ShippedZones()...)
	}
	for _, p := range w.PartnerSites {
		all = append(all, p.Site)
	}
	for _, s := range all {
		targeted := v.siteTargeted(w, s)
		if !v.markShown(siteChip(s), targeted) {
			continue
		}
		p, body, ok := w.SiteWorldPos(s)
		if !ok {
			continue
		}
		if v.canvas.IsBehindBody(p, w.BodyPosition(body), mapBodyPixelRadius(body, false, scale, canvasReach)) {
			continue
		}
		px, py, on := v.canvas.Project(p)
		if !on {
			continue
		}
		c := cell{px / 2, py / 4}
		if _, seen := groups[c]; !seen {
			order = append(order, c)
			pos[c] = s
		}
		groups[c] = append(groups[c], s)
		if targeted {
			pos[c] = s
		}
		col := lipgloss.Color(render.ColorDim)
		glyph := wreckGlyph
		if s.Kind != sim.SiteWreck {
			glyph = flagGlyph
			if s.Owner == "" {
				if cr, _, found := w.CraftByID(s.CraftID); found && cr != nil && cr.Color != "" {
					col = lipgloss.Color(cr.Color)
				}
			}
		}
		ref := InspectRef{Kind: InspectSite, SiteID: s.ID, Owner: s.Owner}
		if targeted {
			col = render.ColorTarget
		}
		col = v.inspectLineColor(ref, col)
		if s.Kind != sim.SiteWreck && sim.LZCirclesOnMap(scale) {
			v.drawLZCircles(w, s, targeted, col)
			continue
		}
		v.canvas.SetCellOverlayColored(p, glyph, col)
	}
	for _, c := range order {
		rep := pos[c]
		p, _, _ := w.SiteWorldPos(rep)
		name := rep.Name
		if n := len(groups[c]); n > 1 {
			name = fmt.Sprintf("%d sites · %s …", n, rep.Name)
		}
		v.addInspectable(InspectRef{Kind: InspectSite, SiteID: rep.ID, Owner: rep.Owner}, name, p, true)
	}
}

// drawLZCircles draws a site's LZs as circles lying on the ground, in the
// colour of the site's glyph, a digit at each centre and the flag at the one
// you fly to (ADR 0068 decision 8). Called from the marks loop, so the `y`
// card that hides the site's glyph hides these too.
func (v *OrbitView) drawLZCircles(w *sim.World, s sim.Site, targeted bool, col lipgloss.Color) {
	flown := 0
	if targeted {
		if _, n, ok := w.ActiveLandingLZ(); ok {
			flown = n
		}
	}
	for _, p := range w.LZPlaces(s) {
		e1 := p.Up.Cross(orbital.Vec3{Z: 1})
		if e1.Norm() < 1e-6 { // at a pole
			e1 = p.Up.Cross(orbital.Vec3{X: 1})
		}
		e1 = e1.Unit()
		e2 := p.Up.Cross(e1)
		v.canvas.PlotPolylineClass(proximityRingPoints(p.Pos, e1, e2, sim.SiteLZRadiusM), col, widgets.ClassReal)
		glyph := rune('0' + p.N)
		if p.N == flown {
			glyph = flagGlyph
		}
		v.canvas.SetCellOverlayColored(p.Pos, glyph, col)
	}
}

// landedGhostHidden says a landed partner vessel is filtered off by the card
// or sits behind its body's limb.
func (v *OrbitView) landedGhostHidden(w *sim.World, g sim.Ghost, scale float64, canvasReach int) bool {
	if !v.settings.ChipEnabled(settings.ChipMarksLanded) || !v.settings.ChipEnabled(settings.ChipMarksPartners) {
		return true
	}
	for _, b := range w.System().Bodies {
		if b.ID == g.PrimaryID {
			return v.canvas.IsBehindBody(g.Pos, w.BodyPosition(b), mapBodyPixelRadius(b, false, scale, canvasReach))
		}
	}
	return false
}

// surfaceVesselHidden says a non-active vessel on the ground is filtered off
// by the card (landed vessels, or wrecks for a crashed one) and is not the
// target.
func (v *OrbitView) surfaceVesselHidden(w *sim.World, idx int, c *spacecraft.Spacecraft) bool {
	if !c.Landed && !c.Crashed {
		return false
	}
	if t, i, ok := w.ResolveTargetCraft(); ok && i == idx && t == c {
		return false
	}
	chip := settings.ChipMarksLanded
	if c.Crashed {
		chip = settings.ChipMarksWrecks
	}
	return !v.settings.ChipEnabled(chip)
}
