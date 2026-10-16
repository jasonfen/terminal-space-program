package screens

import (
	"strings"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// moonTransferWorld plants the [H] Moon transfer on the inclined-LEO
// fixture; shortMps trims the departure so the predicted path misses.
func moonTransferWorld(t *testing.T, shortMps float64) *sim.World {
	t.Helper()
	w := inclinedCircularEarthOrbitCraft(t, 10, 500e3)
	for i, b := range w.System().Bodies {
		if b.ID == "moon" {
			if _, err := w.PlanTransfer(i); err != nil {
				t.Fatalf("PlanTransfer: %v", err)
			}
			w.ActiveCraft().Nodes[0].DV -= shortMps
			return w
		}
	}
	t.Fatal("no Moon in Sol")
	return nil
}

// hudMoonClaims returns the rendered HUD surfaces that name the Moon as
// an encounter: plan: row, FRAME TRANSITION box, CAPTURE PREVIEW box.
// (Text assertions only: lipgloss is colourless under go test, which is
// fine here because the claim is in the words.)
func hudMoonClaims(w *sim.World) (plan, frame, capture bool) {
	v := NewOrbitView(launchThemeForTest())
	plan = strings.Contains(v.buildNavigationBox(w)[7], "Moon encounter")
	frame = strings.Contains(strings.Join(v.buildFrameTransitionChip(w), "\n"), "Moon")
	capture = strings.Contains(strings.Join(v.buildCaptureChip(w), "\n"), "Moon")
	return
}

// #667 at the rendered HUD: a hit shows all three, a miss shows none, and
// the plan: row on a miss names the orbit the path really has.
func TestMissedMoonTransferHUDShowsNoEncounter(t *testing.T) {
	plan, frame, capture := hudMoonClaims(moonTransferWorld(t, 0))
	if !plan || !frame || !capture {
		t.Errorf("a real Moon transfer: plan=%v frame=%v capture=%v, want all three", plan, frame, capture)
	}

	w := moonTransferWorld(t, 100)
	plan, frame, capture = hudMoonClaims(w)
	if plan || frame || capture {
		t.Errorf("a missed Moon transfer still claims an encounter: plan=%v frame=%v capture=%v", plan, frame, capture)
	}
	row := NewOrbitView(launchThemeForTest()).buildNavigationBox(w)[7]
	if !strings.Contains(row, "Earth orbit") {
		t.Errorf("missed transfer plan row = %q, want the Earth orbit the path really has", row)
	}
}
