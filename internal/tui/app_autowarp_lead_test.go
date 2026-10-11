package tui

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// Review #582 L3: on the pad with the launch window inside its 30 s lead,
// G cannot warp (too close), but it must not refuse with "no burn planned
// to warp to" either: it turns the heading to the window's and says so.
// Through the real key path.
func TestGInsideTheLaunchWindowLeadRetrimsTheHeading(t *testing.T) {
	a, c := padApp(t)
	a.world.SetTargetCraft(0)
	lw, ok := a.world.LaunchWindow()
	if !ok || !lw.Open {
		t.Fatalf("setup: want an open window, got ok=%v %+v", ok, lw)
	}
	if math.Abs(lw.HeadingDeg-90) < 5 {
		t.Fatalf("setup: window heading %.1f° too close to due east to tell a trim", lw.HeadingDeg)
	}
	// Inside the lead: 20 s before the pass. Heading is the default 090°.
	a.world.Clock.SimTime = lw.PassAt.Add(-20 * time.Second)
	c.HeadingTrim = 0
	if a.world.AutoWarpEligible() {
		t.Fatal("setup: Auto-Warp should have nothing to warp to inside the lead")
	}

	pressKey(a, 'G')

	gotDeg := (spacecraft.HeadingTrimDueEastRad + c.HeadingTrim) * 180 / math.Pi
	if want := math.Round(lw.HeadingDeg); math.Abs(gotDeg-want) > 1e-9 {
		t.Errorf("heading %.3f° after G inside the lead, want the window's %.0f°", gotDeg, want)
	}
	if a.world.AutoWarpEngaged() {
		t.Error("G inside the lead engaged a warp; it must only re-trim")
	}
	if a.statusMsg == "auto-warp: no burn planned to warp to" {
		t.Error("G inside the lead refused with the no-burn message")
	}
	if want := "for the launch window"; !strings.Contains(a.statusMsg, want) {
		t.Errorf("feedback %q does not say the heading was turned %q", a.statusMsg, want)
	}
}

// With no target there is no window at all, and G keeps refusing.
func TestGOffThePadStillRefusesWithNothingToWarpTo(t *testing.T) {
	a, _ := padApp(t) // no target: no window
	pressKey(a, 'G')
	if a.statusMsg != "auto-warp: no burn planned to warp to" {
		t.Errorf("statusMsg = %q, want the no-burn refusal", a.statusMsg)
	}
}
