package sim

import (
	"testing"
	"time"
)

// #395 review, finding 1 (internal/sim/auto_warp.go:780): the pacing
// deadband holdRendezvousLeader compares `ahead` against is
// rendezvousWaypointMinLead — a flat 5 SIM-second constant — but `ahead`
// is dominated by relay report staleness at any real coast rate: a
// partner's CoWarpPeer.SubspaceTime is only as fresh as its last
// relay.Heartbeat (5 WALL-clock seconds), so at effective warp W the
// staleness alone is up to ~5*W sim-seconds. A flat 5-sim-second deadband
// clears above ~1x warp and, once `ahead` (pure staleness, no genuine
// divergence) exceeds rendezvousPaceCeilingSec, rendezvousPaceMaxWarp
// returns (0, true) — clampedWarp's RendezvousPaced clamp then drives the
// leader's selection to 0. That is the #279 dead-0x stall #395 shipped to
// kill, reintroduced by the flat deadband.
//
// This harness reproduces the mechanism directly (unlike
// rendezvous_pace_harness_test.go's twoWorldRendezvousHarness, which
// always hands DriveRendezvousWarp the partner's freshest in-memory
// state — no relay staleness at all, so it cannot exercise this bug):
// world B ticks every iteration (never paused, so its true subspace time
// keeps advancing), but the CoWarpPeer snapshot fed to world A's
// DriveRendezvousWarp is only refreshed once every simulated
// relay.Heartbeat of WALL-clock time — measured via Clock.BaseStep, the
// real-time-per-tick constant World.Tick already scales by the effective
// warp (world.go's `simDelta := BaseStep * effWarp`).
const testHarnessHeartbeatSec = 5.0 // mirrors relay.Heartbeat (internal/relay/reporter.go); sim cannot import relay (cycle)

// throttledPeerHarness pairs two engaged Worlds and feeds A a
// heartbeat-throttled view of B, reproducing report staleness without
// simulating the relay transport itself.
type throttledPeerHarness struct {
	t    *testing.T
	a, b *World

	reported     CoWarpPeer // last relayed snapshot of B seen by A
	wallSinceRep float64    // wall-seconds since `reported` was refreshed
}

func newThrottledPeerHarness(t *testing.T, tauFromNow time.Duration) *throttledPeerHarness {
	t.Helper()
	a, b := mustWorld(t), mustWorld(t)
	if !a.EngageRendezvousWarp(harnessOwnerB, "bob", a.Clock.SimTime.Add(tauFromNow), 0) {
		t.Fatal("A: EngageRendezvousWarp failed")
	}
	if !b.EngageRendezvousWarp(harnessOwnerA, "alice", b.Clock.SimTime.Add(tauFromNow), 0) {
		t.Fatal("B: EngageRendezvousWarp failed")
	}
	h := &throttledPeerHarness{t: t, a: a, b: b}
	// First mutual pass on both sides so the coast actually starts,
	// mirroring twoWorldRendezvousHarness's construction. Both peers here
	// are necessarily pre-coast snapshots (EffectiveWarp() still the
	// default 1x — the max-seed only applies once DriveRendezvousWarp has
	// actually started the coast), so `reported` is re-captured below
	// AFTER both sides are engaged and running at their real rate —
	// otherwise the harness would hold a stale EffWarp=1 "report" for a
	// full heartbeat, exactly reproducing the deadband's OWN bug as a
	// harness artifact rather than testing it.
	h.a.DriveRendezvousWarp([]CoWarpPeer{h.peerFromB()})
	h.b.DriveRendezvousWarp([]CoWarpPeer{h.peerFromA()})
	h.reported = h.peerFromB()
	return h
}

func (h *throttledPeerHarness) peerFromB() CoWarpPeer {
	return CoWarpPeer{
		Owner: harnessOwnerB, Handle: "bob",
		SubspaceTime: h.b.Clock.SimTime, EffWarp: h.b.EffectiveWarp(),
		Paused: h.b.Clock.Paused, ArmedTowardViewer: true,
	}
}

func (h *throttledPeerHarness) peerFromA() CoWarpPeer {
	return CoWarpPeer{
		Owner: harnessOwnerA, Handle: "alice",
		SubspaceTime: h.a.Clock.SimTime, EffWarp: h.a.EffectiveWarp(),
		Paused: h.a.Clock.Paused, ArmedTowardViewer: true,
	}
}

