package screens

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/render"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// setupHyperbolicInMoonSOI parks the active craft INSIDE the Moon's SOI on
// an inbound escape hyperbola (e = 2, periapsis 500 km above the surface,
// currently at 0.6× the SOI radius) — the post-entry, pre-capture state of
// issue #157, where the orbit line and SOI circle used to vanish. Mirrors
// the sim package's hyperbolicInMoonSOI, replicated here because that
// helper isn't visible across packages.
func setupHyperbolicInMoonSOI(t *testing.T, w *sim.World) (moonIdx int, moon bodies.CelestialBody, soi float64) {
	t.Helper()
	sys := w.System()
	moonIdx = -1
	for i, b := range sys.Bodies {
		if b.EnglishName == "Moon" {
			moonIdx = i
		}
	}
	if moonIdx < 0 {
		t.Skip("Moon not in loaded Sol system")
	}
	moon = sys.Bodies[moonIdx]
	soi = w.BodySOIRadius(moon)
	mu := moon.GravitationalParameter()
	rp := moon.RadiusMeters() + 500e3
	r0 := 0.6 * soi
	v0 := math.Sqrt(mu * (2/r0 + 1/rp)) // vis-viva with a = −rp (e = 2)
	vt := math.Sqrt(3*mu*rp) / r0       // h = √(3·μ·rp)
	vr := -math.Sqrt(v0*v0 - vt*vt)     // inbound: perilune still ahead

	c := w.ActiveCraft()
	c.Primary = moon
	c.Landed = false
	c.Nodes = nil
	c.State.R = orbital.Vec3{X: r0}
	c.State.V = orbital.Vec3{X: vr, Y: vt}
	return moonIdx, moon, soi
}

// TestInSOIRingAndPathPersist is the render-level acceptance for #157's
// no-node case: inside the Moon's SOI on an escape trajectory, focused on
// the Moon, the canvas still carries the dim dotted SOI Ring, the bright
// in-SOI arc (plus its onward continuation), and the SOI PASS chip — the
// whole encounter picture that used to switch off at SOI entry.
func TestInSOIRingAndPathPersist(t *testing.T) {
	v := newSOIPassTestView()
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	moonIdx, moon, soi := setupHyperbolicInMoonSOI(t, w)
	w.ViewMode = sim.ViewTilted
	w.Focus = sim.Focus{Kind: sim.FocusBody, BodyIdx: moonIdx}
	out := v.Render(w, 0, 200, 60)

	// SOI Ring ink, in the ring's unique shade (same gauge as
	// TestSOIRingDrawsAtPassBody).
	pxR := soi * v.canvas.Scale()
	if pxR < float64(soiRingMinPixels) {
		t.Fatalf("test setup: ring projects to only %.1f px — framing too wide to assert the ring", pxR)
	}
	ideal := 2 * math.Pi * pxR / 4
	if got := v.canvas.CountColor(render.Shade(render.ColorForeignSOI, soiRingDim)); float64(got) < ideal/4 {
		t.Errorf("only %d ring-coloured cells inside the SOI, want ≥ %.0f — the SOI Ring vanished at entry (#157)", got, ideal/4)
	}

	// The predicted path: with no node planted the residence arc draws
	// bright, in the undimmed foreign-SOI colour no other element uses.
	if got := v.canvas.CountColor(render.ColorForeignSOI); got < 15 {
		t.Errorf("only %d bright arc cells inside the SOI, want ≥ 15 — the escape path isn't drawn", got)
	}

	// The chip keeps reporting the pass.
	if !strings.Contains(out, "SOI PASS") {
		t.Errorf("SOI PASS chip absent inside the SOI on an escape trajectory")
	}

	// Camera Contract sanity: the Framing-Event fit covers the body the
	// craft is inside — centered on the Moon at SOI scale.
	if d := v.canvas.CenterWorld().Sub(w.BodyPosition(moon)).Norm(); d > 0.5*soi {
		t.Errorf("canvas centered %.2f×SOI from the Moon — focus fit not covering the in-SOI pass", d/soi)
	}
}

// TestInSOIExitAndPeriluneMarkersDraw: the residence pass keeps the Exit ◁
// and Perilune glyphs on the in-SOI leg (entry is in the past, so the pass
// carries no Entry crossing). Positions asserted through the same Project
// mapping the renderer uses. Declutter hides the chips + navball so corner
// overlays can't shadow a marker cell.
func TestInSOIExitAndPeriluneMarkersDraw(t *testing.T) {
	v := newSOIPassTestView()
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	moonIdx, _, _ := setupHyperbolicInMoonSOI(t, w)
	w.ViewMode = sim.ViewTilted
	w.Focus = sim.Focus{Kind: sim.FocusBody, BodyIdx: moonIdx}
	v.declutter = true
	out := v.Render(w, 0, 200, 60)

	pass, ok := w.LiveSOIPass()
	if !ok {
		t.Fatal("precondition: no residence pass inside the SOI")
	}
	if pass.HasEntry {
		t.Error("residence pass reports an Entry crossing — entry is in the past")
	}
	if !pass.HasExit || !pass.HasPerilunePt {
		t.Fatalf("precondition: pass missing exit/perilune (exit=%v perilune=%v)", pass.HasExit, pass.HasPerilunePt)
	}

	if got, want := glyphAtProjected(t, v, out, w.ExitPosition(pass)), render.MarkerGlyph(render.MarkerSOIExit); got != want {
		t.Errorf("cell at the SOI-exit crossing shows %q, want the Exit glyph %q", got, want)
	}
	if got, want := glyphAtProjected(t, v, out, w.PerilunePosition(pass)), render.MarkerGlyph(render.MarkerPerilune); got != want {
		t.Errorf("cell at the perilune shows %q, want the Perilune glyph %q", got, want)
	}
}

