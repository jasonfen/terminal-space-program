package screens

import (
	"math"
	"strings"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/render"
	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// The ring assertions key on the exact cell colour drawSOIRing paints —
// render.Shade(ColorForeignSOI, soiRingDim) — which is unique on the canvas
// (the counterfactual arc dims by soiCounterfactualDim=0.5, the ring by
// 0.4), so CountColor isolates the ring's ink from the arcs that cross it.

// TestSOIRingDrawsAtPassBody: coasting toward the Moon with a live pass,
// focused on the Moon, the canvas carries the dim dotted SOI Ring — a
// substantial count of cells in the ring's unique colour, scaling with the
// ring's projected circumference (ADR 0021 C).
func TestSOIRingDrawsAtPassBody(t *testing.T) {
	v := newSOIPassTestView()
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	moonIdx := setupMoonCoast(t, w)
	w.ViewMode = sim.ViewTilted
	w.Focus = sim.Focus{Kind: sim.FocusBody, BodyIdx: moonIdx}
	v.Render(w, 0, 200, 60)

	moon := w.System().Bodies[moonIdx]
	soi := w.BodySOIRadius(moon)
	pxR := soi * v.canvas.Scale()
	if pxR < float64(soiRingMinPixels) {
		t.Fatalf("test setup: ring projects to only %.1f px — framing too wide to assert the ring", pxR)
	}

	got := v.canvas.CountColor(render.Shade(render.ColorForeignSOI, soiRingDim))
	// One dot per ~ringDotSpacing(4) px of circumference; on-canvas cells
	// can collapse dots, and part of the ring may project off-canvas, so
	// demand a conservative fraction of the ideal count.
	ideal := 2 * math.Pi * pxR / 4
	if float64(got) < ideal/4 {
		t.Errorf("only %d ring-coloured cells on the canvas, want ≥ %.0f (ring radius %.0f px) — SOI Ring not drawn", got, ideal/4, pxR)
	}
}

// TestSOIRingAbsentForQuietBodies: a stable LEO reaches no SOI, so no body
// has an active pass and no ring ink appears anywhere on the canvas —
// quiet bodies draw no ring (ADR 0021 C).
func TestSOIRingAbsentForQuietBodies(t *testing.T) {
	v := newSOIPassTestView()
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()
	c.Landed = false
	c.Nodes = nil
	mu := c.Primary.GravitationalParameter()
	r := c.Primary.RadiusMeters() + 300e3
	c.State.R = orbital.Vec3{X: r}
	c.State.V = orbital.Vec3{Y: math.Sqrt(mu / r)}

	v.Render(w, 0, 200, 60)
	if got := v.canvas.CountColor(render.Shade(render.ColorForeignSOI, soiRingDim)); got != 0 {
		t.Errorf("%d ring-coloured cells on a stable LEO with no pass, want 0", got)
	}
}

// glyphAtProjected returns the rune the rendered frame shows at the cell a
// world position projects to. The canvas content sits 1 col in and 2 rows
// down from the frame's top-left (title row + rounded border), the same
// offsets the mouse dispatch uses (IsCanvasClick / composeChips).
func glyphAtProjected(t *testing.T, v *OrbitView, out string, pos orbital.Vec3) rune {
	t.Helper()
	px, py, ok := v.canvas.Project(pos)
	if !ok {
		t.Fatalf("position %v projects off-canvas", pos)
	}
	col, row := px/2, py/4
	lines := strings.Split(stripANSI(out), "\n")
	frameRow := row + 2
	frameCol := col + 1
	if frameRow >= len(lines) {
		t.Fatalf("frame row %d beyond rendered %d lines", frameRow, len(lines))
	}
	runes := []rune(lines[frameRow])
	if frameCol >= len(runes) {
		t.Fatalf("frame col %d beyond line width %d", frameCol, len(runes))
	}
	return runes[frameCol]
}

// TestSOIEntryExitMarkersDrawAtRingCrossings: the Entry ▷ and Exit ◁ glyphs
// (ADR 0020 family) land in the exact cells the pass's ring crossings
// project to — position asserted through the same Project mapping the
// renderer uses, not a blind string search.
func TestSOIEntryExitMarkersDrawAtRingCrossings(t *testing.T) {
	v := newSOIPassTestView()
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	moonIdx := setupMoonCoast(t, w)
	w.ViewMode = sim.ViewTilted
	w.Focus = sim.Focus{Kind: sim.FocusBody, BodyIdx: moonIdx}
	out := v.Render(w, 0, 200, 60)

	pass, ok := w.LiveSOIPass()
	if !ok {
		t.Fatal("precondition: no live SOI Pass on the Moon coast")
	}
	if !pass.HasEntry || !pass.HasExit {
		t.Fatalf("precondition: pass missing ring crossings (entry=%v exit=%v)", pass.HasEntry, pass.HasExit)
	}

	if got, want := glyphAtProjected(t, v, out, w.EntryPosition(pass)), render.MarkerGlyph(render.MarkerSOIEntry); got != want {
		t.Errorf("cell at the SOI-entry crossing shows %q, want the Entry glyph %q", got, want)
	}
	if got, want := glyphAtProjected(t, v, out, w.ExitPosition(pass)), render.MarkerGlyph(render.MarkerSOIExit); got != want {
		t.Errorf("cell at the SOI-exit crossing shows %q, want the Exit glyph %q", got, want)
	}
}

// TestSOIPassChipShowsEntryTime: the SOI PASS chip surfaces the predicted
// SOI-entry countdown — the value half of the Entry marker (ADR 0020 C /
// ADR 0021 C: glyph marks which/where, the chip carries the number). Both
// chip forms carry it: the no-node single pass and the dual-arc planned row.
func TestSOIPassChipShowsEntryTime(t *testing.T) {
	v := newSOIPassTestView()
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	setupMoonCoast(t, w)
	if out := v.Render(w, 0, 200, 60); !strings.Contains(out, "T-entry:") {
		t.Errorf("no-node SOI PASS chip missing the T-entry row")
	}

	// Dual-arc form: transfer planted, craft still at LEO.
	v2 := newSOIPassTestView()
	w2, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	plantMoonTransferAtLEO(t, w2)
	out := v2.Render(w2, 0, 200, 60)
	if !strings.Contains(out, "planned") {
		t.Fatalf("precondition: dual-arc chip missing its planned row:\n%s", out)
	}
	if !strings.Contains(out, "T-entry:") {
		t.Errorf("dual-arc SOI PASS chip missing the planned T-entry row")
	}
}
