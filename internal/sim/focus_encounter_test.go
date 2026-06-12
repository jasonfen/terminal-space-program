package sim

import (
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
)

// TestEncounterFramingCentersOnLocalArc: a planted Kern→Cursor transfer draws
// its capture geometry Local-to-Body — body-relative samples anchored at
// Cursor's CURRENT position (ADR 0021 B) — so the framing paths must center
// on that drawn ink, next to Cursor's disk. (Pre-ADR-0021 the arc drew at its
// heliocentric sample positions near Cursor's *arrival* point and the framers
// chased it there — the #144 fix this supersedes.)
//
// All three encounter framers — FocusEncounterFraming (plain focus),
// TargetViewFraming (ViewTarget), SOIPassViewFraming (ViewSOIPass) — must
// center within the SOI of Cursor's current position.
func TestEncounterFramingCentersOnLocalArc(t *testing.T) {
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

	// Premise check: the rebase only matters because Cursor moves much farther
	// than its SOI during the transfer. If this ever fails the repro is stale.
	if gap := arrival.Sub(current).Norm(); gap < 10*soi {
		t.Fatalf("Cursor moved only %.0f km (SOI %.0f km) — repro premise gone", gap/1e3, soi/1e3)
	}

	// Every framer must land within the SOI of Cursor's CURRENT position —
	// where the Local-to-Body arc and the body's disk are drawn.
	check := func(name string, center orbital.Vec3, ok bool) {
		if !ok {
			t.Errorf("%s returned ok=false; expected an encounter frame", name)
			return
		}
		if d := center.Sub(current).Norm(); d > soi {
			t.Errorf("%s centered %.0f km from Cursor's current position (SOI %.0f km) — not framing the Local-to-Body arc", name, d/1e3, soi/1e3)
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
