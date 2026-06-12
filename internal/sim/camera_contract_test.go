package sim

import (
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
)

// moonCoast puts the active craft on the node-free LEO→Moon coast where
// LiveSOIPass returns ok with Body.ID == "moon" — the deterministic SOI-Pass
// setup described in the predict_test helper's docstring.
func moonCoast(t *testing.T, w *World) {
	t.Helper()
	leg := coplanarLEOTowardMoon(t, w)
	c := w.ActiveCraft()
	c.State = leg.State
	c.Primary = leg.Primary
	w.Clock.SimTime = leg.StartClock
	c.Nodes = nil
	c.Landed = false
}

// parentBody returns the body p orbits (its ParentID), or the system root when
// p has no parent in the catalog. Test-side alias for the production lookup
// the encounter-aware fit uses (ADR 0021 F / #143 — SOI must be
// parent-relative, not root-relative).
func parentBody(w *World, p bodies.CelestialBody) bodies.CelestialBody {
	return w.parentBodyOf(p)
}

// TestViewModeCycleOrder pins ADR 0021 D: ViewTarget and ViewSOIPass left the
// cycle, so `v` walks Tilted → Top → Right → Bottom → Left → OrbitFlat →
// Launch and wraps — projections only, unconditionally selectable, no
// world-state gates.
func TestViewModeCycleOrder(t *testing.T) {
	w := mustWorld(t)
	moonCoast(t, w) // an active pass must NOT add a view back into the cycle

	want := []ViewMode{ViewTop, ViewRight, ViewBottom, ViewLeft, ViewOrbitFlat, ViewLaunch, ViewTilted}
	w.ViewMode = ViewTilted
	for i, m := range want {
		w.CycleViewMode()
		if w.ViewMode != m {
			t.Fatalf("cycle step %d: got %s, want %s", i+1, w.ViewMode, m)
		}
	}
	if len(AllViewModes) != 7 {
		t.Errorf("AllViewModes has %d modes, want 7 (projections + launch only)", len(AllViewModes))
	}
}

// TestFocusZoomRadiusEncounterAware pins ADR 0021 F on the live-pass path:
// focusing a Body with an active SOI Pass fits to ~1.3× its parent-relative
// SOI (ring + arc + markers in frame), while the same focus with no pass
// keeps the pre-existing terminal-body close-up (8× body radius).
func TestFocusZoomRadiusEncounterAware(t *testing.T) {
	w := mustWorld(t)
	moonCoast(t, w)

	pass, ok := w.LiveSOIPass()
	if !ok {
		t.Fatal("precondition: no live SOI Pass on the Moon coast")
	}
	moonIdx := -1
	for i, b := range w.System().Bodies {
		if b.ID == pass.Body.ID {
			moonIdx = i
		}
	}
	if moonIdx < 0 {
		t.Fatalf("pass body %q not found in system", pass.Body.ID)
	}
	moon := w.System().Bodies[moonIdx]

	w.Focus = Focus{Kind: FocusBody, BodyIdx: moonIdx}
	got := w.FocusZoomRadius()
	want := physics.SOIRadius(moon, parentBody(w, moon)) * 1.3
	if math.Abs(got-want) > want*1e-9 {
		t.Errorf("FocusZoomRadius with active pass = %.0f km, want 1.3× parent-relative SOI = %.0f km", got/1e3, want/1e3)
	}

	// Clear the pass — drop the craft onto a stable LEO. The same focus now
	// keeps the ordinary terminal-body close-up.
	c := w.ActiveCraft()
	mu := c.Primary.GravitationalParameter()
	r := c.Primary.RadiusMeters() + 300e3
	c.State.R = orbital.Vec3{X: r}
	c.State.V = orbital.Vec3{Y: math.Sqrt(mu / r)}
	if _, ok := w.LiveSOIPass(); ok {
		t.Fatal("precondition: stable LEO should have no SOI Pass")
	}
	got = w.FocusZoomRadius()
	want = moon.RadiusMeters() * 8
	if math.Abs(got-want) > want*1e-9 {
		t.Errorf("FocusZoomRadius without pass = %.0f km, want terminal-body 8× radius = %.0f km", got/1e3, want/1e3)
	}
}

// TestFocusZoomRadiusEncounterAwarePlantedTransfer pins the planted-pass path
// (bestSOIPass prefers PlannedSOIPass): focusing Cursor while flying a planted
// Kern→Cursor transfer — whose pre-burn orbit can't reach Cursor, so
// LiveSOIPass alone is false — still fits to 1.3× Cursor's SOI measured
// against its parent Kern, not the Lumen system root (#143).
func TestFocusZoomRadiusEncounterAwarePlantedTransfer(t *testing.T) {
	w := mustWorld(t)
	cursorIdx, kern, cursor := setupKernCursor(t, w)
	if _, err := w.PlanTransfer(cursorIdx); err != nil {
		t.Fatalf("PlanTransfer(Cursor): %v", err)
	}
	if _, ok := w.PlannedSOIPass(); !ok {
		t.Fatal("planted transfer produced no PlannedSOIPass")
	}

	w.Focus = Focus{Kind: FocusBody, BodyIdx: cursorIdx}
	got := w.FocusZoomRadius()
	want := physics.SOIRadius(cursor, kern) * 1.3
	if math.Abs(got-want) > want*1e-9 {
		t.Errorf("FocusZoomRadius = %.0f km, want 1.3× Kern-relative SOI = %.0f km", got/1e3, want/1e3)
	}
	// Guard the #143 shape: the root-relative SOI is a different (much larger)
	// number — if these coincide the parent-relative assertion above is vacuous.
	rootSOI := physics.SOIRadius(cursor, w.System().Bodies[0]) * 1.3
	if math.Abs(rootSOI-want) < want*0.01 {
		t.Skip("root-relative and parent-relative SOI coincide; cannot discriminate")
	}
	if math.Abs(got-rootSOI) < rootSOI*1e-9 {
		t.Errorf("FocusZoomRadius matched the root-relative SOI %.0f km — the #143 wrong-parent fit", rootSOI/1e3)
	}
}
