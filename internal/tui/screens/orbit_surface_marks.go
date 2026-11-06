package screens

// Surface marks (ADR 0066 decision 6): flags and wrecks drawn on the body disk
// through the vessel-glyph path. One-line reason for a new file: the `y`
// filter and the one-cell-per-mark grouping are a slice of their own.

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"

	"github.com/jasonfen/terminal-space-program/internal/render"
	"github.com/jasonfen/terminal-space-program/internal/settings"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
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
	for _, s := range w.Sites {
		targeted := w.Target.Kind == sim.TargetSite && w.Target.SiteID == s.ID
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
		ref := InspectRef{Kind: InspectSite, SiteID: s.ID}
		if targeted {
			col = render.ColorTarget
		}
		col = v.inspectLineColor(ref, col)
		v.canvas.SetCellOverlayColored(p, glyph, col)
	}
	for _, c := range order {
		rep := pos[c]
		p, _, _ := w.SiteWorldPos(rep)
		name := rep.Name
		if n := len(groups[c]); n > 1 {
			name = fmt.Sprintf("%d sites · %s …", n, rep.Name)
		}
		v.addInspectable(InspectRef{Kind: InspectSite, SiteID: rep.ID}, name, p, true)
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
