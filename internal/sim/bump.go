package sim

import (
	"math"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// The bump (ADR 0053 decision 8, slice D). Two hulls that touch outside the
// docking gate bounce: the pair is pushed apart along the line between the
// closest points of the two hull segments, momentum-conserving, so it opens
// at half the contact speed (never under bumpFloorMS). Nothing breaks. The
// bottom edge says what happened (BumpEvent, consumed by App.flashBump).
//
// Local vessels only. Cross-player (slice E) must run this same test against
// a ghost's reported hull (relay.CraftState nose_m / width_m / attitude) and
// push ONLY the bumper, treating the ghost as immovable:
// dv_A = +n*(s + s'). The bumped seat is not told this cycle (ADR 0053
// "Settled here" item 2); the bumpOverlap latch below keys on vessel IDs, so
// E needs a key that a ghost can have too.

const (
	// bumpFloorMS keeps two hulls from resting inside each other: the pair
	// always opens at least this fast.
	bumpFloorMS = 0.05
)

// BumpEvent records the latest hull contact outside the docking gate for
// the bottom-edge caption. Same one-shot contract as DockEvent: App reads
// and clears World.LastBumpEvent.
type BumpEvent struct {
	When time.Time
	// PartnerName is the vessel the active vessel bumped (the second of the
	// pair when neither is active).
	PartnerName string
	// SpeedMS is the closing speed along the line of contact at the touch.
	SpeedMS float64
}

// bumpPairInGate reports whether the pair is in a state where the dock
// takes the touch instead of a bump: the gate holds, or a magnet is pulling.
//
// THE PLUG-IN POINT FOR SLICE B. Today the dock is still the click
// checkDocking does (centres within DockingDistM, relative speed within
// DockingVMS, no re-arm latch), so that is what this mirrors. B replaces
// the body with its port gate (0.5 m / 0.1 m/s / 4 degrees on the two
// ports) OR "a magnet pull is running on this pair". Nothing else in this
// file needs to change.
func bumpPairInGate(w *World, a, b *spacecraft.Spacecraft) bool {
	if a.State.R.Sub(b.State.R).Norm() > DockingDistM {
		return false
	}
	if a.State.V.Sub(b.State.V).Norm() > DockingVMS {
		return false
	}
	return !w.isLocalReArmed(a.ID, b.ID)
}

func bumpKey(a, b *spacecraft.Spacecraft) [2]uint64 {
	if a.ID > b.ID {
		return [2]uint64{b.ID, a.ID}
	}
	return [2]uint64{a.ID, b.ID}
}

// hullSegment is a vessel's hull as the axis segment from the tail of the
// bottom row to the drawn nose, plus its width. State.R is the bottom of
// the stack (dockport.go), and a hull spans -half a row to L - half a row
// along the axis.
func hullSegment(c *spacecraft.Spacecraft) (p0, p1 orbital.Vec3, width float64, ok bool) {
	g := VesselPortGeometry(c)
	if !g.Known || g.NoseM <= 0 {
		return orbital.Vec3{}, orbital.Vec3{}, 0, false
	}
	p0 = c.State.R.Sub(g.Axis.Scale(dockPortTipInsetM))
	p1 = c.State.R.Add(g.Axis.Scale(g.NoseM - dockPortTipInsetM))
	return p0, p1, g.WidthM, true
}

// segmentClosest returns the closest points between segments p0-p1 and
// q0-q1 (Ericson, Real-Time Collision Detection 5.1.9).
func segmentClosest(p0, p1, q0, q1 orbital.Vec3) (cp, cq orbital.Vec3) {
	d1, d2, r := p1.Sub(p0), q1.Sub(q0), p0.Sub(q0)
	a, e, f := d1.Dot(d1), d2.Dot(d2), d2.Dot(r)
	const eps = 1e-12
	var s, t float64
	clamp := func(x float64) float64 { return math.Max(0, math.Min(1, x)) }
	switch {
	case a <= eps && e <= eps:
	case a <= eps:
		t = clamp(f / e)
	default:
		c := d1.Dot(r)
		if e <= eps {
			s = clamp(-c / a)
		} else {
			b := d1.Dot(d2)
			den := a*e - b*b
			if den > eps {
				s = clamp((b*f - c*e) / den)
			}
			t = (b*s + f) / e
			if t < 0 {
				t, s = 0, clamp(-c/a)
			} else if t > 1 {
				t, s = 1, clamp((b-c)/a)
			}
		}
	}
	return p0.Add(d1.Scale(s)), q0.Add(d2.Scale(t))
}

// checkBumps runs the contact test over every pair of flying vessels inside
// DockingDistM (centre to centre) and bumps the ones whose hulls touch
// outside the gate. Edge-triggered through World.bumpOverlap: a pair bumps
// once per contact, not once per tick it stays overlapped.
func (w *World) checkBumps() {
	if len(w.Crafts) < 2 {
		return
	}
	for i := 0; i < len(w.Crafts); i++ {
		a := w.Crafts[i]
		if a == nil {
			continue
		}
		for j := i + 1; j < len(w.Crafts); j++ {
			b := w.Crafts[j]
			if b == nil || a.SystemIdx != b.SystemIdx || a.Primary.ID != b.Primary.ID {
				continue
			}
			// Same exclusions as checkDocking: Landed craft are pinned to the
			// ground and debris never meets debris.
			if a.Landed || b.Landed {
				continue
			}
			if a.Role == spacecraft.RoleJettisonedStage && b.Role == spacecraft.RoleJettisonedStage {
				continue
			}
			key := bumpKey(a, b)
			dr := a.State.R.Sub(b.State.R)
			if dr.Norm() > DockingDistM {
				delete(w.bumpOverlap, key)
				continue
			}
			a0, a1, wa, okA := hullSegment(a)
			b0, b1, wb, okB := hullSegment(b)
			if !okA || !okB {
				continue
			}
			pa, pb := segmentClosest(a0, a1, b0, b1)
			n := pa.Sub(pb)
			if n.Norm() >= (wa+wb)/2 {
				delete(w.bumpOverlap, key)
				continue
			}
			if w.bumpOverlap[key] || bumpPairInGate(w, a, b) {
				continue
			}
			// Crossing or collinear segments have no closest-point line;
			// fall back to the line between the positions.
			if n.Norm() < 1e-6 {
				n = dr
			}
			if n.Norm() < 1e-9 {
				continue
			}
			nh := n.Unit()
			s := -a.State.V.Sub(b.State.V).Dot(nh)
			if s <= 0 {
				continue // already parting: nothing to bounce
			}
			mA, mB := a.TotalMass(), b.TotalMass()
			if mA <= 0 || mB <= 0 {
				continue
			}
			sp := math.Max(s/2, bumpFloorMS)
			kick := s + sp
			a.State.V = a.State.V.Add(nh.Scale(kick * mB / (mA + mB)))
			b.State.V = b.State.V.Sub(nh.Scale(kick * mA / (mA + mB)))
			if w.bumpOverlap == nil {
				w.bumpOverlap = map[[2]uint64]bool{}
			}
			w.bumpOverlap[key] = true
			partner := b
			if b == w.ActiveCraft() {
				partner = a
			}
			var when time.Time
			if w.Clock != nil {
				when = w.Clock.SimTime
			}
			w.LastBumpEvent = &BumpEvent{When: when, PartnerName: partner.Name, SpeedMS: s}
		}
	}
}

// activeNearOtherHull reports whether the active vessel is inside
// DockingDistM of another non-debris hull in the same frame: the zone where
// warp is capped at burnWarpCap so two hulls cannot skip through each other
// between ticks (ADR 0053 decision 8). Landed vessels are exempt, as they
// are from docking and bumping.
func (w *World) activeNearOtherHull() bool {
	a := w.ActiveCraft()
	if a == nil || a.Landed {
		return false
	}
	for _, b := range w.Crafts {
		if b == nil || b == a || b.Landed || b.Role == spacecraft.RoleJettisonedStage {
			continue
		}
		if a.SystemIdx != b.SystemIdx || a.Primary.ID != b.Primary.ID {
			continue
		}
		if a.State.R.Sub(b.State.R).Norm() <= DockingDistM {
			return true
		}
	}
	return false
}
