package sim

import (
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
)

// TestEncounterFramingCentersOnArrivalPosition is the regression for #144: a
// planted Kern→Cursor transfer draws its capture geometry at Cursor's *arrival*
// position (where Cursor will be when the craft gets there), but the framing
// paths used to center on Cursor's *current* position. For a short-period moon
// the two diverge by far more than the SOI, leaving the predicted capture curve
// off-canvas — "focus on Cursor shows nothing until the maneuvers finish."
//
// All three encounter framers — FocusEncounterFraming (plain focus),
// TargetViewFraming (ViewTarget), SOIPassViewFraming (ViewSOIPass) — must center
// on the encounter (≈ the arrival position), not the current body position.
func TestEncounterFramingCentersOnArrivalPosition(t *testing.T) {
	w := mustWorld(t)
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
	if !pass.HasPerilunePt {
		t.Fatal("pass has no perilune point to frame")
	}

	now := w.Clock.SimTime
	arrival := w.BodyPositionAt(cursor, now.Add(time.Duration(pass.TimeToPerilune*float64(time.Second))))
	current := w.BodyPosition(cursor)
	soi := physics.SOIRadius(cursor, kern)

	// Premise check: the bug only bites because Cursor moves much farther than
	// its SOI during the transfer. If this ever fails the repro has gone stale.
	if gap := arrival.Sub(current).Norm(); gap < 10*soi {
		t.Fatalf("Cursor moved only %.0f km (SOI %.0f km) — repro premise gone", gap/1e3, soi/1e3)
	}

	// Every framer must land within the SOI of the arrival position (where the
	// capture curve is drawn) and nowhere near the current position.
	check := func(name string, center orbital.Vec3, ok bool) {
		if !ok {
			t.Errorf("%s returned ok=false; expected an encounter frame", name)
			return
		}
		if d := center.Sub(arrival).Norm(); d > soi {
			t.Errorf("%s centered %.0f km from Cursor's arrival position (SOI %.0f km) — encounter off-canvas", name, d/1e3, soi/1e3)
		}
		if d := center.Sub(current).Norm(); d < 10*soi {
			t.Errorf("%s centered only %.0f km from Cursor's CURRENT position — the #144 bug", name, d/1e3)
		}
	}

	w.Focus = Focus{Kind: FocusBody, BodyIdx: cursorIdx}
	fc, _, fok := w.FocusEncounterFraming()
	check("FocusEncounterFraming", fc, fok)

	w.SetTargetBody(cursorIdx)
	tc, _, tok := w.TargetViewFraming()
	check("TargetViewFraming", tc, tok)

	sc, _, sok := w.SOIPassViewFraming()
	check("SOIPassViewFraming", sc, sok)
}

// TestViewSOIPassSelectableDuringPlantedTransfer pins the second compounding
// gap in #144: ViewSOIPass used to gate on LiveSOIPass, which is false while
// flying a *planted* transfer — the pre-burn orbit can't reach the moon, so the
// soi-pass view was unavailable in exactly the case it's most useful. After the
// fix the `v` cycle reaches it whenever any pass exists (planted or live).
func TestViewSOIPassSelectableDuringPlantedTransfer(t *testing.T) {
	w := mustWorld(t)
	cursorIdx, _, _ := setupKernCursor(t, w)
	if _, err := w.PlanTransfer(cursorIdx); err != nil {
		t.Fatalf("PlanTransfer(Cursor): %v", err)
	}

	// The unburned parking orbit can't reach Cursor — the gap the old gate hit.
	if _, ok := w.LiveSOIPass(); ok {
		t.Skip("pre-burn parking orbit already reaches Cursor — can't exercise the planted-only gate")
	}
	if _, ok := w.PlannedSOIPass(); !ok {
		t.Fatal("planted transfer produced no PlannedSOIPass to frame")
	}
	if !w.viewModeSelectable(ViewSOIPass) {
		t.Error("ViewSOIPass unselectable during a planted transfer — the #144 second gap")
	}

	reached := false
	w.ViewMode = ViewTilted
	for range AllViewModes {
		w.CycleViewMode()
		if w.ViewMode == ViewSOIPass {
			reached = true
			break
		}
	}
	if !reached {
		t.Error("cycling v never reached ViewSOIPass during a planted transfer")
	}
}
