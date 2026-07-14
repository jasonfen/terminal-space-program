package screens

import (
	"math"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// --- test helpers (v0.28 S2) -------------------------------------------------
//
// The headless test renderer emits no ANSI (lipgloss sees no TTY), so ghost
// vs own-orbit ink is told apart by geometry, not color: braille-cell counts,
// cell-set containment, sampling density, and on-grid position.

// brailleCells returns the [col,row] grid positions of every non-blank braille
// glyph (U+2801–U+28FF) in a rendered frame — the drawn orbit/trajectory ink.
func brailleCells(s string) map[[2]int]bool {
	cells := map[[2]int]bool{}
	s = stripANSI(s)
	row, col := 0, 0
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == '\n' {
			row++
			col = 0
		} else {
			if r > 0x2800 && r <= 0x28FF {
				cells[[2]int{col, row}] = true
			}
			col++
		}
		i += size
	}
	return cells
}

func ghostTestTheme() Theme {
	return Theme{
		Primary: lipgloss.NewStyle(),
		Warning: lipgloss.NewStyle(),
		Alert:   lipgloss.NewStyle(),
		Dim:     lipgloss.NewStyle(),
		HUDBox:  lipgloss.NewStyle().Border(lipgloss.RoundedBorder()),
		Footer:  lipgloss.NewStyle(),
		Title:   lipgloss.NewStyle(),
	}
}

// leoWorld returns a world whose active craft sits in a clean equatorial
// circular LEO around its default primary, so the own-craft orbit renders and
// the camera fits to it — the frame ghosts are placed into.
func leoWorld(t *testing.T) (*sim.World, orbital.Vec3, float64) {
	t.Helper()
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()
	c.Landed = false
	mu := c.Primary.GravitationalParameter()
	r := c.Primary.RadiusMeters() + 300e3
	c.State.R = orbital.Vec3{X: r}
	c.State.V = orbital.Vec3{Y: math.Sqrt(mu / r)}
	c.State.M = c.TotalMass()
	return w, c.State.R, mu
}

// circularGhost builds a ghost on a circular orbit of the given radius around
// the active craft's primary (RelPos/Vel are primary-relative, as the wire
// delivers them; Pos is the world-frame marker position).
func circularGhost(w *sim.World, radius float64) sim.Ghost {
	c := w.ActiveCraft()
	mu := c.Primary.GravitationalParameter()
	rel := orbital.Vec3{X: radius}
	vel := orbital.Vec3{Y: math.Sqrt(mu / radius)}
	return sim.Ghost{
		Owner: "SHA256:guest", CraftID: 42, Handle: "gern", Name: "gern's ship",
		Glyph: "◆", PrimaryID: c.Primary.ID,
		Pos: w.BodyPosition(c.Primary).Add(rel), RelPos: rel, Vel: vel,
	}
}

// --- acceptance tests --------------------------------------------------------

// A bound ghost draws its orbit as an ellipse: braille ink jumps well past the
// marker-only baseline once the ghost is present.
func TestGhostBoundOrbitRendersEllipse(t *testing.T) {
	v := NewOrbitView(ghostTestTheme())
	v.Resize(200, 60)

	w, ownR, _ := leoWorld(t)
	base := countBraille(v.Render(w, 0, 200, 60))

	// A larger circular orbit than the active craft, so its ellipse ink does
	// not fall entirely on the own-craft track.
	w.Ghosts = []sim.Ghost{circularGhost(w, ownR.Norm()*1.6)}
	got := countBraille(v.Render(w, 0, 200, 60))

	if got-base < 20 {
		t.Errorf("braille only rose %d (%d→%d) — ghost orbit ellipse not drawn", got-base, base, got)
	}
}

// Golden check: adding a ghost must not disturb the own-craft orbit ink
// (ADR 0021 camera contract + display-only ghosts). Every braille cell drawn
// without a ghost is still drawn with one present — own ink is preserved, only
// ghost ink is added.
func TestGhostOrbitLeavesOwnOrbitUntouched(t *testing.T) {
	v := NewOrbitView(ghostTestTheme())
	v.Resize(200, 60)

	w, ownR, _ := leoWorld(t)
	before := brailleCells(v.Render(w, 0, 200, 60))
	if len(before) < 10 {
		t.Fatalf("own-craft orbit barely rendered (%d cells) — bad baseline", len(before))
	}

	w.Ghosts = []sim.Ghost{circularGhost(w, ownR.Norm()*1.6)}
	after := brailleCells(v.Render(w, 0, 200, 60))

	for cell := range before {
		if !after[cell] {
			t.Fatalf("own orbit cell %v erased by a ghost present", cell)
		}
	}
	if len(after) <= len(before) {
		t.Fatalf("ghost added no ink: %d→%d cells", len(before), len(after))
	}
}