// TestInSOIQuietLunarOrbitNoRing pins the quiet case at render level: a
// captured (bound, apoapsis inside the SOI) low lunar orbit draws no ring
// ink and no SOI PASS chip — parked orbits look exactly as they did before
// the in-SOI continuation. The Earth-LEO twin of this regression guard is
// TestSOIRingAbsentForQuietBodies.
func TestInSOIQuietLunarOrbitNoRing(t *testing.T) {
	v := newSOIPassTestView()
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	sys := w.System()
	moonIdx := -1
	for i, b := range sys.Bodies {
		if b.EnglishName == "Moon" {
			moonIdx = i
		}
	}
	if moonIdx < 0 {
		t.Skip("Moon not in loaded Sol system")
	}
	moon := sys.Bodies[moonIdx]
	c := w.ActiveCraft()
	c.Primary = moon
	c.Landed = false
	c.Nodes = nil
	mu := moon.GravitationalParameter()
	r := moon.RadiusMeters() + 200e3
	c.State.R = orbital.Vec3{X: r}
	c.State.V = orbital.Vec3{Y: math.Sqrt(mu / r)}
	w.ViewMode = sim.ViewTilted
	w.Focus = sim.Focus{Kind: sim.FocusBody, BodyIdx: moonIdx}

	out := v.Render(w, 0, 200, 60)
	if got := v.canvas.CountColor(render.Shade(render.ColorForeignSOI, soiRingDim)); got != 0 {
		t.Errorf("%d ring-coloured cells on a captured low lunar orbit, want 0 — the quiet case regressed", got)
	}
	if strings.Contains(out, "SOI PASS") {
		t.Errorf("captured low lunar orbit must not show a SOI PASS chip")
	}
}

// TestInSOIRingPersistsWithCaptureNode covers #157's planted-node case: a
// capture node inside the SOI keeps the ring drawing and demotes the
// residence arc to the dim node-capped counterfactual, while the planned
// overlay keeps rendering through the unchanged drawNodes path — pinned
// here by the Δ node marker (anchored at the Body's current position via
// NodeInertialPosition) and at the sim level by
// TestInSOICounterfactualCappedAtNode's draw-identity check on the legs.
func TestInSOIRingPersistsWithCaptureNode(t *testing.T) {
	v := newSOIPassTestView()
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	moonIdx, _, soi := setupHyperbolicInMoonSOI(t, w)

	live, ok := w.LiveSOIPass()
	if !ok || live.TimeToPerilune <= 0 {
		t.Fatalf("precondition: live residence pass with perilune ahead (ok=%v tca=%.0f)", ok, live.TimeToPerilune)
	}
	// A retrograde burn on the outbound leg (1.5× TCA — past perilune,
	// still well inside the SOI: the e=2 transit exits at ~2.3× TCA). The
	// capped counterfactual then spans the true perilune, and the Δ node
	// glyph sits in a different canvas cell than the ⊕ Perilune marker —
	// a node planted *before* perilune makes the capped arc's closest
	// approach the cap point itself, stacking both glyphs in one cell.
	c := w.ActiveCraft()
	c.Nodes = []spacecraft.ManeuverNode{{
		TriggerTime: w.Clock.SimTime.Add(time.Duration(live.TimeToPerilune * 1.5 * float64(time.Second))),
		Mode:        spacecraft.BurnRetrograde,
		DV:          600,
	}}

	w.ViewMode = sim.ViewTilted
	w.Focus = sim.Focus{Kind: sim.FocusBody, BodyIdx: moonIdx}
	v.declutter = true
	out := v.Render(w, 0, 200, 60)

	pxR := soi * v.canvas.Scale()
	if pxR < float64(soiRingMinPixels) {
		t.Fatalf("test setup: ring projects to only %.1f px", pxR)
	}
	ideal := 2 * math.Pi * pxR / 4
	if got := v.canvas.CountColor(render.Shade(render.ColorForeignSOI, soiRingDim)); float64(got) < ideal/4 {
		t.Errorf("only %d ring-coloured cells with a capture node planted, want ≥ %.0f — ring must persist through capture planning", got, ideal/4)
	}
	// The no-burn residence arc is now the dim counterfactual…
	if got := v.canvas.CountColor(render.Shade(render.ColorForeignSOI, soiCounterfactualDim)); got < 5 {
		t.Errorf("only %d dim counterfactual cells with a node planted, want ≥ 5", got)
	}
	// …and no longer paints in the bright no-node colour.
	if got := v.canvas.CountColor(render.ColorForeignSOI); got != 0 {
		t.Errorf("%d bright arc cells with a node planted, want 0 (brightness = state, ADR 0020 B)", got)
	}
	// The planned overlay still draws: the Δ node marker rides the in-SOI
	// trajectory at the burn point.
	if got, want := glyphAtProjected(t, v, out, w.NodeInertialPosition(c.Nodes[0])), render.MarkerGlyph(render.MarkerManeuver); got != want {
		t.Errorf("cell at the planted node shows %q, want the maneuver glyph %q", got, want)
	}
}
