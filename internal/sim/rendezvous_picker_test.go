package sim

import (
	"errors"
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/planner"
)

// rendezvousPhaseMismatchWorld places the sister craft on the SAME
// circular orbit as the active craft (same altitude, same plane) but
// 90° ahead in phase — far enough that a single trim-rung nudge exceeds
// the burn ceiling (ErrRendezvousBurnTooLarge), while the Rendezvous
// Planner's tangential-burn model, working from the same coplanar,
// same-radius geometry, has a real ladder to solve.
func rendezvousPhaseMismatchWorld(t *testing.T) *World {
	t.Helper()
	w := rendezvousTwoCraftWorld(t)
	active := w.Crafts[0]
	target := w.Crafts[1]
	h := active.State.R.Cross(active.State.V)
	axis := h.Unit()
	angle := 90 * math.Pi / 180
	target.State.R = rotateAboutAxis(active.State.R, axis, angle)
	target.State.V = rotateAboutAxis(active.State.V, axis, angle)
	target.Primary = active.Primary
	return w
}

// rendezvousPlaneMismatchWorld places the sister craft 2° ahead in phase
// AND 2° out of the active craft's orbital plane — mirroring the
// planner package's own TestRecommendRendezvousNudge_InclinedTarget
// fixture exactly (chaser + inclinedCircularState(r, 2°, 2°, mu)), which
// is deliberately chosen there so the trim rung SUCCEEDS with an
// AxisNormalPlus/Minus pick (that test's whole point is exercising the
// normal-axis branch). Reusing that same geometry here is what proves
// the ordering invariant in PlanRendezvousOrOpenPicker's own doc
// comment: a 30° tilt would ALSO fail the trim rung on its own merits
// (no improvement available, independent of any plane check), so a test
// built on one couldn't tell "the plane gate fired first" apart from
// "the trim rung just didn't find anything" — only a geometry where the
// trim rung WOULD otherwise plant a plane-changing burn actually
// exercises "do not let K plant a plane change" (ADR 0045 §2). 2° clears
// rendezvousPlaneTolDeg's 1° tolerance easily.
func rendezvousPlaneMismatchWorld(t *testing.T) *World {
	t.Helper()
	w := rendezvousTwoCraftWorld(t)
	active := w.Crafts[0]
	target := w.Crafts[1]

	h := active.State.R.Cross(active.State.V)
	hAxis := h.Unit()
	phaseAngle := 2 * math.Pi / 180
	r1 := rotateAboutAxis(active.State.R, hAxis, phaseAngle)
	v1 := rotateAboutAxis(active.State.V, hAxis, phaseAngle)

	tiltAxis := active.State.R.Unit()
	tiltAngle := 2 * math.Pi / 180
	target.State.R = rotateAboutAxis(r1, tiltAxis, tiltAngle)
	target.State.V = rotateAboutAxis(v1, tiltAxis, tiltAngle)
	target.Primary = active.Primary
	return w
}

// TestPlanRendezvousOrOpenPicker_CloseNearMatched_PlantsDirect — ADR
// 0045 S6 acceptance: "K on a near-matched close pair still plants a
// nudge directly, with no picker." rendezvousSmallLagWorld is the exact
// fixture PlanRendezvousNudge's own happy-path test uses.
func TestPlanRendezvousOrOpenPicker_CloseNearMatched_PlantsDirect(t *testing.T) {
	w := rendezvousSmallLagWorld(t)
	c := w.ActiveCraft()
	if len(c.Nodes) != 0 {
		t.Fatalf("precondition: %d nodes, want 0", len(c.Nodes))
	}

	out, err := w.PlanRendezvousOrOpenPicker()
	if err != nil {
		t.Fatalf("PlanRendezvousOrOpenPicker err: %v", err)
	}
	if out.Planted == nil {
		t.Fatalf("expected Planted != nil for a near-matched close pair, got OpenPicker=%v", out.OpenPicker)
	}
	if out.OpenPicker {
		t.Errorf("OpenPicker=true on a near-matched close pair — must plant directly, no picker")
	}
	if len(c.Nodes) != 1 {
		t.Fatalf("expected 1 node planted, got %d", len(c.Nodes))
	}
}

// TestPlanRendezvousOrOpenPicker_PhaseMismatch_OpensPicker — ADR 0045 S6
// acceptance: "K on a phase-mismatched pair opens the picker rather than
// returning a refusal." Nothing is planted; the returned ladder carries
// at least one usable row.
func TestPlanRendezvousOrOpenPicker_PhaseMismatch_OpensPicker(t *testing.T) {
	w := rendezvousPhaseMismatchWorld(t)
	c := w.ActiveCraft()

	out, err := w.PlanRendezvousOrOpenPicker()
	if err != nil {
		t.Fatalf("PlanRendezvousOrOpenPicker err: %v (expected the picker to open, not a refusal)", err)
	}
	if out.Planted != nil {
		t.Fatalf("expected no direct plant on a phase-mismatched pair, got Planted=%+v", out.Planted)
	}
	if !out.OpenPicker {
		t.Fatalf("expected OpenPicker=true on a phase-mismatched pair")
	}
	if out.LadderErr != nil {
		t.Fatalf("LadderErr = %v, want nil (this geometry has a real ladder)", out.LadderErr)
	}
	foundOk := false
	for _, row := range out.Ladder.Rows {
		if row.Ok {
			foundOk = true
			break
		}
	}
	if !foundOk {
		t.Fatalf("expected at least one Ok row in the picker's ladder: %+v", out.Ladder.Rows)
	}
	if len(c.Nodes) != 0 {
		t.Fatalf("opening the picker must not plant anything, got %d nodes", len(c.Nodes))
	}
}

