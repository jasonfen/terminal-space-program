package screens

import (
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// setupKernCursorTransfer places the active craft in a coplanar circular
// parking orbit around Kern (Lumen system) and plants the transfer to
// Cursor — the measured Local-to-Body repro (the heliocentric smear was
// ~24× the SOI on this pair). Mirrors the sim package's setupKernCursor,
// replicated here because that helper isn't visible across packages.
func setupKernCursorTransfer(t *testing.T, w *sim.World) (cursorIdx int, kern, cursor bodies.CelestialBody) {
	t.Helper()
	sysIdx := -1
	for i, s := range w.Systems {
		if s.Name == "Lumen" {
			sysIdx = i
		}
	}
	if sysIdx < 0 {
		t.Skip("Lumen not loaded")
	}
	w.SystemIdx = sysIdx
	craft := w.ActiveCraft()
	craft.SystemIdx = sysIdx
	sys := w.System()

	kernIdx := -1
	cursorIdx = -1
	for i, b := range sys.Bodies {
		switch b.EnglishName {
		case "Kern":
			kernIdx = i
		case "Cursor":
			cursorIdx = i
		}
	}
	if kernIdx < 0 || cursorIdx < 0 {
		t.Skip("Kern/Cursor not in Lumen")
	}
	kern, cursor = sys.Bodies[kernIdx], sys.Bodies[cursorIdx]

	rPark := kern.RadiusMeters() * 1.08
	if atm := kern.Atmosphere; atm != nil {
		if min := kern.RadiusMeters() + atm.CutoffAltitude + 50e3; min > rPark {
			rPark = min
		}
	}
	// Circular parking orbit in Cursor's orbital plane (the sim package's
	// moonPlaneCircularState construction).
	mel := orbital.ElementsFromBody(cursor)
	sI, cI := math.Sin(mel.I), math.Cos(mel.I)
	sO, cO := math.Sin(mel.Omega), math.Cos(mel.Omega)
	moonN := orbital.Vec3{X: sO * sI, Y: -cO * sI, Z: cI}.Unit()
	ref := orbital.Vec3{X: 1}
	if math.Abs(moonN.Dot(ref)) > 0.9 {
		ref = orbital.Vec3{Y: 1}
	}
	e1 := ref.Sub(moonN.Scale(moonN.Dot(ref))).Unit()
	e2 := moonN.Cross(e1)
	craft.Primary = kern
	craft.Landed = false
	craft.State.R = e1.Scale(rPark)
	craft.State.V = e2.Scale(math.Sqrt(kern.GravitationalParameter() / rPark))

	if _, err := w.PlanTransfer(cursorIdx); err != nil {
		t.Fatalf("PlanTransfer(Cursor): %v", err)
	}
	return cursorIdx, kern, cursor
}

// countBraille counts non-blank braille cells (U+2801–U+28FF) in a rendered
// frame — drawn trajectory/orbit ink, excluding the blank braille space.
func countBraille(s string) int {
	n := 0
	for _, r := range stripANSI(s) {
		if r > 0x2800 && r <= 0x28FF {
			n++
		}
	}
	return n
}

// TestPlantedKernCursorArcWrapsCursorDisk is the render-level acceptance for
// the Local-to-Body Arc (ADR 0021 B / #147): a planted Kern→Cursor transfer,
// focused on Cursor, draws the capture hyperbola wrapping Cursor's drawn
// disk — the foreign-SOI ink anchors at Cursor's CURRENT position at SOI
// scale, with visible curvature near perilune, instead of smearing along
// Cursor's transit path at heliocentric sample positions (~24× the SOI,
// where the arc reads as a straight line).
func TestPlantedKernCursorArcWrapsCursorDisk(t *testing.T) {
	v := newSOIPassTestView()
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	cursorIdx, kern, cursor := setupKernCursorTransfer(t, w)
	soi := physics.SOIRadius(cursor, kern)

	// Plain body focus — the KSP model: reading an encounter = focus the body.
	w.ViewMode = sim.ViewTilted
	w.Focus = sim.Focus{Kind: sim.FocusBody, BodyIdx: cursorIdx}
	out := v.Render(w, 0, 200, 60)

	// Ink is on the canvas at all.
	if n := countBraille(out); n < 20 {
		t.Fatalf("only %d non-blank braille cells rendered — canvas effectively empty", n)
	}

	cursorNow := w.BodyPosition(cursor)

	// Camera: centered near Cursor's current position (where the rebased arc
	// and the disk now live), framed at SOI scale — not the heliocentric
	// smear (~24×SOI) and not centered a transfer away at the arrival point.
	center := v.canvas.CenterWorld()
	if d := center.Sub(cursorNow).Norm(); d > 1.5*soi {
		t.Errorf("canvas centered %.1f×SOI from Cursor's current position — not framing the Local-to-Body arc", d/soi)
	}
	if px := soi * v.canvas.Scale(); px < 25 {
		t.Errorf("Cursor's SOI spans only %.1f braille px — framed to the heliocentric smear (~24×SOI), not SOI scale", px)
	}

	// Geometry of the drawn (rebased) arc: gather the foreign-SOI draw
	// points the node legs plot.
	var draw []orbital.Vec3
	for _, leg := range w.PredictedLegs() {
		for _, seg := range w.PredictedSegmentsFrom(leg.State, leg.Primary, leg.StartClock, leg.HorizonSecs, leg.Samples) {
			if seg.PrimaryID != cursor.ID {
				continue
			}
			draw = append(draw, w.SegmentDrawPoints(seg, kern.ID)...)
		}
	}
	if len(draw) < 3 {
		t.Fatalf("only %d foreign-SOI draw points — no arc to assert on", len(draw))
	}

	// Wraps the disk: every draw point within ~1×SOI of the disk, and the
	// whole arc projects onto the canvas.
	periIdx, periD := 0, math.Inf(1)
	for i, p := range draw {
		if d := p.Sub(cursorNow).Norm(); d > 1.05*soi {
			t.Fatalf("draw point %d sits %.1f×SOI from Cursor's disk — arc not anchored at the body", i, d/soi)
		} else if d < periD {
			periD = d
			periIdx = i
		}
	}
	for _, p := range []orbital.Vec3{draw[0], draw[periIdx], draw[len(draw)-1]} {
		if _, _, ok := v.canvas.Project(p); !ok {
			t.Errorf("rebased arc point %v projects off-canvas — encounter not in frame", p)
		}
	}

	// Visible curvature near perilune: the perilune sample must sit well off
	// the entry→exit chord. At heliocentric sample positions the transit is
	// smeared nearly straight (sagitta ≪ extent) — that's the bug this pins.
	entry, exit, peri := draw[0], draw[len(draw)-1], draw[periIdx]
	chord := exit.Sub(entry)
	sagitta := 0.0
	if chord.Norm() > 0 {
		rel := peri.Sub(entry)
		along := chord.Unit().Scale(rel.Dot(chord.Unit()))
		sagitta = rel.Sub(along).Norm()
	}
	if sagitta < 0.05*soi {
		t.Errorf("perilune sits only %.3f×SOI off the entry→exit chord — arc reads as a straight line, no visible curvature", sagitta/soi)
	}
}
