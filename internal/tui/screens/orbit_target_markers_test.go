package screens

import (
	"math"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/render"
	"github.com/jasonfen/terminal-space-program/internal/settings"
	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// eccentricStateAtNu90 returns the (R, V) state of an eccentric orbit
// (periapsis rPeri, apoapsis rApo, gravitational parameter mu) at true
// anomaly 90°, given the orbit's periapsis direction pHat and its
// in-plane perpendicular qHat (= orbitNormal × pHat) — the standard
// perifocal parametrization R = r·Q̂, V = √(μ/p)·(−P̂ + e·Q̂) at ν=90°.
// Test-only helper so the two fixtures below don't hand-derive the same
// arithmetic twice.
func eccentricStateAtNu90(mu, rPeri, rApo float64, pHat, qHat orbital.Vec3) (r, v orbital.Vec3) {
	e := (rApo - rPeri) / (rApo + rPeri)
	p := rPeri * (1 + e) // semi-latus rectum
	vScale := math.Sqrt(mu / p)
	r = qHat.Scale(p)
	v = pHat.Scale(-vScale).Add(qHat.Scale(vScale * e))
	return r, v
}

// targetMarkerWorld builds a two-craft world (active idx0, target idx1)
// on distinct eccentric orbits around the same primary, each large
// enough to clear the minOrbitPixels zoom-skip. The two orbits differ in
// BOTH size and orientation (not just a 90° rotation of the same shape)
// so their Ap/Pe points and the ✕ closest-approach point don't land in
// the same rendered cell by geometric coincidence — mirrors
// TestApsisMarkersRenderAsUnifiedGlyphs' single-craft eccentric-orbit
// construction (orbit_markers_test.go), doubled for a craft target.
func targetMarkerWorld(t *testing.T) *sim.World {
	t.Helper()
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	active := w.ActiveCraft()
	active.Landed = false
	mu := active.Primary.GravitationalParameter()
	primaryR := active.Primary.RadiusMeters()

	// Active: line of apsides along ±X.
	active.State.R, active.State.V = eccentricStateAtNu90(
		mu, primaryR+400e3, primaryR+4000e3,
		orbital.Vec3{X: 1}, orbital.Vec3{Y: 1},
	)

	if _, err := w.SpawnCraft(sim.SpawnSpec{AltitudeM: 500e3}); err != nil {
		t.Fatalf("SpawnCraft: %v", err)
	}
	target := w.Crafts[1]
	target.Landed = false
	target.Primary = active.Primary
	// Target: a differently-sized eccentric orbit, line of apsides along
	// ±Y (rotated 90° from the active craft's — qHat = orbitNormal(+Z) ×
	// pHat(0,1,0) = (-1,0,0)).
	target.State.R, target.State.V = eccentricStateAtNu90(
		mu, primaryR+900e3, primaryR+7000e3,
		orbital.Vec3{Y: 1}, orbital.Vec3{X: -1},
	)

	w.ActiveCraftIdx = 0
	w.SetTargetCraft(1)
	return w
}

// TestTargetApsisMarkersDimmerThanOwnApsis is the deliverable's brightness
// requirement (#346 §1.2): the targeted craft's Ap/Pe render dimmed
// (MarkerCounterfactual) while the active craft's own Ap/Pe stay bright
// (MarkerNominal) — both present, distinctly colored, via the same
// ADR 0020 state-encodes-brightness convention the SOI Pass
// counterfactual arc already uses (not a new styling invention).
func TestTargetApsisMarkersDimmerThanOwnApsis(t *testing.T) {
	v := NewOrbitView(Theme{HUDBox: lipgloss.NewStyle()})
	v.Resize(200, 60)
	w := targetMarkerWorld(t)

	v.Render(w, 0, 200, 60)

	ownApo := render.MarkerColor(render.MarkerApoapsis, render.MarkerNominal, "")
	ownPeri := render.MarkerColor(render.MarkerPeriapsis, render.MarkerNominal, "")
	targetApo := render.MarkerColor(render.MarkerApoapsis, render.MarkerCounterfactual, "")
	targetPeri := render.MarkerColor(render.MarkerPeriapsis, render.MarkerCounterfactual, "")

	if ownApo == targetApo || ownPeri == targetPeri {
		t.Fatalf("nominal and counterfactual marker colors must differ: apo %v vs %v, peri %v vs %v",
			ownApo, targetApo, ownPeri, targetPeri)
	}
	if got := v.canvas.CountOverlayColor(ownApo); got == 0 {
		t.Error("own craft's apoapsis did not render in the bright/nominal color")
	}
	if got := v.canvas.CountOverlayColor(ownPeri); got == 0 {
		t.Error("own craft's periapsis did not render in the bright/nominal color")
	}
	if got := v.canvas.CountOverlayColor(targetApo); got == 0 {
		t.Error("target's apoapsis did not render in the dimmed/counterfactual color")
	}
	if got := v.canvas.CountOverlayColor(targetPeri); got == 0 {
		t.Error("target's periapsis did not render in the dimmed/counterfactual color")
	}
}

// closestApproachTestWorld builds a two-craft world (active idx0, target
// idx1) on different-altitude circular LEO orbits sharing the active
// craft's primary — the default spawn geometry the sim package's own
// rendezvousTwoCraftWorld fixture uses for closest-approach tests. Unlike
// targetMarkerWorld's eccentric pair (tuned for well-separated Ap/Pe
// cells), this fixture's two craft pass close enough over the 4h horizon
// that the ✕ marker's two ends land several canvas cells apart at typical
// zoom — verified to hold at both 200×60 and 80×24 canvases.
func closestApproachTestWorld(t *testing.T) *sim.World {
	t.Helper()
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	if _, err := w.SpawnCraft(sim.SpawnSpec{AltitudeM: 600e3}); err != nil {
		t.Fatalf("SpawnCraft: %v", err)
	}
	w.ActiveCraftIdx = 0
	w.SetTargetCraft(1)
	return w
}

// TestClosestApproachMarkerRendersOnBothOrbits is deliverable #346 §1.1:
// the ✕ glyph must appear at least twice — once on the active craft's
// own orbit, once as its twin on the target's — when a craft target
// shares the active craft's primary.
func TestClosestApproachMarkerRendersOnBothOrbits(t *testing.T) {
	v := NewOrbitView(Theme{HUDBox: lipgloss.NewStyle()})
	v.Resize(200, 60)
	w := closestApproachTestWorld(t)

	out := v.Render(w, 0, 200, 60)
	glyph := render.MarkerGlyph(render.MarkerClosestApproach)
	if got := strings.Count(out, string(glyph)); got < 2 {
		t.Errorf("expected the ✕ marker at both ends of the encounter (>=2 occurrences), got %d in:\n%s", got, out)
	}
}

// TestClosestApproachMarkerAbsentWithoutTarget: no bound target means no
// defined encounter, so the ✕ marker must not appear at all — it's not a
// property of the active craft's own orbit alone.
func TestClosestApproachMarkerAbsentWithoutTarget(t *testing.T) {
	v := NewOrbitView(Theme{HUDBox: lipgloss.NewStyle()})
	v.Resize(200, 60)
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}

	out := v.Render(w, 0, 200, 60)
	glyph := render.MarkerGlyph(render.MarkerClosestApproach)
	if strings.ContainsRune(out, glyph) {
		t.Errorf("✕ marker rendered with no target bound:\n%s", out)
	}
}

