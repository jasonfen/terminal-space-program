package sim

import (
	"math"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
)

// DefaultLunarTransferLead is how far before the next ideal Moon-transfer
// departure a fresh game should start. Short enough to reach by warping a
// little, long enough to look at the system and set up the burn first.
const DefaultLunarTransferLead = 4 * time.Hour

// lunarStartTolerance bounds how far the chosen start may sit from the
// requested lead. The achievable departure wait is quantized to the craft's
// parking period (~94 min for a 500 km LEO), so we accept anything within one
// such period of the target.
const lunarStartTolerance = 95 * time.Minute

// AdjustStartForLunarTransferWindow shifts the world clock so the next ideal
// Moon-transfer departure (the split-strategy line-of-nodes crossing the
// planner would plant on `H`) sits ~lead away, instead of the ~10 days out
// that the J2000 epoch yields. It mutates only the clock — the craft and the
// calculator are untouched — so the Moon's phase (a pure function of SimTime
// since the J2000-anchored ephemeris fix) moves with the chosen start.
//
// Returns false and leaves the clock at its original value if there's no
// Moon, no craft in orbit around it, or no start within tolerance can be
// found. A false return is harmless: the game simply opens at J2000.
func (w *World) AdjustStartForLunarTransferWindow(lead time.Duration) bool {
	sys := w.System()
	moon := sys.FindBody("Moon")
	if moon == nil {
		return false
	}
	c := w.ActiveCraft()
	if c == nil || c.Primary.ID != moon.ParentID {
		return false
	}

	base := w.Clock.SimTime
	leadSec := lead.Seconds()

	// Fixed-point search. The departure wait τ decreases ~1:1 as the start
	// advances (the node crossing is a fixed Moon-ephemeris event), so moving
	// the start later by (τ − lead) lands it `lead` before that same crossing.
	// One step from J2000 essentially converges; a few more refine. We never
	// step past the crossing, so there's no overshoot.
	epoch := base
	for i := 0; i < 5; i++ {
		w.Clock.SimTime = epoch
		tau, ok := w.lunarDepartureWait(*moon)
		if !ok {
			w.Clock.SimTime = base
			return false
		}
		delta := tau - leadSec
		if math.Abs(delta) < 300 { // within 5 min — let the local scan finish
			break
		}
		epoch = epoch.Add(time.Duration(delta * float64(time.Second)))
	}

	// Local pick. τ is a step function of the start quantized to the parking
	// period (the craft's spawn geometry fixes the in-plane departure point),
	// so scan a small window and keep the start whose τ is closest to lead.
	bestEpoch := epoch
	bestErr := math.Inf(1)
	for off := -3 * time.Hour; off <= 3*time.Hour; off += 5 * time.Minute {
		cand := epoch.Add(off)
		w.Clock.SimTime = cand
		tau, ok := w.lunarDepartureWait(*moon)
		if !ok {
			continue
		}
		if e := math.Abs(tau - leadSec); e < bestErr {
			bestErr = e
			bestEpoch = cand
		}
	}

	if bestErr > lunarStartTolerance.Seconds() {
		w.Clock.SimTime = base
		return false
	}

	w.Clock.SimTime = bestEpoch
	w.Clock.RotationTime = bestEpoch
	return true
}

// lunarDepartureWait returns the wait (seconds) until the split-strategy
// departure burn would fire for a transfer to `target`, reading the current
// w.Clock.SimTime. It is read-only and mirrors the split-timing setup in
// PlanTransfer's intra-primary branch, calling the same pure helpers so the
// number tracks what the game actually plants on `H`.
func (w *World) lunarDepartureWait(target bodies.CelestialBody) (float64, bool) {
	c := w.ActiveCraft()
	if c == nil {
		return 0, false
	}
	muShared := c.Primary.GravitationalParameter()
	rPark := c.State.R.Norm()
	rArrival := target.SemimajorAxisMeters()
	dvDep := estimateIntraPrimaryDepDv(muShared, rPark, rArrival) +
		intraPlaneChangeAllowance(w, c, target, muShared, rPark)
	minLead := c.BurnTimeForDV(dvDep).Seconds() / 2
	tau, _, ok := w.splitNodePhasing(c, target, muShared, rPark, rArrival, minLead)
	return tau, ok
}
