package sim

import (
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/physics"
)

// TestPassEntryExitCrossingsLandOnRing: the SOI Pass arc's ring crossings
// (ADR 0021 C) sit on the parent-relative SOI Ring — the body-relative
// Entry/Exit offsets are ≈ the SOI radius, and the rebased arc's first and
// last DRAWN samples (SegmentDrawPoints, anchored at the Body's current
// position) land on the ring around the Body. This is the geometry the
// Entry ▷ / Exit ◁ glyphs mark.
func TestPassEntryExitCrossingsLandOnRing(t *testing.T) {
	w := mustWorld(t)
	moonCoast(t, w)

	pass, ok := w.LiveSOIPass()
	if !ok {
		t.Fatal("precondition: no live SOI Pass on the Moon coast")
	}
	soi := physics.SOIRadius(pass.Body, parentBody(w, pass.Body))
	if got := w.BodySOIRadius(pass.Body); math.Abs(got-soi) > 1 {
		t.Fatalf("BodySOIRadius = %.0f km, want parent-relative %.0f km", got/1e3, soi/1e3)
	}

	if !pass.HasEntry {
		t.Fatal("pass has no SOI Entry crossing — the arc should open on the ring")
	}
	if d := pass.EntryRel.Norm(); math.Abs(d-soi) > soi*0.05 {
		t.Errorf("EntryRel %.0f km from the Moon, want ring radius %.0f km (±5%%)", d/1e3, soi/1e3)
	}
	if !pass.HasExit {
		t.Fatal("pass has no SOI Exit crossing — a non-impact flyby should close on the ring")
	}
	if d := pass.ExitRel.Norm(); math.Abs(d-soi) > soi*0.05 {
		t.Errorf("ExitRel %.0f km from the Moon, want ring radius %.0f km (±5%%)", d/1e3, soi/1e3)
	}

	// The marker draw positions anchor at the Body's CURRENT position, the
	// same anchor SegmentDrawPoints gives the arc.
	bodyNow := w.BodyPosition(pass.Body)
	if d := w.EntryPosition(pass).Sub(bodyNow).Norm(); math.Abs(d-soi) > soi*0.05 {
		t.Errorf("EntryPosition %.0f km from the Moon's current position, want on the ring (%.0f km)", d/1e3, soi/1e3)
	}
	if d := w.ExitPosition(pass).Sub(bodyNow).Norm(); math.Abs(d-soi) > soi*0.05 {
		t.Errorf("ExitPosition %.0f km from the Moon's current position, want on the ring (%.0f km)", d/1e3, soi/1e3)
	}

	// And the drawn arc itself: first and last rebased draw samples land
	// on/near the ring, so the hyperbola visibly enters and exits on it.
	homeID := w.ActiveCraft().Primary.ID
	if len(pass.ArcSegments) == 0 {
		t.Fatal("pass has no arc segments")
	}
	firstSeg := w.SegmentDrawPoints(pass.ArcSegments[0], homeID)
	lastSeg := w.SegmentDrawPoints(pass.ArcSegments[len(pass.ArcSegments)-1], homeID)
	if len(firstSeg) == 0 || len(lastSeg) == 0 {
		t.Fatal("empty draw segments")
	}
	if d := firstSeg[0].Sub(bodyNow).Norm(); math.Abs(d-soi) > soi*0.05 {
		t.Errorf("arc's first drawn sample %.0f km from the Moon, want ring radius %.0f km", d/1e3, soi/1e3)
	}
	if d := lastSeg[len(lastSeg)-1].Sub(bodyNow).Norm(); math.Abs(d-soi) > soi*0.05 {
		t.Errorf("arc's last drawn sample %.0f km from the Moon, want ring radius %.0f km", d/1e3, soi/1e3)
	}
}

// TestPassEntryTimePrecedesPerilune: the pass carries the predicted
// SOI-entry clock for the chip (ADR 0021 C: the glyph marks where, the
// chip carries when) — strictly between now and the perilune.
func TestPassEntryTimePrecedesPerilune(t *testing.T) {
	w := mustWorld(t)
	moonCoast(t, w)

	pass, ok := w.LiveSOIPass()
	if !ok {
		t.Fatal("precondition: no live SOI Pass on the Moon coast")
	}
	if !pass.HasEntryTime {
		t.Fatal("pass carries no entry time")
	}
	if pass.TimeToEntry <= 0 {
		t.Errorf("TimeToEntry = %.0f s, want > 0 (entry is ahead)", pass.TimeToEntry)
	}
	if pass.TimeToEntry >= pass.TimeToPerilune {
		t.Errorf("TimeToEntry %.0f s ≥ TimeToPerilune %.0f s — entry must precede the perilune", pass.TimeToEntry, pass.TimeToPerilune)
	}
}

// TestPlannedPassEntryTimeRebasedToNow: the planned (node-modified) pass's
// entry clock is rebased from the leg's start to now, like TimeToPerilune —
// so the chip's T-entry counts down from the present even though the leg
// begins when its node fires, in the future.
func TestPlannedPassEntryTimeRebasedToNow(t *testing.T) {
	w := mustWorld(t)
	pass, _ := plantKernCursorPass(t, w)

	if !pass.HasEntryTime {
		t.Fatal("planned pass carries no entry time")
	}
	if pass.TimeToEntry <= 0 {
		t.Errorf("TimeToEntry = %.0f s, want > 0", pass.TimeToEntry)
	}
	if pass.TimeToEntry >= pass.TimeToPerilune {
		t.Errorf("TimeToEntry %.0f s ≥ TimeToPerilune %.0f s", pass.TimeToEntry, pass.TimeToPerilune)
	}
	// Rebased to now: the entry can't happen before the departure burn the
	// leg starts from.
	legs := w.PredictedLegs()
	if len(legs) == 0 {
		t.Fatal("no predicted legs")
	}
	if offset := legs[0].StartClock.Sub(w.Clock.SimTime).Seconds(); pass.TimeToEntry < offset {
		t.Errorf("TimeToEntry %.0f s precedes the departure burn at +%.0f s — not rebased to now", pass.TimeToEntry, offset)
	}
}

// TestBodySOIRadiusIsParentRelative pins the #143 fix: a moon's SOI radius
// comes from its parent planet, not the system root — SOIRadius(moon, sun)
// is the absurd Sun-relative value the physics docs warn about (a Luna at
// 384 000 km sized as if it orbited the Sun).
func TestBodySOIRadiusIsParentRelative(t *testing.T) {
	w := mustWorld(t)
	sys := w.System()
	_, moon := findMoon(t, w)
	earth := parentBody(w, moon)
	if earth.ID != moon.ParentID {
		t.Fatalf("test setup: Moon's parent resolved to %q", earth.ID)
	}

	got := w.BodySOIRadius(moon)
	wantParent := physics.SOIRadius(moon, earth)
	rootSized := physics.SOIRadius(moon, sys.Bodies[0])
	if math.Abs(got-wantParent) > 1 {
		t.Errorf("BodySOIRadius(moon) = %.0f km, want parent-relative %.0f km", got/1e3, wantParent/1e3)
	}
	if got <= rootSized*2 {
		t.Errorf("BodySOIRadius(moon) = %.0f km is root-sized (%.0f km) — #143 regressed", got/1e3, rootSized/1e3)
	}
	// The system root itself has no SOI.
	if r := w.BodySOIRadius(sys.Bodies[0]); r != 0 {
		t.Errorf("BodySOIRadius(root) = %g, want 0", r)
	}
}