// TestClosestApproachMarkerAbsentCrossPrimary: a target on a different
// primary has no well-defined encounter in the active craft's frame
// (cross-SOI rendezvous is out of scope — CONTEXT.md's Rendezvous
// entry), so the ✕ marker must not appear.
func TestClosestApproachMarkerAbsentCrossPrimary(t *testing.T) {
	v := NewOrbitView(Theme{HUDBox: lipgloss.NewStyle()})
	v.Resize(200, 60)
	w := closestApproachTestWorld(t)
	sys := w.System()
	// Re-primary the target craft to any body that isn't the active
	// craft's primary.
	activePrimaryID := w.ActiveCraft().Primary.ID
	for _, b := range sys.Bodies {
		if b.ID != activePrimaryID {
			w.Crafts[1].Primary = b
			break
		}
	}

	out := v.Render(w, 0, 200, 60)
	glyph := render.MarkerGlyph(render.MarkerClosestApproach)
	if strings.ContainsRune(out, glyph) {
		t.Errorf("✕ marker rendered for a cross-primary target:\n%s", out)
	}
}

// tiltedPlaneWorld builds a two-craft world where the target orbits
// circularly in the primary's XY-plane and the active craft orbits the
// same circle tilted 30° about the +X axis — the line of nodes with the
// target's plane is exactly the X-axis (see the sim package's
// TestTargetPlaneNodePositions_TiltedAboutXAxis for the full derivation
// this mirrors at the screen level).
func tiltedPlaneWorld(t *testing.T) *sim.World {
	t.Helper()
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	active := w.ActiveCraft()
	active.Landed = false
	mu := active.Primary.GravitationalParameter()
	const r = 6.771e6
	baseV := math.Sqrt(mu / r)

	if _, err := w.SpawnCraft(sim.SpawnSpec{AltitudeM: r - active.Primary.RadiusMeters()}); err != nil {
		t.Fatalf("SpawnCraft: %v", err)
	}
	target := w.Crafts[1]
	target.Landed = false
	target.Primary = active.Primary
	target.State.R = orbital.Vec3{X: r}
	target.State.V = orbital.Vec3{Y: baseV}

	preR := orbital.Vec3{Y: r}
	preV := orbital.Vec3{X: -baseV}
	theta := 30 * math.Pi / 180
	axis := orbital.Vec3{X: 1}
	rotate := func(v orbital.Vec3) orbital.Vec3 {
		cos, sin := math.Cos(theta), math.Sin(theta)
		return v.Scale(cos).Add(axis.Cross(v).Scale(sin)).Add(axis.Scale(axis.Dot(v) * (1 - cos)))
	}
	active.State.R = rotate(preR)
	active.State.V = rotate(preV)

	w.ActiveCraftIdx = 0
	w.SetTargetCraft(1)
	return w
}

