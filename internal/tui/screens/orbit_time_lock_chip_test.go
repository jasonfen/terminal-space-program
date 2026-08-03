package screens

import (
	"strings"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// ADR 0037 §3: a lock on the player's warp is always explained on screen.
// The terminal phase gets the full RENDEZVOUS approach block — who you're
// flying with, your seat, the pair's rate, and what is holding it when it
// isn't you. That last row is the answer to "why do my warp keys do
// nothing", which #305 spent 30 minutes not having.

func approachChipWorld(t *testing.T, seat sim.RendezvousSeat) *sim.World {
	t.Helper()
	w := rendezvousChipWorld(t)
	// τ is in the PAST once the handoff has happened; Engage is
	// forward-only, so arm forward and demote, exactly as the drive does.
	w.EngageRendezvousWarpAs("SHA256:guest", "gern", w.Clock.SimTime.Add(time.Hour), 6000,
		seat == sim.RendezvousSeatPilot)
	w.RendezvousArm.Approach = true
	w.RendezvousRate = sim.RendezvousRateState{Seat: seat, Handle: "gern"}
	return w
}

func TestRendezvousChipApproachPhasePilot(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	w := approachChipWorld(t, sim.RendezvousSeatPilot)
	joined := strings.Join(v.buildRendezvousChip(w), "\n")
	for _, want := range []string{"RENDEZVOUS", "approach with gern", "pilot", "rate:", "[/] cancel"} {
		if !strings.Contains(joined, want) {
			t.Errorf("approach chip missing %q:\n%s", want, joined)
		}
	}
	// Nothing is holding the pair but the pilot, so no "held" row.
	if strings.Contains(joined, "held:") {
		t.Errorf("pilot with a free clock got a held row:\n%s", joined)
	}
	// It is an approach, not a coast: no τ countdown to a passed waypoint.
	if strings.Contains(joined, "τ in:") {
		t.Errorf("approach chip still counts down to a passed τ:\n%s", joined)
	}
}

func TestRendezvousChipApproachPhaseCopilotNamesTheHolder(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	w := approachChipWorld(t, sim.RendezvousSeatCopilot)
	w.RendezvousRate.PartnerRate = 1000

	joined := strings.Join(v.buildRendezvousChip(w), "\n")
	for _, want := range []string{"copilot", "held:", "gern"} {
		if !strings.Contains(joined, want) {
			t.Errorf("copilot approach chip missing %q:\n%s", want, joined)
		}
	}

	// A burning partner reads as a burn, not as somebody braking.
	w.RendezvousRate.PartnerBurning = true
	w.RendezvousRate.PartnerRate = 10
	if joined := strings.Join(v.buildRendezvousChip(w), "\n"); !strings.Contains(joined, "burning") {
		t.Errorf("burn hold not named:\n%s", joined)
	}
	// A braking copilot, read from the pilot's seat.
	p := approachChipWorld(t, sim.RendezvousSeatPilot)
	p.Clock.WarpIdx = 5
	p.RendezvousRate.PartnerRate = 10
	if joined := strings.Join(v.buildRendezvousChip(p), "\n"); !strings.Contains(joined, "braking") {
		t.Errorf("copilot brake not named from the pilot's seat:\n%s", joined)
	}
}

// The standing away + hold lines carry over into the terminal phase — an
// away partner lasts hours, and the 6 s went-quiet moment can't hold it.
func TestRendezvousChipApproachPhaseKeepsStandingLines(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	w := approachChipWorld(t, sim.RendezvousSeatPilot)
	w.RendezvousPartnerAway = true
	w.RendezvousHold = true
	joined := strings.Join(v.buildRendezvousChip(w), "\n")
	for _, want := range []string{"is away", "holding"} {
		if !strings.Contains(joined, want) {
			t.Errorf("approach chip dropped the standing %q line:\n%s", want, joined)
		}
	}
}

// ADR 0037 §3, second half: a plain proximity lock — no agreement, so no
// seats — gets a minimal standing TIME LOCK line (who + rate). #305's
// 30m12s clamp was announced by exactly one 6-second chip.
func TestTimeLockChipForPlainProximityLock(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	w := rendezvousChipWorld(t)
	if chip := v.buildTimeLockChip(w); chip != nil {
		t.Errorf("TIME LOCK rendered while uncoupled:\n%s", strings.Join(chip, "\n"))
	}

	w.CoWarp = sim.CoWarpState{Coupled: true, MinWarp: 10, Partners: []string{"gern"}}
	joined := strings.Join(v.buildTimeLockChip(w), "\n")
	for _, want := range []string{"TIME LOCK", "gern"} {
		if !strings.Contains(joined, want) {
			t.Errorf("TIME LOCK line missing %q:\n%s", want, joined)
		}
	}
	if !strings.Contains(joined, WarpLabel(w.EffectiveWarp())) {
		t.Errorf("TIME LOCK line does not state the rate:\n%s", joined)
	}

	// Inside an agreement the RENDEZVOUS chip carries all of this with far
	// more context — two blocks saying "your warp is locked" is clutter.
	w.EngageRendezvousWarpAs("SHA256:guest", "gern", w.Clock.SimTime.Add(time.Hour), 6000, true)
	w.RendezvousArm.Approach = true
	if chip := v.buildTimeLockChip(w); chip != nil {
		t.Errorf("TIME LOCK duplicated the RENDEZVOUS chip inside an agreement:\n%s", strings.Join(chip, "\n"))
	}
}

// ADR 0037 §3 review: the old `RendezvousArm != nil` gate suppressed the
// TIME LOCK line for ANY arm, including armed-waiting — no reciprocal arm
// yet, no coast, nothing on the RENDEZVOUS chip naming a rate. A
// coincidental proximity couple with a third player in that state left
// nothing on screen explaining the lock at all.
func TestTimeLockChipShowsWhileArmedWaiting(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	w := rendezvousChipWorld(t)
	w.CoWarp = sim.CoWarpState{Coupled: true, MinWarp: 10, Partners: []string{"gern"}}
	w.EngageRendezvousWarpAs("SHA256:guest", "gern", w.Clock.SimTime.Add(time.Hour), 6000, true)
	// Deliberately NOT engaging the coast and NOT setting Approach — this
	// is the armed-waiting sub-state (partner hasn't reciprocated yet).

	chip := v.buildTimeLockChip(w)
	if chip == nil {
		t.Fatalf("TIME LOCK suppressed while merely armed-waiting")
	}
	joined := strings.Join(chip, "\n")
	for _, want := range []string{"TIME LOCK", "gern"} {
		if !strings.Contains(joined, want) {
			t.Errorf("TIME LOCK line missing %q:\n%s", want, joined)
		}
	}
}

// ADR 0037 §3 review: an arm toward player A must not blank out a lock the
// viewer is actually holding with an unrelated player B — RENDEZVOUS only
// ever narrates the arm's own target, never B.
func TestTimeLockChipShowsWhenCoupledToADifferentPlayerThanTheArm(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	w := rendezvousChipWorld(t)
	w.CoWarp = sim.CoWarpState{Coupled: true, MinWarp: 10, Partners: []string{"berta"}}
	w.EngageRendezvousWarpAs("SHA256:guest", "gern", w.Clock.SimTime.Add(time.Hour), 6000, true)
	w.RendezvousArm.Approach = true

	chip := v.buildTimeLockChip(w)
	if chip == nil {
		t.Fatalf("TIME LOCK suppressed while coupled to a player the arm doesn't name")
	}
	joined := strings.Join(chip, "\n")
	for _, want := range []string{"TIME LOCK", "berta"} {
		if !strings.Contains(joined, want) {
			t.Errorf("TIME LOCK line missing %q:\n%s", want, joined)
		}
	}
}

// #280 / ADR 0037 §4: the SESSION chip gets CHAT's depth cap so any burst
// stays bounded. Before it, every moment inside the 6 s TTL rendered —
// 852 coupled/released moments in one live session grew a block tall
// enough to occlude the flight view.
func TestSessionEventsChipCapsDepth(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	w := rendezvousChipWorld(t)
	now := time.Now()
	for i := 0; i < sessionEventChipDepth*3; i++ {
		w.SessionEvents = append(w.SessionEvents, sim.SessionEvent{
			Kind: sim.SessionEventJoin, Handle: "p" + string(rune('a'+i)), At: now,
		})
	}
	lines := v.buildSessionEventsChip(w)
	if got := len(lines) - 1; got != sessionEventChipDepth { // minus the header
		t.Errorf("rendered %d rows, want the cap of %d", got, sessionEventChipDepth)
	}
	// The tail survives, not the head: the newest moments are the ones
	// worth the space.
	joined := strings.Join(lines, "\n")
	if strings.Contains(joined, "pa joined") {
		t.Errorf("oldest moment survived the cap:\n%s", joined)
	}
	if !strings.Contains(joined, "p"+string(rune('a'+sessionEventChipDepth*3-1))+" joined") {
		t.Errorf("newest moment dropped by the cap:\n%s", joined)
	}
}