// TestPlanRendezvousOrOpenPicker_PlaneMismatch_NamesI — ADR 0045 S6
// acceptance: "K on a plane-mismatched pair names [I] and plants
// nothing." Critically, this must refuse BEFORE ever attempting the
// trim-rung plant (ADR 0045 §2: "do not let K plant a plane change") —
// checked by asserting zero nodes even though PlanRendezvousNudge alone,
// called against this same fixture, does not refuse on its own (it has
// no plane gate of its own; see the doc comment on
// PlanRendezvousOrOpenPicker).
func TestPlanRendezvousOrOpenPicker_PlaneMismatch_NamesI(t *testing.T) {
	// Precondition, proven concretely rather than just asserted in prose:
	// the trim rung ALONE, on this exact fixture, plants a plane-changing
	// (Normal±) burn when called directly — so the modal wrapper's refusal
	// below is doing real work, not just reporting a geometry that was
	// never plantable in the first place.
	precheck := rendezvousPlaneMismatchWorld(t)
	preAdv, preErr := precheck.PlanRendezvousNudge()
	if preErr != nil {
		t.Fatalf("test fixture broken: PlanRendezvousNudge alone must succeed on this geometry (so the modal wrapper's plane gate is the thing stopping it), got err=%v", preErr)
	}
	if preAdv.Axis != planner.AxisNormalPlus && preAdv.Axis != planner.AxisNormalMinus {
		t.Fatalf("test fixture broken: expected a Normal± plant (a plane-changing side effect) on this geometry, got Axis=%v", preAdv.Axis)
	}

	w := rendezvousPlaneMismatchWorld(t)
	c := w.ActiveCraft()

	out, err := w.PlanRendezvousOrOpenPicker()
	if err == nil {
		t.Fatalf("expected a refusal for a plane-mismatched pair, got out=%+v", out)
	}
	if !errors.Is(err, ErrRendezvousPlaneMismatch) {
		t.Fatalf("err = %v, want ErrRendezvousPlaneMismatch", err)
	}
	if out.Planted != nil || out.OpenPicker {
		t.Errorf("expected a bare refusal (no Planted, no OpenPicker), got %+v", out)
	}
	if len(c.Nodes) != 0 {
		t.Fatalf("plane-mismatched K must plant nothing (not even a trim-rung side effect), got %d nodes", len(c.Nodes))
	}
}

// TestPlanRendezvousOrOpenPicker_NoTarget mirrors the existing K
// refusal wording exactly — RecommendRendezvousLadder's own no-target gate
// is the same sentinel PlanRendezvousNudge itself returns
// (TestPlanRendezvousReachesPlannerFromBodyInfo pins "rendezvous: no
// vessel target" at the tui layer), so this must not regress that text.
func TestPlanRendezvousOrOpenPicker_NoTarget(t *testing.T) {
	w := mustWorld(t)
	_, err := w.PlanRendezvousOrOpenPicker()
	if !errors.Is(err, ErrRendezvousNoTarget) {
		t.Fatalf("err = %v, want ErrRendezvousNoTarget", err)
	}
}

// TestPlanRendezvousOrOpenPicker_AlreadyDocked confirms the docked gate
// (which the Rendezvous Planner has no equivalent of) still short-circuits
// straight to a refusal rather than opening the picker.
func TestPlanRendezvousOrOpenPicker_AlreadyDocked(t *testing.T) {
	w := rendezvousTwoCraftWorld(t)
	active := w.Crafts[0]
	target := w.Crafts[1]
	target.State.R = active.State.R
	target.State.V = active.State.V
	target.Primary = active.Primary

	out, err := w.PlanRendezvousOrOpenPicker()
	if !errors.Is(err, ErrRendezvousAlreadyDocked) {
		t.Fatalf("err = %v, want ErrRendezvousAlreadyDocked", err)
	}
	if out.OpenPicker {
		t.Errorf("OpenPicker=true for an already-docked pair — nothing to meet")
	}
}

// TestPlanRendezvousOrOpenPicker_DefaultPlaceIsTheirOrbit pins the
// picker's initial Rendezvous Orbit, mirroring ADR 0045 §2's own mockup.
func TestPlanRendezvousOrOpenPicker_DefaultPlaceIsTheirOrbit(t *testing.T) {
	w := rendezvousPhaseMismatchWorld(t)
	out, err := w.PlanRendezvousOrOpenPicker()
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if out.Place != planner.RendezvousTheirOrbit {
		t.Errorf("default Place = %v, want RendezvousTheirOrbit", out.Place)
	}
}
