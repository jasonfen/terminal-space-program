package screens

import (
	"strings"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// #295, arming side: the standing RENDEZVOUS chip is what the arming
// player looks at while they wait, so it carries the acting craft too —
// not just the partner's handle.
func TestRendezvousChipNamesTheActingCraft(t *testing.T) {
	v := NewOrbitView(plainTheme())
	v.Resize(200, 60)
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	w.RendezvousArm = &sim.RendezvousArm{
		TargetOwner: "SHA256:guest", Handle: "gern", CraftName: "Relay Tug-1",
		Tau: w.Clock.SimTime.Add(2 * time.Hour), CommittedCA: 577_000,
	}
	joined := strings.Join(v.buildRendezvousChip(w), "\n")
	if !strings.Contains(joined, "Relay Tug-1") {
		t.Errorf("armed chip does not name the acting craft:\n%s", joined)
	}
	if !strings.Contains(joined, "gern") {
		t.Errorf("armed chip stopped naming the partner:\n%s", joined)
	}
}

// #295, partner side: the join prompt read "jason wants to rendezvous"
// — the player, not the vessel. The partner's seat is where the live
// wrong-vessel arm was actually caught (from an implausible CA), so the
// craft name belongs on the prompt they answer.
func TestRendezvousInviteNamesTheInitiatorsCraft(t *testing.T) {
	v := NewOrbitView(plainTheme())
	v.Resize(200, 60)
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	w.RendezvousInvite = &sim.RendezvousInvite{
		Owner: "SHA256:host", Handle: "jason", CraftName: "Science Probe-3",
		Tau: w.Clock.SimTime.Add(2 * time.Hour), CA: 4_498_000,
	}
	joined := strings.Join(v.buildRendezvousChip(w), "\n")
	if !strings.Contains(joined, "Science Probe-3") {
		t.Errorf("join prompt does not name the initiator's craft:\n%s", joined)
	}
	if !strings.Contains(joined, "[y] join") {
		t.Errorf("join affordance vanished:\n%s", joined)
	}

	// A report with no craft name (older peer, or a craft that left the
	// set) must still render a usable prompt rather than an empty paren.
	w.RendezvousInvite.CraftName = ""
	bare := strings.Join(v.buildRendezvousChip(w), "\n")
	if strings.Contains(bare, "()") {
		t.Errorf("nameless invite rendered an empty parenthetical:\n%s", bare)
	}
	if !strings.Contains(bare, "jason wants to rendezvous") {
		t.Errorf("nameless invite lost its attribution:\n%s", bare)
	}
}
