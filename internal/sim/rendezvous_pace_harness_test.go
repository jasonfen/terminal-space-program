package sim

import (
	"math"
	"testing"
	"time"
)

// #395 (ADR 0045 S2, closing #279): nothing in the existing suite ticks
// two *World instances against each other through the actual relay peer
// path — that absence is precisely why #279 was re-triaged twice on "does
// this still happen" grounds. This harness drives two independently-
// ticking Worlds through CoWarpPeer / DriveRendezvousWarp / ComputeCoWarp,
// the same sequence internal/serve/reporting.go's refreshSession runs on
// EVERY tick (confirmed by reading app.go's Update loop — refreshSession
// is not throttled to any report heartbeat; only the WRITE of a fresh
// report to the shared store is), with one deliberate asymmetry: the two
// Worlds tick at different real-world rates. That is a REAL, permanent
// divergence source (client tick-rate difference) — distinct from #248's
// report staleness, which the armed-partner min-wins exemption already
// handles and which this harness deliberately does not model (each
// world always reads the OTHER's freshest in-memory state, no simulated
// relay lag) so the reproduction isolates the tick-rate mechanism alone.

const (
	harnessOwnerA = "SHA256:alice"
	harnessOwnerB = "SHA256:bob"
)

// twoWorldRendezvousHarness owns a pair of mutually-armed *World
// instances and steps them tick-by-tick, interleaved.
type twoWorldRendezvousHarness struct {
	t    *testing.T
	a, b *World

	coupledFromA map[string]bool
	coupledFromB map[string]bool
}

func newTwoWorldRendezvousHarness(t *testing.T, tauFromNow time.Duration) *twoWorldRendezvousHarness {
	t.Helper()
	a := mustWorld(t)
	b := mustWorld(t)
	if !a.EngageRendezvousWarp(harnessOwnerB, "bob", a.Clock.SimTime.Add(tauFromNow), 0) {
		t.Fatal("A: EngageRendezvousWarp failed")
	}
	if !b.EngageRendezvousWarp(harnessOwnerA, "alice", b.Clock.SimTime.Add(tauFromNow), 0) {
		t.Fatal("B: EngageRendezvousWarp failed")
	}
	h := &twoWorldRendezvousHarness{t: t, a: a, b: b}
	// First mutual pass on both sides so the coast actually starts before
	// either World ticks.
	h.refreshA()
	h.refreshB()
	return h
}

func (h *twoWorldRendezvousHarness) peerFrom(w *World, owner, handle string) CoWarpPeer {
	return CoWarpPeer{
		Owner: owner, Handle: handle,
		SubspaceTime: w.Clock.SimTime, EffWarp: w.EffectiveWarp(),
		Paused: w.Clock.Paused, ArmedTowardViewer: true,
	}
}

// refreshA / refreshB run DriveRendezvousWarp + ComputeCoWarp for one
// side against the OTHER side's CURRENT (always-fresh — see the package
// doc comment above) state — exactly what refreshSession does every tick
// in production, minus relay staleness.
func (h *twoWorldRendezvousHarness) refreshA() {
	peerB := h.peerFrom(h.b, harnessOwnerB, "bob")
	h.a.DriveRendezvousWarp([]CoWarpPeer{peerB})
	res := h.a.ComputeCoWarp([]CoWarpPeer{peerB}, h.coupledFromA)
	h.coupledFromA = res.CoupledOwners
	h.a.CoWarp = res.State
}

func (h *twoWorldRendezvousHarness) refreshB() {
	peerA := h.peerFrom(h.a, harnessOwnerA, "alice")
	h.b.DriveRendezvousWarp([]CoWarpPeer{peerA})
	res := h.b.ComputeCoWarp([]CoWarpPeer{peerA}, h.coupledFromB)
	h.coupledFromB = res.CoupledOwners
	h.b.CoWarp = res.State
}

// stepA / stepB are one tick each, refresh-then-advance.
func (h *twoWorldRendezvousHarness) stepA() {
	h.refreshA()
	h.a.Tick()
}

func (h *twoWorldRendezvousHarness) stepB() {
	h.refreshB()
	h.b.Tick()
}

// run advances both worlds for n "units" of real time, ticking A at
// rateA ticks/unit and B at rateB ticks/unit, finely interleaved (a
// fractional accumulator, not batched) so each side's own recompute
// always sees the other's freshest position — the different RATES are
// the whole reproduction, not a batching artifact of the harness.
func (h *twoWorldRendezvousHarness) run(n int, rateA, rateB float64) {
	var accA, accB float64
	for i := 0; i < n; i++ {
		accA += rateA
		accB += rateB
		for accA >= 1 {
			h.stepA()
			accA--
		}
		for accB >= 1 {
			h.stepB()
			accB--
		}
	}
}

func meanStdev(xs []float64) (mean, stdev float64) {
	if len(xs) == 0 {
		return 0, 0
	}
	var sum float64
	for _, x := range xs {
		sum += x
	}
	mean = sum / float64(len(xs))
	var sq float64
	for _, x := range xs {
		d := x - mean
		sq += d * d
	}
	return mean, math.Sqrt(sq/float64(len(xs)))
}

