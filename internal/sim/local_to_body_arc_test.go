package sim

import (
	"math"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// plantKernCursorPass plants the Kern→Cursor transfer and returns the
// planned pass plus the SOI radius the rebased arc must live within —
// the shared setup for the Local-to-Body Arc tests (ADR 0021 B).
func plantKernCursorPass(t *testing.T, w *World) (SOIPass, float64) {
	t.Helper()
	cursorIdx, kern, cursor := setupKernCursor(t, w)
	if _, err := w.PlanTransfer(cursorIdx); err != nil {
		t.Fatalf("PlanTransfer(Cursor): %v", err)
	}
	pass, ok := w.PlannedSOIPass()
	if !ok {
		t.Fatal("PlannedSOIPass returned ok=false; expected the planted capture pass")
	}
	if pass.Body.ID != cursor.ID {
		t.Fatalf("pass body = %q, want Cursor", pass.Body.EnglishName)
	}
	if len(pass.ArcSegments) == 0 {
		t.Fatal("pass has no arc segments")
	}
	return pass, physics.SOIRadius(cursor, kern)
}

// TestForeignSegmentsRecordBodyRelativePoints: every predicted segment
// carries body-relative samples (offset from its owning primary at each
// sample's clock) alongside the inertial ones — the Local-to-Body Arc's
// raw material, recorded at prediction time in the shared predictor
// (ADR 0021 B). For the foreign Cursor segment those offsets are in-SOI
// by construction, and the smallest one is the drawn perilune.
func TestForeignSegmentsRecordBodyRelativePoints(t *testing.T) {
	w := mustWorld(t)
	pass, soi := plantKernCursorPass(t, w)

	minD := math.Inf(1)
	for _, s := range pass.ArcSegments {
		if len(s.RelPoints) != len(s.Points) {
			t.Fatalf("segment %q: %d RelPoints for %d Points — body-relative samples missing",
				s.PrimaryID, len(s.RelPoints), len(s.Points))
		}
		for _, r := range s.RelPoints {
			if d := r.Norm(); d > soi*1.05 {
				t.Errorf("body-relative sample %.0f km from Cursor exceeds its SOI (%.0f km)", d/1e3, soi/1e3)
			} else if d < minD {
				minD = d
			}
		}
	}
	if !pass.HasPerilunePt {
		t.Fatal("pass has no perilune point")
	}
	// PeriluneRel is the analytic closest approach (ADR 0023 B): its norm
	// matches the chip's PeriluneRadius exactly, replacing the old
	// nearest-sample snap that re-phased each prediction.
	if got := pass.PeriluneRel.Norm(); math.Abs(got-pass.PeriluneRadius) > 1 {
		t.Errorf("PeriluneRel norm = %.0f km, want analytic PeriluneRadius %.0f km", got/1e3, pass.PeriluneRadius/1e3)
	}
	// It still sits at the drawn arc's closest sample to SOI scale — the
	// analytic point is two-body while the sample carries the integrated
	// perturbation, so they agree closely without strictly bounding each
	// other.
	if math.Abs(pass.PeriluneRel.Norm()-minD) > 0.1*soi {
		t.Errorf("analytic perilune %.0f km disagrees with nearest sample %.0f km by >0.1×SOI", pass.PeriluneRel.Norm()/1e3, minD/1e3)
	}
}

// TestLocalToBodyArcExtentIsSOIScale pins the drawn extent of the rebased
// foreign-SOI arc to SOI scale: anchored at Cursor's CURRENT position the
// draw points all land within ~1× the SOI of the body, while the same
// samples at their heliocentric (absolute) positions smear across many
// times the SOI (measured ~24× for Kern→Cursor — the #144/#147 straight
// line). The before/after ratio is logged for the record.
func TestLocalToBodyArcExtentIsSOIScale(t *testing.T) {
	w := mustWorld(t)
	pass, soi := plantKernCursorPass(t, w)
	c := w.ActiveCraft()
	cursorNow := w.BodyPosition(pass.Body)

	var maxDraw, maxAbs float64
	var abs []orbital.Vec3
	for _, s := range pass.ArcSegments {
		for _, p := range w.SegmentDrawPoints(s, c.Primary.ID) {
			if d := p.Sub(cursorNow).Norm(); d > maxDraw {
				maxDraw = d
			}
		}
		for _, p := range s.Points {
			abs = append(abs, p)
			if d := p.Sub(cursorNow).Norm(); d > maxAbs {
				maxAbs = d
			}
		}
	}
	// The arc's own smear — max distance between absolute samples — is the
	// ADR 0021 "~24× the SOI" measurement (the body keeps moving through
	// the transit, stretching the drawn hyperbola along its path).
	var smear float64
	for i := range abs {
		for j := i + 1; j < len(abs); j++ {
			if d := abs[i].Sub(abs[j]).Norm(); d > smear {
				smear = d
			}
		}
	}
	t.Logf("arc extent around Cursor's current position: rebased %.2f×SOI, heliocentric %.2f×SOI; heliocentric smear %.2f×SOI (SOI %.0f km)",
		maxDraw/soi, maxAbs/soi, smear/soi, soi/1e3)

	// Premise: the heliocentric samples really do smear far past the SOI
	// (Cursor moves between "now" and the transit). If this goes stale the
	// repro no longer demonstrates anything.
	if maxAbs < 5*soi {
		t.Fatalf("heliocentric arc extent only %.1f×SOI — smear premise gone stale", maxAbs/soi)
	}
	// The rebased ink wraps the body: every draw point within ~1×SOI of
	// Cursor's current position (in-SOI offsets anchored at the body).
	if maxDraw > soi*1.05 {
		t.Errorf("rebased arc extends %.1f×SOI from Cursor's current position, want ≤ ~1×SOI (Local-to-Body)", maxDraw/soi)
	}
}

// TestLivePassAndPlantedLegAgree: the same encounter rebased through both
// prediction paths — the SOI Pass arc (soiPassFromState) and the
// planted-node leg segments (PredictedSegmentsFrom, what drawNodes plots)
// — produces the same body-relative geometry. Both sites share one step
// core (the #66 two-site lesson); this pins that the Local-to-Body rebase
// lives in that shared layer, so the bright pass arc and the planted leg
// at one body can never disagree.
func TestLivePassAndPlantedLegAgree(t *testing.T) {
	w := mustWorld(t)
	planned, soi := plantKernCursorPass(t, w)
	c := w.ActiveCraft()

	// Path 1: the planted transfer leg's segments, exactly as the node
	// renderer draws them. Only the transfer leg (leg 0) — the later legs
	// fly the post-capture orbit, a different trajectory than the flyby.
	legs := w.PredictedLegs()
	if len(legs) == 0 {
		t.Fatal("no predicted legs")
	}
	leg := legs[0]
	legMin := math.Inf(1)
	found := false
	for _, s := range w.PredictedSegmentsFrom(leg.State, leg.Primary, leg.StartClock, leg.HorizonSecs, leg.Samples) {
		if s.PrimaryID != planned.Body.ID {
			continue
		}
		found = true
		for _, r := range s.RelPoints {
			if d := r.Norm(); d < legMin {
				legMin = d
			}
		}
	}
	if !found {
		t.Fatal("no planted leg segment reaches Cursor's SOI")
	}

	// Path 2: the live pass from the post-departure coast — same state the
	// transfer leg starts from, flown with no nodes.
	w.Clock.SimTime = leg.StartClock
	c.State = leg.State
	c.Primary = leg.Primary
	c.Nodes = nil
	live, ok := w.LiveSOIPass()
	if !ok {
		t.Fatal("live pass absent on the post-departure coast")
	}
	if live.Body.ID != planned.Body.ID {
		t.Fatalf("live pass body %q != planned pass body %q", live.Body.ID, planned.Body.ID)
	}

	// Same body-relative geometry, both ways. The legs and the pass sample
	// at different densities, so the perilune-sample agreement tolerance is
	// a small fraction of the SOI, not bitwise.
	tol := soi * 0.05
	if d := live.PeriluneRel.Sub(planned.PeriluneRel).Norm(); d > tol {
		t.Errorf("live vs planned PeriluneRel differ by %.0f km (tol %.0f km) — the two rebase paths disagree", d/1e3, tol/1e3)
	}
	if d := math.Abs(legMin - live.PeriluneRel.Norm()); d > tol {
		t.Errorf("planted-leg min body-relative distance %.0f km vs live pass perilune %.0f km (tol %.0f km)",
			legMin/1e3, live.PeriluneRel.Norm()/1e3, tol/1e3)
	}
}

// TestCounterfactualRebasesLikeBrightArc: with a node planted *after* the
// encounter, the dim node-capped counterfactual arc carries the same
// body-relative samples as the bright live pass — one rebase rule for all
// foreign-SOI ink (ADR 0021 B), so the two arcs at the Moon can't split.
func TestCounterfactualRebasesLikeBrightArc(t *testing.T) {
	w := mustWorld(t)
	moonCoast(t, w)
	c := w.ActiveCraft()

	live, ok := w.LiveSOIPass()
	if !ok {
		t.Fatal("precondition: live pass should reach the Moon")
	}
	soi := physics.SOIRadius(live.Body, parentBody(w, live.Body))

	// Node well past the encounter: the counterfactual still reaches the SOI.
	c.Nodes = []spacecraft.ManeuverNode{{TriggerTime: w.Clock.SimTime.Add(time.Duration(live.TimeToPerilune * 2 * float64(time.Second)))}}
	cf, ok := w.CounterfactualSOIPass()
	if !ok || cf.Body.ID != live.Body.ID {
		t.Fatalf("counterfactual should still reach the Moon with a post-encounter node; ok=%v", ok)
	}

	for _, s := range cf.ArcSegments {
		if len(s.RelPoints) != len(s.Points) {
			t.Fatalf("counterfactual segment missing body-relative samples (%d vs %d)", len(s.RelPoints), len(s.Points))
		}
		for _, r := range s.RelPoints {
			if d := r.Norm(); d > soi*1.05 {
				t.Errorf("counterfactual rel sample %.1f×SOI from the Moon — not rebased like the bright arc", d/soi)
			}
		}
	}
	if d := cf.PeriluneRel.Sub(live.PeriluneRel).Norm(); d > soi*0.05 {
		t.Errorf("counterfactual PeriluneRel differs from live by %.0f km — rebase paths split", d/1e3)
	}
}

// TestPerilunePositionConvergesTowardBody: the Perilune marker draws at
// the body's CURRENT position plus the body-relative offset, so it stays
// glued to the body's neighborhood (≤ SOI) and converges on the true
// perilune as arrival nears — the body's current position closes on its
// encounter position (ADR 0021 B).
func TestPerilunePositionConvergesTowardBody(t *testing.T) {
	w := mustWorld(t)
	moonCoast(t, w)
	c := w.ActiveCraft()

	errAt := func() (float64, float64) {
		pass, ok := w.LiveSOIPass()
		if !ok {
			t.Fatal("live pass absent")
		}
		if !pass.HasPerilunePt {
			t.Fatal("pass has no perilune point")
		}
		marker := w.PerilunePosition(pass)
		bodyNow := w.BodyPosition(pass.Body)
		soi := physics.SOIRadius(pass.Body, parentBody(w, pass.Body))
		if d := marker.Sub(bodyNow).Norm(); d > soi {
			t.Errorf("Perilune marker %.0f km from the Moon's current position (SOI %.0f km) — not anchored Local-to-Body", d/1e3, soi/1e3)
		}
		// Truth proxy: the body's position at TCA plus the same offset.
		truth := w.BodyPositionAt(pass.Body, w.Clock.SimTime.Add(time.Duration(pass.TimeToPerilune*float64(time.Second)))).Add(pass.PeriluneRel)
		return marker.Sub(truth).Norm(), pass.TimeToPerilune
	}

	err0, tca := errAt()

	// Coast half-way to the encounter and re-read: the marker must have
	// closed on the truth.
	dt := tca / 2
	state, primary := w.propagateStateWithPrimary(c.State, c.Primary, w.Clock.SimTime, dt)
	c.State, c.Primary = state, primary
	w.Clock.SimTime = w.Clock.SimTime.Add(time.Duration(dt * float64(time.Second)))
	err1, _ := errAt()

	if err1 > err0*0.75 {
		t.Errorf("Perilune marker error did not converge: %.0f km at plant, %.0f km half-way to arrival", err0/1e3, err1/1e3)
	}
}
