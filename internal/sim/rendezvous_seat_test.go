package sim

import (
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
)

// seatedPair puts the world in the terminal phase with the seats
// resolved: the viewer holds `initiator`, the partner holds the other
// seat and publishes rate as their contribution to the pair's clock.
func seatedPair(t *testing.T, initiator bool, partnerRate float64) (*World, CoWarpPeer) {
	t.Helper()
	w, near := armedApproachWorld(t, initiator)
	near.RendezvousInitiator = !initiator
	near.RendezvousRate = partnerRate
	w.DriveRendezvousWarp([]CoWarpPeer{near})
	return w, near
}

// The initiator flies the clock (ADR 0037 §2): their own selected warp is
// the pair's rate, with a following copilot imposing nothing.
func TestInitiatorSelectionSetsPairRate(t *testing.T) {
	w, _ := seatedPair(t, true, 0) // copilot following — no ceiling published
	if got := w.RendezvousRate.Seat; got != RendezvousSeatPilot {
		t.Fatalf("seat = %v, want pilot", got)
	}
	w.Clock.WarpIdx = 3 // 1000×
	if got := w.EffectiveWarp(); got != 1000 {
		t.Errorf("effective warp = %v, want the initiator's own 1000×", got)
	}
	if h := w.RendezvousRateHold(); h != RendezvousRateYou {
		t.Errorf("holder = %v, want you (nothing else is constraining the pair)", h)
	}
}

// The copilot's keys default to FOLLOWING (ADR 0037 §2): their own 1×
// selection is not the pair's rate — the initiator's relayed selection
// is. This is the #302 confusion inverted: warp keys that did nothing.
func TestCopilotFollowsTheInitiatorsClock(t *testing.T) {
	w, _ := seatedPair(t, false, 1000)
	if got := w.RendezvousRate.Seat; got != RendezvousSeatCopilot {
		t.Fatalf("seat = %v, want copilot", got)
	}
	if w.Clock.WarpIdx != 0 {
		t.Fatal("precondition: copilot sitting at their own 1× selection")
	}
	if got := w.EffectiveWarp(); got != 1000 {
		t.Errorf("effective warp = %v, want the initiator's 1000× — the copilot follows", got)
	}
	if h := w.RendezvousRateHold(); h != RendezvousRateFollowing {
		t.Errorf("holder = %v, want following", h)
	}
}

// A lower selection wins downward: the copilot may brake the pair, and
// the brake is what their own clock runs at too.
func TestCopilotBrakeSlowsThePair(t *testing.T) {
	w, _ := seatedPair(t, false, 1000)
	brake, ok := w.StepRendezvousBrake(false)
	if !ok {
		t.Fatal("copilot could not brake the pair")
	}
	if brake <= 0 || brake >= 1000 {
		t.Fatalf("brake = %v, want a rung below the initiator's 1000×", brake)
	}
	if got := w.RendezvousSeatRate(); got != brake {
		t.Errorf("published seat rate = %v, want the brake %v — this is what reaches the initiator", got, brake)
	}
	if got := w.EffectiveWarp(); got != brake {
		t.Errorf("effective warp = %v, want the brake %v", got, brake)
	}
}

// ...and the copilot can never push the pair faster: from FOLLOWING there
// is nothing above to select, and a brake released past the top rung
// returns to following rather than overriding the initiator.
func TestCopilotCannotPushThePairFaster(t *testing.T) {
	w, _ := seatedPair(t, false, 10)
	if _, ok := w.StepRendezvousBrake(true); ok {
		t.Error("copilot pushed the pair up from the following seat")
	}
	if got := w.EffectiveWarp(); got != 10 {
		t.Errorf("effective warp = %v, want the initiator's 10×", got)
	}
	// Brake all the way to the floor, then release back up to following.
	for i := 0; i < len(WarpFactors); i++ {
		w.StepRendezvousBrake(false)
	}
	if w.RendezvousArm.BrakeIdx != 0 {
		t.Fatalf("brake floor = %d, want the 1× rung", w.RendezvousArm.BrakeIdx)
	}
	for i := 0; i < len(WarpFactors); i++ {
		w.StepRendezvousBrake(true)
	}
	if w.RendezvousArm.BrakeIdx != rendezvousFollowing {
		t.Errorf("releasing the brake past the top rung left BrakeIdx = %d, want following", w.RendezvousArm.BrakeIdx)
	}
}

// The initiator is braked by the copilot's selection — "a lower selection
// wins downward" is symmetric in effect, asymmetric in authority.
func TestCopilotBrakeReachesTheInitiator(t *testing.T) {
	w, _ := seatedPair(t, true, 10) // copilot has braked to 10×
	w.Clock.WarpIdx = 5             // the pilot wants 100000×
	if got := w.EffectiveWarp(); got != 10 {
		t.Errorf("effective warp = %v, want the copilot's 10× brake", got)
	}
	if h := w.RendezvousRateHold(); h != RendezvousRatePartnerBraking {
		t.Errorf("holder = %v, want partner-braking — the chip must say why the keys do nothing", h)
	}
}

