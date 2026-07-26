package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/tui/screens"
)

// ghostBesideActive plants a ghost 50 km from the active craft in the
// same primary, resolvable by the target/rendezvous machinery.
func ghostBesideActive(w *sim.World, owner string, craftID uint64) sim.Ghost {
	c := w.ActiveCraft()
	relR := c.State.R.Add(orbital.Vec3{X: 50_000})
	return sim.Ghost{
		Owner:     owner,
		CraftID:   craftID,
		Handle:    "gern",
		Name:      "Gernaut",
		PrimaryID: c.Primary.ID,
		Pos:       w.BodyPosition(c.Primary).Add(relR),
		RelPos:    relR,
		Vel:       c.State.V,
	}
}

// SessionCmdRendezvous (v0.29 S2): targets the partner's ghost, commits
// an encounter, and arms — the App-side initiate path.
func TestSessionRendezvousCommandArms(t *testing.T) {
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	w := a.world
	w.Ghosts = []sim.Ghost{ghostBesideActive(w, "SHA256:guest", 42)}

	a.applySessionCommand(screens.SessionCommand{
		Kind: screens.SessionCmdRendezvous, Owner: "SHA256:guest", CraftID: 42, Handle: "gern",
	})
	if w.Target.Kind != sim.TargetGhost || w.Target.GhostOwner != "SHA256:guest" {
		t.Errorf("target after rendezvous command = %+v, want gern's ghost", w.Target)
	}
	if w.RendezvousArm == nil || w.RendezvousArm.TargetOwner != "SHA256:guest" {
		t.Fatalf("arm after rendezvous command = %+v, want toward SHA256:guest", w.RendezvousArm)
	}
	if !w.RendezvousArm.Tau.After(w.Clock.SimTime) {
		t.Errorf("committed τ %v not in the future", w.RendezvousArm.Tau)
	}
	// Arming alone never starts the coast (waits for the partner).
	if w.AutoWarp != nil {
		t.Error("Auto-Warp started on initiate — must wait for mutual arm")
	}
}

// The main-screen respond key (v0.29 S2): [y] with a pending invite
// adopts the initiator's τ+CA verbatim; without an invite `y` is inert.
func TestRendezvousRespondKey(t *testing.T) {
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	w := a.world

	// No invite: y is a free key — nothing arms.
	a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	if w.RendezvousArm != nil {
		t.Fatal("y with no invite armed a rendezvous")
	}

	tau := w.Clock.SimTime.Add(3 * time.Hour)
	w.RendezvousInvite = &sim.RendezvousInvite{
		Owner: "SHA256:guest", Handle: "gern", Tau: tau, CA: 900,
	}
	a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	arm := w.RendezvousArm
	if arm == nil || arm.TargetOwner != "SHA256:guest" || !arm.Tau.Equal(tau) || arm.CommittedCA != 900 {
		t.Fatalf("arm after respond = %+v, want gern's committed τ+CA adopted verbatim", arm)
	}
	if !strings.Contains(a.statusMsg, "gern") {
		t.Errorf("status %q does not name the partner", a.statusMsg)
	}
}

// CancelWarp `/` extends to Rendezvous Warp (v0.29 S2): it releases the
// arm and the shared coast, not just the Auto-Warp driver — otherwise
// DriveRendezvousWarp would restart the coast next tick.
func TestCancelWarpCancelsRendezvous(t *testing.T) {
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	w := a.world
	tau := w.Clock.SimTime.Add(3 * time.Hour)
	w.EngageRendezvousWarp("SHA256:guest", "gern", tau, 0)
	w.AutoWarp = &sim.AutoWarpTarget{T: tau, Rendezvous: true, RendezvousOwner: "SHA256:guest", RendezvousHandle: "gern"}

	a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	if w.RendezvousArm != nil {
		t.Error("/ left the rendezvous arm set (coast would restart next tick)")
	}
	if w.AutoWarp != nil {
		t.Error("/ left the Auto-Warp engaged")
	}
}

// Warp keys during an engaged rendezvous coast (#249): `.`/`,` refuse —
// arm intact, coast intact, Selected Warp untouched — and surface a
// toast pointing at `/`, the one deliberate cancel gesture. Before the
// fix a single tap silently cancelled the rendezvous for both players.
func TestWarpKeysInertDuringRendezvousCoast(t *testing.T) {
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	w := a.world
	tau := w.Clock.SimTime.Add(3 * time.Hour)
	w.EngageRendezvousWarp("SHA256:guest", "gern", tau, 0)
	w.AutoWarp = &sim.AutoWarpTarget{T: tau, Rendezvous: true, RendezvousOwner: "SHA256:guest", RendezvousHandle: "gern"}
	idx := w.Clock.WarpIdx

	for _, r := range []rune{'.', ','} {
		a.statusMsg = ""
		a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		if w.RendezvousArm == nil {
			t.Fatalf("%q cleared the rendezvous arm", r)
		}
		if w.AutoWarp == nil || !w.AutoWarp.Rendezvous {
			t.Fatalf("%q disengaged the rendezvous coast (AutoWarp=%+v)", r, w.AutoWarp)
		}
		if w.Clock.WarpIdx != idx {
			t.Errorf("%q moved Selected Warp: WarpIdx %d, want %d", r, w.Clock.WarpIdx, idx)
		}
		if !strings.Contains(a.statusMsg, "/") {
			t.Errorf("%q gave no feedback pointing at [/]: status %q", r, a.statusMsg)
		}
	}
}

// The refusal is scoped to the rendezvous coast: a plain node-chase
// Auto-Warp — and a Sync — keep today's behavior, where a manual warp
// key cancels the driver and then applies the step (ADR 0016).
func TestWarpKeysStillCancelPlainAutoWarp(t *testing.T) {
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	w := a.world

	// Node-chase: `.` cancels the driver and warps up.
	w.AutoWarp = &sim.AutoWarpTarget{T: w.Clock.SimTime.Add(time.Hour)}
	idx := w.Clock.WarpIdx
	a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'.'}})
	if w.AutoWarp != nil {
		t.Error(". left a node-chase Auto-Warp engaged")
	}
	if w.Clock.WarpIdx != idx+1 {
		t.Errorf(". did not warp up: WarpIdx %d, want %d", w.Clock.WarpIdx, idx+1)
	}

	// Sync: `,` cancels the driver and warps down.
	w.AutoWarp = &sim.AutoWarpTarget{T: w.Clock.SimTime.Add(time.Hour), Sync: true, SyncHandle: "gern", SyncOwner: "SHA256:guest"}
	a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{','}})
	if w.AutoWarp != nil {
		t.Error(", left a Sync Auto-Warp engaged")
	}
	if w.Clock.WarpIdx != idx {
		t.Errorf(", did not warp down: WarpIdx %d, want %d", w.Clock.WarpIdx, idx)
	}
}