// TestRendezvousLeaderSettlesInsteadOfBangBanging is the harness Part 1
// calls for. World A ticks faster than World B (a permanent real
// tick-rate difference, not report staleness), so A is the one that
// keeps pulling ahead and hitting the pace/hold boundary — the #279
// scenario. B is never paused, so post-fix A must never genuinely
// RendezvousHold (that flag is now reserved for a stopped partner) and
// its Effective Warp must settle on a rate instead of bang-banging
// between the subspace step cap and a dead 0.
//
// Pre-fix (the old holdRendezvousLeader, freezing at exactly
// coWarpSubspaceToleranceSec with no deadband against the couple gate)
// this test fails on both assertions: RendezvousHold flips true/false
// repeatedly, and A's EffectiveWarp alternates between the full step cap
// and 0.
func TestRendezvousLeaderSettlesInsteadOfBangBanging(t *testing.T) {
	h := newTwoWorldRendezvousHarness(t, 200*time.Hour)
	if !h.a.RendezvousWarpEngaged() || !h.b.RendezvousWarpEngaged() {
		t.Fatal("precondition: the mutual coast did not start")
	}

	// A ticks noticeably faster than B — e.g. A's client running at a
	// smooth frame rate while B's is under load. Not exaggerated to a
	// pathological ratio: a real, modest, permanent divergence.
	const rateA, rateB = 1.0, 0.7

	h.run(400, rateA, rateB) // warm-up: let the pair reach steady state

	holdTransitions := 0
	prevHold := h.a.RendezvousHold
	rates := make([]float64, 0, 300)
	zeroCount := 0
	sampleRate := func() {
		if h.a.RendezvousHold != prevHold {
			holdTransitions++
			prevHold = h.a.RendezvousHold
		}
		rate := h.a.EffectiveWarp()
		rates = append(rates, rate)
		if rate == 0 {
			zeroCount++
		}
	}
	// Sample after every one of A's own ticks across the measured window
	// (not just once per harness unit) so the series reflects the actual
	// rate A ran at, tick by tick.
	var accA, accB float64
	for i := 0; i < 300; i++ {
		accA += rateA
		accB += rateB
		for accA >= 1 {
			h.stepA()
			accA--
			if !h.a.RendezvousWarpEngaged() {
				t.Fatalf("coast dropped mid-run at measured step %d (A SimTime=%v, B SimTime=%v)",
					i, h.a.Clock.SimTime, h.b.Clock.SimTime)
			}
			sampleRate()
		}
		for accB >= 1 {
			h.stepB()
			accB--
		}
	}

	if h.a.RendezvousHold {
		t.Error("A ended the run genuinely held — B was never paused, RendezvousHold must not trip here")
	}
	if holdTransitions > 0 {
		t.Errorf("RendezvousHold flipped %d times across %d measured ticks — B is never paused, so this "+
			"must stay false throughout (the #279 bang-bang was exactly this flag toggling)",
			holdTransitions, len(rates))
	}

	mean, stdev := meanStdev(rates)
	if mean == 0 {
		t.Fatalf("A's effective warp collapsed to a permanent 0 across the measured window: %v", rates)
	}
	const maxRatio = 0.5
	if ratio := stdev / mean; ratio > maxRatio {
		t.Errorf("A's effective warp did not settle: stdev/mean = %.3f (want <= %.2f) over %d samples "+
			"(mean=%.1f, stdev=%.1f)", ratio, maxRatio, len(rates), mean, stdev)
	}
	zeroFraction := float64(zeroCount) / float64(len(rates))
	const maxZeroFraction = 0.15
	if zeroFraction > maxZeroFraction {
		t.Errorf("A's effective warp hit exactly 0 on %d/%d measured ticks (%.0f%%) — still bang-banging "+
			"to a dead stop instead of pacing down smoothly", zeroCount, len(rates), zeroFraction*100)
	}
}

// TestRendezvousLeaderHoldsThroughHarnessOnGenuinePause exercises the
// SPLIT the fix makes through the same two-World harness: pacing (drift,
// a live partner) and holding (a genuinely stopped partner) are different
// mechanisms now, not one flag. Pause B mid-run — A must hard-hold
// (RendezvousHold, EffectiveWarp exactly 0) rather than pace, and release
// cleanly (RendezvousHold false again, warp resumes) once B unpauses and
// is back inside the pace ceiling.
func TestRendezvousLeaderHoldsThroughHarnessOnGenuinePause(t *testing.T) {
	h := newTwoWorldRendezvousHarness(t, 200*time.Hour)
	if !h.a.RendezvousWarpEngaged() {
		t.Fatal("precondition: the mutual coast did not start")
	}

	// Run a while at unequal rates first so A has genuinely pulled ahead.
	h.run(200, 1.0, 0.7)

	h.b.Clock.Paused = true
	h.refreshA()
	if !h.a.RendezvousHold {
		t.Fatal("A did not hold against a paused B")
	}
	if h.a.RendezvousPaced {
		t.Error("A reports Paced while genuinely holding — the two must be mutually exclusive")
	}
	if got := h.a.EffectiveWarp(); got != 0 {
		t.Errorf("EffectiveWarp = %v while genuinely held, want 0", got)
	}
	// Advancing A's own ticks under the hold must not move its clock.
	before := h.a.Clock.SimTime
	for i := 0; i < 20; i++ {
		h.refreshA()
		h.a.Tick()
	}
	if !h.a.Clock.SimTime.Equal(before) {
		t.Errorf("A's SimTime advanced by %v while held", h.a.Clock.SimTime.Sub(before))
	}

	// B resumes at A's own clock (no residual gap) — the hold must release.
	h.b.Clock.Paused = false
	h.b.Clock.SimTime = h.a.Clock.SimTime
	h.refreshA()
	if h.a.RendezvousHold {
		t.Error("hold survived B's resume")
	}
}
