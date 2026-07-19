package screens

import (
	"strings"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// The RENDEZVOUS chip (v0.29 S2) is the persistent main-screen surface
// of the Rendezvous Warp state machine: the join prompt while a partner
// is armed toward the viewer, the armed-waiting readout after
// initiating, and the coasting readout (committed CA + live approach +
// degrade warning) once the shared coast runs. buildRendezvousChip
// reads the World slate directly; states are exercised through it.

func rendezvousChipWorld(t *testing.T) *sim.World {
	t.Helper()
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	return w
}

func TestRendezvousChipHiddenWhenIdle(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	w := rendezvousChipWorld(t)
	if chip := v.buildRendezvousChip(w); chip != nil {
		t.Errorf("chip rendered with no rendezvous state:\n%s", strings.Join(chip, "\n"))
	}
}

func TestRendezvousChipInvitePrompt(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	w := rendezvousChipWorld(t)
	w.RendezvousInvite = &sim.RendezvousInvite{
		Owner: "SHA256:guest", Handle: "gern",
		Tau: w.Clock.SimTime.Add(2 * time.Hour), CA: 900,
	}
	joined := strings.Join(v.buildRendezvousChip(w), "\n")
	for _, want := range []string{"RENDEZVOUS", "gern wants to rendezvous", "[y] join", "2h0m", "900 m"} {
		if !strings.Contains(joined, want) {
			t.Errorf("invite prompt missing %q:\n%s", want, joined)
		}
	}
}

func TestRendezvousChipArmedWaiting(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	w := rendezvousChipWorld(t)
	w.EngageRendezvousWarp("SHA256:guest", "gern", w.Clock.SimTime.Add(2*time.Hour), 900)
	w.Session = &sim.SessionInfo{Players: []sim.SessionPlayer{
		{Fingerprint: "SHA256:guest", Handle: "gern"},
	}}
	joined := strings.Join(v.buildRendezvousChip(w), "\n")
	for _, want := range []string{"RENDEZVOUS", "gern", "waiting", "[/] cancel"} {
		if !strings.Contains(joined, want) {
			t.Errorf("armed-waiting chip missing %q:\n%s", want, joined)
		}
	}
}

func TestRendezvousChipCoastingAndDegraded(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	w := rendezvousChipWorld(t)
	tau := w.Clock.SimTime.Add(2 * time.Hour)
	w.EngageRendezvousWarp("SHA256:guest", "gern", tau, 900)
	w.AutoWarp = &sim.AutoWarpTarget{
		T: tau, Rendezvous: true,
		RendezvousOwner: "SHA256:guest", RendezvousHandle: "gern",
	}
	w.RendezvousApproachM = 1200

	joined := strings.Join(v.buildRendezvousChip(w), "\n")
	for _, want := range []string{"RENDEZVOUS", "gern", "committed", "900 m", "1.20 km", "[/] cancel"} {
		if !strings.Contains(joined, want) {
			t.Errorf("coasting chip missing %q:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "degraded") {
		t.Errorf("degrade warning shown while the encounter holds:\n%s", joined)
	}

	w.RendezvousDegraded = true
	w.RendezvousApproachM = 15_000
	joined = strings.Join(v.buildRendezvousChip(w), "\n")
	if !strings.Contains(joined, "degraded") {
		t.Errorf("no degrade warning after the encounter slipped:\n%s", joined)
	}

	// Hold-the-leader (v0.29 review): the freeze is surfaced, not silent.
	w.RendezvousHold = true
	joined = strings.Join(v.buildRendezvousChip(w), "\n")
	if !strings.Contains(joined, "holding — waiting for gern") {
		t.Errorf("hold state not surfaced on the chip:\n%s", joined)
	}
}

// The SESSION moments chip renders the four new rendezvous kinds
// (v0.29 S2) alongside the existing vocabulary.
func TestSessionEventsChipRendezvousKinds(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	w := rendezvousChipWorld(t)
	now := time.Now()
	w.SessionEvents = []sim.SessionEvent{
		{Kind: sim.SessionEventRendezvousArmed, Handle: "gern", At: now},
		{Kind: sim.SessionEventRendezvousArrived, Handle: "gern", At: now},
		{Kind: sim.SessionEventRendezvousCancelled, Handle: "gern", At: now},
		{Kind: sim.SessionEventRendezvousDegraded, Handle: "gern", At: now},
	}
	joined := strings.Join(v.buildSessionEventsChip(w), "\n")
	for _, want := range []string{
		"gern wants to rendezvous",
		"rendezvous: encounter reached",
		"rendezvous with gern cancelled",
		"rendezvous encounter degraded",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("session chip missing %q:\n%s", want, joined)
		}
	}
}