// A hyperbolic ghost (a < 0) stays glyph-only: no ellipse ink, but the marker
// glyph and handle still render. Conic-arc path is out of scope for v0.28.
func TestHyperbolicGhostGlyphOnly(t *testing.T) {
	v := NewOrbitView(ghostTestTheme())
	v.Resize(200, 60)

	w, _, mu := leoWorld(t)
	base := countBraille(v.Render(w, 0, 200, 60))

	c := w.ActiveCraft()
	rel := orbital.Vec3{X: c.Primary.RadiusMeters() + 300e3}
	// Well above escape speed → e > 1, a < 0.
	vel := orbital.Vec3{Y: 2.0 * math.Sqrt(mu/rel.Norm())}
	w.Ghosts = []sim.Ghost{{
		Owner: "SHA256:guest", CraftID: 7, Handle: "hypr", Glyph: "◆",
		PrimaryID: c.Primary.ID, Pos: w.BodyPosition(c.Primary).Add(rel),
		RelPos: rel, Vel: vel,
	}}
	out := v.Render(w, 0, 200, 60)

	if got := countBraille(out); got-base > 6 {
		t.Errorf("braille rose %d for a hyperbolic ghost — an ellipse was drawn (should be glyph-only)", got-base)
	}
	if !strings.Contains(out, "◆") {
		t.Error("hyperbolic ghost glyph not rendered")
	}
	if !strings.Contains(out, "hypr") {
		t.Error("hyperbolic ghost handle not rendered")
	}
}

// A ghost whose orbit projects below minOrbitPixels is gated exactly like the
// own craft's: marker only, no ellipse ink. The same ghost scaled up does draw
// one, proving the gate (not occlusion or some other cause) is what suppresses
// it. Needs a zoomed-out frame — at a body's own framing every above-surface
// orbit clears the 6px gate — so the active craft rides a very large orbit to
// pull the scale out.
func TestSubPixelGhostOrbitGated(t *testing.T) {
	v := NewOrbitView(ghostTestTheme())
	v.Resize(200, 60)

	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()
	c.Landed = false
	mu := c.Primary.GravitationalParameter()
	bodyR := c.Primary.RadiusMeters()
	rOwn := 7e9 // far beyond any moon → autofit pulls the scale way out
	c.State.R = orbital.Vec3{X: rOwn}
	c.State.V = orbital.Vec3{Y: math.Sqrt(mu / rOwn)}
	c.State.M = c.TotalMass()

	base := countBraille(v.Render(w, 0, 200, 60))
	scale := v.canvas.Scale()
	if scale <= 0 {
		t.Fatal("non-positive canvas scale")
	}
	gateR := minOrbitPixels / scale // radius at which apoapsis*scale == the gate
	if gateR < 4*bodyR {
		t.Skipf("frame not zoomed out enough (gate radius %.2g < 4×body %.2g)", gateR, 4*bodyR)
	}

	// Above the primary's surface (so not occluded) yet well below the pixel
	// gate → no ellipse.
	tinyR := 2 * bodyR // apoapsis*scale < gate/2 < 6 px
	w.Ghosts = []sim.Ghost{circularGhost(w, tinyR)}
	gated := countBraille(v.Render(w, 0, 200, 60))
	if gated-base > 6 {
		t.Errorf("sub-pixel ghost orbit drew %d cells past baseline — gate not applied", gated-base)
	}

	// Same ghost, orbit comfortably above the gate → ellipse appears.
	bigR := 4 * gateR // apoapsis*scale ≈ 24 px
	w.Ghosts = []sim.Ghost{circularGhost(w, bigR)}
	shown := countBraille(v.Render(w, 0, 200, 60))
	if shown-base < 20 {
		t.Errorf("above-gate ghost orbit drew only %d cells past baseline — gate mis-firing", shown-base)
	}
}

