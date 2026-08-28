package tui

import (
	"math"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/tui/screens"
)

// lagGhostWorld places a ghost on the SAME circular orbit as the active
// craft, lagging by angleDeg degrees — mirrors the small-lag fixtures
// used elsewhere in this cycle (S1's arrival-speed test, the sim
// package's rendezvousSmallLagWorld), just with a Ghost target instead
// of a local sister craft so the SessionCmdRendezvous initiate path
// (which requires a ghost) has something to commit to. Larger |angleDeg|
// gives a larger natural (unburned) closest approach.
func lagGhostWorld(a *App, angleDeg float64) {
	active := a.world.ActiveCraft()
	h := active.State.R.Cross(active.State.V)
	axis := h.Unit()
	angle := angleDeg * math.Pi / 180
	relR := rotateAboutAxis(active.State.R, axis, angle)
	relV := rotateAboutAxis(active.State.V, axis, angle)
	a.world.Ghosts = []sim.Ghost{{
		Owner: "SHA256:gern", CraftID: 7, Handle: "gern", Name: "Aloft",
		PrimaryID: active.Primary.ID,
		Pos:       a.world.BodyPosition(active.Primary).Add(relR),
		RelPos:    relR,
		Vel:       relV,
	}}
}

// gapGhostBeside mirrors ghostBesideActive (app_rendezvous_test.go,
// #276's converging-geometry fixture) with a controllable lateral
// offset: relR carries both the 50 km closing-axis offset and a
// lateralOffsetM component the closing velocity never cancels, so the
// resulting current-course closest approach lands near lateralOffsetM
// itself rather than collapsing to ~0 on a head-on intercept. Lets the
// two gap-note tests below dial the committed CA to opposite sides of
// the far-CA bar (ADR 0039 S3, 3× the 35 km couple gate = 105 km)
// without relying on a matched-orbit/zero-drift fixture that
// RendezvousCommit (correctly, #276) always refuses with nothing
// planted — PR #392 review finding 3.
func gapGhostBeside(w *sim.World, owner string, craftID uint64, lateralOffsetM float64) sim.Ghost {
	c := w.ActiveCraft()
	relR := c.State.R.Add(orbital.Vec3{X: 50_000, Y: lateralOffsetM})
	closingVel := c.State.V.Add(orbital.Vec3{X: -50})
	return sim.Ghost{
		Owner:     owner,
		CraftID:   craftID,
		Handle:    "gern",
		Name:      "Aloft",
		PrimaryID: c.Primary.ID,
		Pos:       w.BodyPosition(c.Primary).Add(relR),
		RelPos:    relR,
		Vel:       closingVel,
	}
}

// TestEngageRendezvousFarCAAddsGapNote — ADR 0039 S3 / #281: Engage on a
// committed CA far above the 35 km lock gate adds one info line so
// riding waypoints indefinitely is a visible choice, not a mystery.
// #281's live case: initiator/target orbits phasing the wrong direction
// — the coast rode waypoint-to-waypoint for days with committed CA
// growing 4,400 km → 11,049 km and nothing on screen ever said a burn
// was needed. This checks the initiator's own Engage (SessionCmdRendezvous).
func TestEngageRendezvousFarCAAddsGapNote(t *testing.T) {
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if a.world.ActiveCraft() == nil {
		t.Fatal("no active craft in a fresh world")
	}
	// 150 km lateral offset — comfortably above the 105 km far-CA bar.
	a.world.Ghosts = []sim.Ghost{gapGhostBeside(a.world, "SHA256:gern", 7, 150_000)}

	model, _ := a.applySessionCommand(screens.SessionCommand{
		Kind: screens.SessionCmdRendezvous, Owner: "SHA256:gern", CraftID: 7, Handle: "gern",
	})
	app := model.(*App)

	if app.statusMsg == "" {
		t.Fatal("Engage produced no status message")
	}
	if strings.Contains(app.statusMsg, "plant a rendezvous nudge") {
		t.Fatalf("Engage refused on a genuinely converging current-course geometry (%q)", app.statusMsg)
	}
	if !strings.Contains(app.statusMsg, "a burn will be needed to close this") {
		t.Errorf("armed status %q missing the far-CA gap note", app.statusMsg)
	}
}

// TestEngageRendezvousCloseCANoGapNote — the mirror: a committed CA
// close to the couple gate must NOT carry the note (a normal near-miss
// the coast can plausibly narrow on its own, not the case where riding
// waypoints indefinitely is silently the wrong call).
func TestEngageRendezvousCloseCANoGapNote(t *testing.T) {
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// 60 km lateral offset — above the 35 km couple gate itself, but
	// well under the 105 km far-CA bar this note exists for.
	a.world.Ghosts = []sim.Ghost{gapGhostBeside(a.world, "SHA256:gern", 7, 60_000)}

	model, _ := a.applySessionCommand(screens.SessionCommand{
		Kind: screens.SessionCmdRendezvous, Owner: "SHA256:gern", CraftID: 7, Handle: "gern",
	})
	app := model.(*App)

	if app.statusMsg == "" {
		t.Fatal("Engage produced no status message")
	}
	if strings.Contains(app.statusMsg, "plant a rendezvous nudge") {
		t.Fatalf("Engage refused on a genuinely converging current-course geometry (%q)", app.statusMsg)
	}
	if strings.Contains(app.statusMsg, "a burn will be needed to close this") {
		t.Errorf("close-CA armed status wrongly carries the gap note: %q", app.statusMsg)
	}
}

// TestJoinRendezvousFarCAAddsGapNote — the responder's Engage (the `y`
// join path) must carry the same gap note as the initiator's, since it
// commits to the SAME τ/CA carried in the invite.
func TestJoinRendezvousFarCAAddsGapNote(t *testing.T) {
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	a.active = screenOrbit
	a.world.RendezvousInvite = &sim.RendezvousInvite{
		Owner: "SHA256:host", Handle: "fenbot",
		Tau: a.world.Clock.SimTime.Add(2 * time.Hour), CA: 500_000,
	}

	a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})

	if a.statusMsg == "" {
		t.Fatal("join produced no status message")
	}
	if !strings.Contains(a.statusMsg, "a burn will be needed to close this") {
		t.Errorf("join status %q missing the far-CA gap note", a.statusMsg)
	}
}
