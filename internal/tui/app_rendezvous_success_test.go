package tui

import (
	"math"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// rotateAboutAxis rotates v about axis by angle radians (Rodrigues'
// formula). Test-only — mirrors internal/sim's own test helper of the
// same name, duplicated here since it's unexported in that package.
func rotateAboutAxis(v, axis orbital.Vec3, angle float64) orbital.Vec3 {
	axis = axis.Unit()
	cos, sin := math.Cos(angle), math.Sin(angle)
	return v.Scale(cos).Add(axis.Cross(v).Scale(sin)).Add(axis.Scale(axis.Dot(v) * (1 - cos)))
}

// TestRendezvousNudgeSuccessQuotesArrivalSpeed — ADR 0039 S1: every
// successful K plant quotes predicted arrival speed as a plain info row
// ("CA 9 km, arriving ~540 m/s" in the ADR's own example). #290 found
// this invisible at plan time — a K-plant that read as a clean success
// arrived at 95.4 m/s, 4.6 under the lock gate, with nothing on screen
// warning the player before they committed to it.
func TestRendezvousNudgeSuccessQuotesArrivalSpeed(t *testing.T) {
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := a.world.SpawnCraft(sim.SpawnSpec{AltitudeM: 600e3}); err != nil {
		t.Fatalf("SpawnCraft: %v", err)
	}
	active := a.world.Crafts[0]
	target := a.world.Crafts[1]
	// Small co-orbital lag — deterministic, always-plantable nudge
	// (mirrors the sim package's own rendezvousSmallLagWorld fixture).
	h := active.State.R.Cross(active.State.V)
	axis := h.Unit()
	angle := -0.5 * math.Pi / 180
	target.State.R = rotateAboutAxis(active.State.R, axis, angle)
	target.State.V = rotateAboutAxis(active.State.V, axis, angle)
	target.Primary = active.Primary
	a.world.ActiveCraftIdx = 0
	a.world.SetTargetCraft(1)

	a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'K'}})

	if a.statusMsg == "" {
		t.Fatal("K produced no status message")
	}
	if strings.Contains(a.statusMsg, "rendezvous: ") {
		// A refusal, not a plant — the fixed small-lag geometry is
		// documented (below) as deterministic and always-plantable, so a
		// refusal here means the geometry or planner tuning drifted, not
		// that this test's precondition is merely "not exercised" (PR
		// #392 review finding 3: a Skipf hedge here would report PASS
		// while covering nothing).
		t.Fatalf("K refused on the deterministic small-lag geometry (%q); expected a plant", a.statusMsg)
	}
	if !strings.Contains(a.statusMsg, "arriving ~") || !strings.Contains(a.statusMsg, "m/s") {
		t.Errorf("success status %q missing the arrival-speed info row", a.statusMsg)
	}
}