// Targeting a ghost promotes its ellipse to the TARGET treatment: denser
// sampling (stride 3 vs 5), mirroring a targeted local craft. With no color in
// the headless frame, the promotion is observed as more ellipse ink.
func TestTargetedGhostOrbitPromoted(t *testing.T) {
	v := NewOrbitView(ghostTestTheme())
	v.Resize(200, 60)

	w, ownR, _ := leoWorld(t)
	base := countBraille(v.Render(w, 0, 200, 60))
	g := circularGhost(w, ownR.Norm()*1.6)
	w.Ghosts = []sim.Ghost{g}

	untarg := countBraille(v.Render(w, 0, 200, 60))
	if untarg-base < 20 {
		t.Fatalf("untargeted ghost drew only %d cells past baseline — no ellipse to promote", untarg-base)
	}

	w.SetTargetGhost(g.Owner, g.CraftID)
	targ := countBraille(v.Render(w, 0, 200, 60))
	if targ <= untarg {
		t.Errorf("targeting a ghost did not densify its ellipse (%d→%d) — not promoted", untarg, targ)
	}
}

// Other-primary offset: a ghost orbiting a moon draws its ellipse centered on
// THAT body's on-screen position, not the active craft's primary. Focused on
// the moon (own craft off-frame around its own primary), the added braille ink
// clusters on the moon's projected cell.
func TestGhostOrbitOffsetToOwnPrimary(t *testing.T) {
	v := NewOrbitView(ghostTestTheme())
	v.Resize(200, 60)

	w, _, _ := leoWorld(t)
	c := w.ActiveCraft()

	// A moon of the active craft's primary.
	sys := w.System()
	moonIdx := -1
	for i := range sys.Bodies {
		if sys.Bodies[i].ParentID == c.Primary.ID {
			moonIdx = i
			break
		}
	}
	if moonIdx < 0 {
		t.Skip("no moon of the active primary in the default system")
	}
	moon := sys.Bodies[moonIdx]
	muMoon := moon.GravitationalParameter()
	rMoon := moon.RadiusMeters() * 3.0

	// Frame the moon (KSP model: focus the body to read its neighborhood).
	w.Focus = sim.Focus{Kind: sim.FocusBody, BodyIdx: moonIdx}
	baseCells := brailleCells(v.Render(w, 0, 200, 60))

	rel := orbital.Vec3{X: rMoon}
	g := sim.Ghost{
		Owner: "SHA256:guest", CraftID: 99, Handle: "luna", Glyph: "◆",
		PrimaryID: moon.ID, Pos: w.BodyPosition(moon).Add(rel),
		RelPos: rel, Vel: orbital.Vec3{Y: math.Sqrt(muMoon / rMoon)},
	}
	w.Ghosts = []sim.Ghost{g}
	out := v.Render(w, 0, 200, 60)

	// Own craft (LEO around its primary, a moon-distance away) must be off the
	// moon-scale frame, so the added ink is unambiguously the ghost's.
	if _, _, ok := v.canvas.Project(w.CraftInertial()); ok {
		t.Fatal("own craft projects onto the moon frame — offset test not isolated")
	}

	// Isolate the ghost's ellipse: cells present with the ghost but not in the
	// static (moon-disk + background) baseline.
	var added [][2]int
	for cell := range brailleCells(out) {
		if !baseCells[cell] {
			added = append(added, cell)
		}
	}
	if len(added) < 20 {
		t.Fatalf("moon ghost added only %d cells — no ellipse to locate", len(added))
	}

	mpx, mpy, ok := v.canvas.Project(w.BodyPosition(moon))
	if !ok {
		t.Fatal("moon projects off-canvas — cannot check offset")
	}
	moonCol, moonRow := mpx/2, mpy/4

	sumC, sumR := 0, 0
	for _, cell := range added {
		sumC += cell[0]
		sumR += cell[1]
	}
	cCol, cRow := sumC/len(added), sumR/len(added)
	if dc, dr := abs(cCol-moonCol), abs(cRow-moonRow); dc > 10 || dr > 8 {
		t.Errorf("ellipse ink centroid (%d,%d) not centered on moon cell (%d,%d) — not offset onto the ghost's primary", cCol, cRow, moonCol, moonRow)
	}
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