// TestTargetPlaneNodesRenderDiamonds is deliverable #346 §1.3: the ◇/◆
// Ascending/Descending Node markers against the TARGET's plane must
// appear on the map when the two orbits are inclined relative to each
// other.
//
// Asserted via CountOverlayColor rather than a raw glyph search: '◇' is
// also used as a plain-text bullet for unrelated session-event chip rows
// (orbit_chip_builders.go — "joined" / "left" / "docked with" etc.), so a
// bare strings.ContainsRune check can pass for the wrong reason. The
// marker's pinned color (render.ColorMarkerAscendingNode /
// ColorMarkerDescendingNode via SetCellOverlayColored) is unique to the
// canvas glyph.
func TestTargetPlaneNodesRenderDiamonds(t *testing.T) {
	v := NewOrbitView(Theme{HUDBox: lipgloss.NewStyle()})
	v.Resize(200, 60)
	w := tiltedPlaneWorld(t)

	v.Render(w, 0, 200, 60)
	anColor := render.MarkerColor(render.MarkerAscendingNode, render.MarkerNominal, "")
	dnColor := render.MarkerColor(render.MarkerDescendingNode, render.MarkerNominal, "")
	if got := v.canvas.CountOverlayColor(anColor); got == 0 {
		t.Error("ascending-node marker did not render")
	}
	if got := v.canvas.CountOverlayColor(dnColor); got == 0 {
		t.Error("descending-node marker did not render")
	}
}