// Either side's active burn holds the pair at the burn cap (ADR 0037 §2).
func TestPartnerBurnHoldsThePairAtTheBurnCap(t *testing.T) {
	w, near := seatedPair(t, true, 0)
	near.RendezvousRate, near.RendezvousBurning = burnWarpCap, true
	w.DriveRendezvousWarp([]CoWarpPeer{near})
	w.Clock.WarpIdx = 5
	if got := w.EffectiveWarp(); got != burnWarpCap {
		t.Errorf("effective warp = %v, want the partner's burn cap %v", got, burnWarpCap)
	}
	if h := w.RendezvousRateHold(); h != RendezvousRatePartnerBurning {
		t.Errorf("holder = %v, want partner-burning", h)
	}
	// The viewer's own burn is published the same way, so it reaches them.
	near.RendezvousRate, near.RendezvousBurning = 0, false
	w.DriveRendezvousWarp([]CoWarpPeer{near})
	w.StartManualBurn()
	if got := w.RendezvousSeatRate(); got != burnWarpCap {
		t.Errorf("published seat rate under our own burn = %v, want %v", got, burnWarpCap)
	}
}

// A paused partner crawls the pair rather than letting the other side
// max-seed away from them: pause is the deepest brake in either seat.
func TestPausedPartnerCrawlsThePair(t *testing.T) {
	w, near := seatedPair(t, false, 1000)
	near.Paused, near.RendezvousRate = true, 0
	near.SubspaceTime = w.Clock.SimTime.Add(time.Second) // viewer behind: no leader hold
	w.DriveRendezvousWarp([]CoWarpPeer{near})
	if got := w.EffectiveWarp(); got != 1 {
		t.Errorf("effective warp = %v, want 1× behind a paused partner", got)
	}
	if h := w.RendezvousRateHold(); h != RendezvousRatePartnerPaused {
		t.Errorf("holder = %v, want partner-paused", h)
	}
}

// Seats must be unambiguous. Two initiators (crossed invites) or none (an
// older peer) fall back to today's symmetric min-wins rather than one side
// silently assuming authority.
func TestAmbiguousSeatsFallBackToMinWins(t *testing.T) {
	w, near := seatedPair(t, true, 1000)
	near.RendezvousInitiator = true // both claim the seat
	w.DriveRendezvousWarp([]CoWarpPeer{near})
	if got := w.RendezvousRate.Seat; got != RendezvousSeatNone {
		t.Errorf("seat = %v, want none when both sides claim the initiator seat", got)
	}
	near.EffWarp = 10
	res := w.ComputeCoWarp([]CoWarpPeer{near}, map[string]bool{near.Owner: true})
	if res.State.MinWarp != 10 {
		t.Errorf("MinWarp = %v, want the partner's reported 10× — min-wins is the fallback", res.State.MinWarp)
	}
}

// With the seats resolved the partner's relayed EffWarp must NOT feed the
// co-warp min: that is the #248 ratchet, and it would pin a pair at 1×
// through exactly the phase this ADR frees up.
func TestSeatedPairIsExemptFromTheStaleReportRatchet(t *testing.T) {
	w, near := seatedPair(t, true, 0)
	near.EffWarp = 1 // their post-clamp report, a tick stale
	res := w.ComputeCoWarp([]CoWarpPeer{near}, map[string]bool{near.Owner: true})
	if !res.State.Coupled {
		t.Fatal("seated pair uncoupled")
	}
	if res.State.MinWarp != 0 {
		t.Errorf("MinWarp = %v, want 0 (no min-wins) — the seats set the rate", res.State.MinWarp)
	}
}

// The brake is meaningless in the pilot's seat and outside the terminal
// phase — the key must refuse rather than quietly stash a value.
func TestBrakeRefusedOutsideTheCopilotSeat(t *testing.T) {
	w, _ := seatedPair(t, true, 0)
	if _, ok := w.StepRendezvousBrake(false); ok {
		t.Error("the initiator braked instead of flying the clock")
	}
	w2, primary, st := anchorWorld(t)
	w2.EngageRendezvousWarp("SHA256:gern", "gern", st.Add(time.Hour), 0)
	near := peerAt(w2, primary, st, 50, orbital.Vec3{X: 5.4e6}, orbital.Vec3{}, "gern")
	near.ArmedTowardViewer, near.RendezvousInitiator = true, true
	w2.DriveRendezvousWarp([]CoWarpPeer{near})
	if _, ok := w2.StepRendezvousBrake(false); ok {
		t.Error("braked during the shared coast — the coast derives its own rate from τ")
	}
}
