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

	// bumpMinNormalM is the closest-point distance under which the contact
	// normal is taken from the line between the two positions instead.
	bumpMinNormalM = 0.05
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
// takes the touch instead of a bump: the port gate holds (docking_magnet.go,
// the same predicate the fuse and the Proximity ring read) and the pair is
// not re-arm latched, or a magnet pull is running on this pair.
func bumpPairInGate(w *World, a, b *spacecraft.Spacecraft) bool {
	if m := w.dockMagnet; m != nil {
		if (m.pullerID == a.ID && m.otherID == b.ID) || (m.pullerID == b.ID && m.otherID == a.ID) {
			return true
		}
	}
	return dockPairGateHolds(a, b) && !w.isLocalReArmed(a.ID, b.ID)
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

// hullsTouch is the contact test between two hulls given as axis segments
// with widths. Each hull is a FLAT-ENDED cylinder, not a capsule: radius
// W/2 applies alongside a hull, and there is no rounded cap beyond the nose
// or below the base. So when the closest points of the two axes are both
// segment ENDS (nose against nose, nose against tail), the hulls touch only
// if they actually overlap axially, not merely because the ends are within
// the radii of each other. Without that, two coaxial noses would "touch"
// metres apart and a straight, slow, aligned approach could never reach a
// 0.5 m port gate. (ADR 0053 decision 8 says "closer than (W_A+W_B)/2" on
// the segments, which is the capsule; that formula is wrong at the ends.)
// n is the closest-point vector pa - pb (B to A); it is zero for coaxial
// overlap, where the caller falls back to the line between positions.
func hullsTouch(a0, a1 orbital.Vec3, wa float64, b0, b1 orbital.Vec3, wb float64) (bool, orbital.Vec3) {
	pa, pb := segmentClosest(a0, a1, b0, b1)
	n := pa.Sub(pb)
	if n.Norm() >= (wa+wb)/2 {
		return false, n
	}
	const eps = 1e-9
	endA, noseA := segmentEnd(pa, a0, a1)
	endB, noseB := segmentEnd(pb, b0, b1)
	if endA && endB {
		// Nose of A: B's point must not lie beyond it. Tail: not behind it.
		ua, ub := a1.Sub(a0).Unit(), b1.Sub(b0).Unit()
		if along := pb.Sub(pa).Dot(ua); (noseA && along > eps) || (!noseA && along < -eps) {
			return false, n
		}
		if along := pa.Sub(pb).Dot(ub); (noseB && along > eps) || (!noseB && along < -eps) {
			return false, n
		}
	}
	return true, n
}

// hullNearParallelCos is the |cos| of the axis angle above which two hulls are
// treated as lying (anti)parallel for the contact test: within 45 degrees.
const hullNearParallelCos = 0.7071

// hullContact is the contact test AND the contact normal for two hulls
// (flat-ended cylinders as axis segments with widths). It returns whether they
// touch and, when they do, the unit normal pointing from B to A (the
// direction A is pushed), or the zero vector when no usable normal exists and
// the caller should fall back to the line between the positions. A
// cross-player bump against a ghost's reported hull calls this same helper.
//
// For hulls lying within 45 degrees of parallel the contact is decided by
// LEAST PENETRATION, not by the closest-point line between the axes: with the
// noses laterally off the same line by more than a few cm (a crooked or
// off-centre nose-first hit) that line is the mostly LATERAL nose-to-nose
// offset, while the real contact is the end FACE, so the closing speed along
// it read ~0 and the hulls passed through (review finding 1). Here the axial
// overlap (how far the entering end has crossed the other's face plane) is
// compared with the lateral depth ((wa+wb)/2 - axis distance); the smaller
// one is the face the hulls are really touching, so a nose-first approach
// bounces along the axis, and a side hit, whose axial overlap is long, keeps
// the lateral normal. Touch needs axial overlap, which keeps the flat-ended
// rule (two noses never touch with a positive gap between them). Hulls at a
// steeper angle (a nose into a flank) use hullsTouch and its closest-point
// normal unchanged.
func hullContact(a0, a1 orbital.Vec3, wa float64, b0, b1 orbital.Vec3, wb float64) (bool, orbital.Vec3) {
	ua, ub := a1.Sub(a0).Unit(), b1.Sub(b0).Unit()
	cos := ua.Dot(ub)
	if math.Abs(cos) < hullNearParallelCos || ua.Norm() == 0 || ub.Norm() == 0 {
		touch, n := hullsTouch(a0, a1, wa, b0, b1, wb)
		if !touch || n.Norm() < bumpMinNormalM {
			return touch, orbital.Vec3{}
		}
		return true, n.Unit()
	}
	pa, pb := segmentClosest(a0, a1, b0, b1)
	n := pa.Sub(pb)
	rsum := (wa + wb) / 2
	if n.Norm() >= rsum {
		return false, orbital.Vec3{}
	}
	if cos < 0 {
		ub = ub.Scale(-1)
	}
	u := ua.Add(ub).Unit()
	// Axial intervals along u, relative to b0 to keep the numbers small.
	ta0, ta1 := a0.Sub(b0).Dot(u), a1.Sub(b0).Dot(u)
	tb0, tb1 := 0.0, b1.Sub(b0).Dot(u)
	loA, hiA := math.Min(ta0, ta1), math.Max(ta0, ta1)
	loB, hiB := math.Min(tb0, tb1), math.Max(tb0, tb1)
	overlap := math.Min(hiA, hiB) - math.Max(loA, loB)
	if overlap <= 1e-9 {
		return false, orbital.Vec3{}
	}
	if overlap <= rsum-n.Norm() {
		if (loA+hiA)/2 >= (loB+hiB)/2 {
			return true, u
		}
		return true, u.Scale(-1)
	}
	if n.Norm() < bumpMinNormalM {
		return true, orbital.Vec3{}
	}
	return true, n.Unit()
}

// segmentEnd reports whether p sits on an end of segment p0-p1, and if so
// whether it is the p1 (nose) end.
func segmentEnd(p, p0, p1 orbital.Vec3) (isEnd, isP1 bool) {
	const tol = 1e-9
	if p.Sub(p1).Norm() <= tol {
		return true, true
	}
	if p.Sub(p0).Norm() <= tol {
		return true, false
	}
	return false, false
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
			touch, nh := hullContact(a0, a1, wa, b0, b1, wb)
			if !touch {
				delete(w.bumpOverlap, key)
				continue
			}
			if w.bumpOverlap[key] || bumpPairInGate(w, a, b) {
				continue
			}
			// No usable contact normal (collinear axes carrying only
			// integrator-noise lateral offset): fall back to the line
			// between the positions.
			if nh.Norm() == 0 {
				if dr.Norm() < 1e-9 {
					continue
				}
				nh = dr.Unit()
			}
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