// TestTargetPlaneNodesAbsentWhenCoplanar: two craft on the SAME orbital
// plane have no defined line of nodes, so neither ◇ nor ◆ should render.
func TestTargetPlaneNodesAbsentWhenCoplanar(t *testing.T) {
	v := NewOrbitView(Theme{HUDBox: lipgloss.NewStyle()})
	v.Resize(200, 60)
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	active := w.ActiveCraft()
	if _, err := w.SpawnCraft(sim.SpawnSpec{AltitudeM: 500e3}); err != nil {
		t.Fatalf("SpawnCraft: %v", err)
	}
	target := w.Crafts[1]
	// Same orbital plane as the active craft (its own h axis), phase-
	// shifted 90° — coplanar, not identical, so it's still a distinct
	// craft target.
	h := active.State.R.Cross(active.State.V).Unit()
	angle := math.Pi / 2
	cos, sin := math.Cos(angle), math.Sin(angle)
	rotate := func(v orbital.Vec3) orbital.Vec3 {
		return v.Scale(cos).Add(h.Cross(v).Scale(sin)).Add(h.Scale(h.Dot(v) * (1 - cos)))
	}
	target.State.R = rotate(active.State.R)
	target.State.V = rotate(active.State.V)
	target.Primary = active.Primary

	w.ActiveCraftIdx = 0
	w.SetTargetCraft(1)

	v.Render(w, 0, 200, 60)
	anColor := render.MarkerColor(render.MarkerAscendingNode, render.MarkerNominal, "")
	dnColor := render.MarkerColor(render.MarkerDescendingNode, render.MarkerNominal, "")
	if got := v.canvas.CountOverlayColor(anColor); got != 0 {
		t.Errorf("ascending-node marker rendered for a coplanar target (no defined line of nodes), count=%d", got)
	}
	if got := v.canvas.CountOverlayColor(dnColor); got != 0 {
		t.Errorf("descending-node marker rendered for a coplanar target (no defined line of nodes), count=%d", got)
	}
}

// TestClosestApproachMarkerRendersAtNarrowCanvas and
// TestApsisMarkersRenderDimmedAtNarrowCanvas exercise the new marker
// kinds (✕, target Ap/Pe) at 80×24 — the terminal-minimum size a past
// lesson (project_v030/rendezvous fix cluster) flagged for narrow-glyph
// rendering — rather than only the roomy 200×60 canvas the other tests in
// this file use, so a cramped canvas / stride skip doesn't silently drop
// a marker kind at the sizes players actually run in.
func TestClosestApproachMarkerRendersAtNarrowCanvas(t *testing.T) {
	v := NewOrbitView(Theme{HUDBox: lipgloss.NewStyle()})
	v.Resize(80, 24)
	w := closestApproachTestWorld(t)
	// ADR 0046 (#422): VESSEL now has a Compact Form, so at 80×24 there is
	// enough shared left-side budget for MISSION and ATTITUDE to both
	// render in full where before ATTITUDE alone was silently dropped for
	// space — a Graceful Shrink improvement, but ATTITUDE's box happens to
	// land where this fixture's ✕ marker draws. This test is about marker
	// glyph rendering at a narrow canvas, not chip crowding, so turn the
	// unrelated ATTITUDE chip off to isolate what it actually checks.
	s := settings.Default()
	s.SetChip(settings.ChipAttitude, false)
	v.SetSettings(s)

	out := v.Render(w, 0, 80, 24)
	caGlyph := render.MarkerGlyph(render.MarkerClosestApproach)
	if strings.Count(out, string(caGlyph)) < 2 {
		t.Errorf("expected both ✕ marker ends at 80x24, got render:\n%s", out)
	}
}

func TestApsisMarkersRenderDimmedAtNarrowCanvas(t *testing.T) {
	v := NewOrbitView(Theme{HUDBox: lipgloss.NewStyle()})
	v.Resize(80, 24)
	w := targetMarkerWorld(t)

	v.Render(w, 0, 80, 24)
	targetApo := render.MarkerColor(render.MarkerApoapsis, render.MarkerCounterfactual, "")
	if got := v.canvas.CountOverlayColor(targetApo); got == 0 {
		t.Error("target apoapsis did not render dimmed at 80x24")
	}
}

// TestTargetPlaneNodesRenderAtNarrowCanvas mirrors the above for the
// ◇/◆ node markers, which need a different (tilted-plane) world fixture.
func TestTargetPlaneNodesRenderAtNarrowCanvas(t *testing.T) {
	v := NewOrbitView(Theme{HUDBox: lipgloss.NewStyle()})
	v.Resize(80, 24)
	w := tiltedPlaneWorld(t)

	v.Render(w, 0, 80, 24)
	anColor := render.MarkerColor(render.MarkerAscendingNode, render.MarkerNominal, "")
	dnColor := render.MarkerColor(render.MarkerDescendingNode, render.MarkerNominal, "")
	if got := v.canvas.CountOverlayColor(anColor); got == 0 {
		t.Error("ascending-node marker missing at 80x24")
	}
	if got := v.canvas.CountOverlayColor(dnColor); got == 0 {
		t.Error("descending-node marker missing at 80x24")
	}
}