// step advances B a real tick (always fresh to itself, never paused),
// then advances A against a peer snapshot that only refreshes once a
// simulated relay.Heartbeat of wall-clock time has elapsed — exactly
// the cadence relay.Reporter.Tick uses for a steady coast (elements
// Kepler-constant, EffWarp constant: nothing forces an early report).
//
// Ordering matters and mirrors production exactly (internal/serve/
// reporting.go's Update): World.Tick() runs first each iteration, using
// whatever RendezvousPaced/RendezvousPaceWarp the PREVIOUS DriveRendezvous-
// Warp call left set ("clampedWarp reads it on the sim tick that
// follows" — auto_warp.go's DriveRendezvousWarp doc). DriveRendezvousWarp
// runs after, recomputing `ahead` from the tick that just happened and
// setting the pacing for the NEXT tick. That one-tick lag is exactly
// what lets `ahead` jump past rendezvousPaceCeilingSec in a single step
// before the ramp reacts — collapsing the harness's two calls into one
// tight per-tick feedback loop (Drive-then-Tick, same iteration) hides
// the bug entirely: the ramp gets to react before the SAME tick's
// advance, so it self-limits smoothly and never overshoots.
func (h *throttledPeerHarness) step() {
	h.b.Tick()
	h.b.DriveRendezvousWarp([]CoWarpPeer{h.peerFromA()})

	h.wallSinceRep += h.a.Clock.BaseStep.Seconds()
	if h.wallSinceRep >= testHarnessHeartbeatSec {
		h.reported = h.peerFromB()
		h.wallSinceRep = 0
	}

	h.a.Tick()
	h.a.DriveRendezvousWarp([]CoWarpPeer{h.reported})
}

// TestRendezvousPaceDeadbandStallsAtHighWarp is the finding-1 regression
// guard. Revert the fix (restore `if ahead <= rendezvousWaypointMinLead.
// Seconds() { return }` as the sole guard, feeding raw `ahead` into
// rendezvousPaceMaxWarp) and this test fails: A's clock goes dead (no
// tick advances its SimTime at all) on a large fraction of ticks, purely
// from relay report staleness against a partner that is never paused and
// never genuinely falls behind. "Dead" is measured directly as
// SimTime-does-not-advance across a tick, not EffectiveWarp()==0: the
// continuous ramp's asymptotic approach toward the ceiling can converge
// a float64 warp down through denormally small values that still round
// to a nonzero EffectiveWarp() yet, once World.Tick's `time.Duration(
// BaseStep * effWarp)` truncates below 1ns, produce the exact same
// player-visible dead stop — measuring the truncated advance is the
// honest check, matching what a player watching the clock would see.
func TestRendezvousPaceDeadbandStallsAtHighWarp(t *testing.T) {
	h := newThrottledPeerHarness(t, 200*time.Hour)
	if !h.a.RendezvousWarpEngaged() || !h.b.RendezvousWarpEngaged() {
		t.Fatal("precondition: the mutual coast did not start")
	}
	if warp := h.a.EffectiveWarp(); warp < 100 {
		t.Fatalf("precondition: A's effective warp is %.1f, want >= 100x for this reproduction", warp)
	}
	t.Logf("A's steady effective warp: %.1fx", h.a.EffectiveWarp())

	const ticks = 300
	pacedTransitions := 0
	prevPaced := h.a.RendezvousPaced
	deadTicks := 0
	for i := 0; i < ticks; i++ {
		before := h.a.Clock.SimTime
		h.step()
		if !h.a.RendezvousWarpEngaged() {
			t.Fatalf("coast dropped mid-run at tick %d", i)
		}
		if h.a.RendezvousPaced != prevPaced {
			pacedTransitions++
			prevPaced = h.a.RendezvousPaced
		}
		if h.a.Clock.SimTime.Equal(before) {
			deadTicks++
		}
	}

	deadFraction := float64(deadTicks) / float64(ticks)
	t.Logf("pacedTransitions=%d deadTicks=%d/%d (%.0f%%)", pacedTransitions, deadTicks, ticks, deadFraction*100)

	const maxDeadFraction = 0.05
	if deadFraction > maxDeadFraction {
		t.Errorf("A's clock went dead on %d/%d ticks (%.0f%%) from pure report staleness "+
			"(B never paused, never genuinely diverging) — the #279 stall #395 was meant to kill "+
			"(want <= %.0f%%)", deadTicks, ticks, deadFraction*100, maxDeadFraction*100)
	}
}
