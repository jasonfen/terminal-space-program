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
	// Latched is true when the touch met the whole port gate and only the
	// re-arm latch refused it, so the caption names the latch and not the
	// approach speed.
	Latched bool
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
	return hullSegmentOf(c.State.R, VesselPortGeometry(c))
}

// hullSegmentOf is hullSegment for a hull given as a position (the stack
// bottom) and its derived geometry, so a ghost's reported hull and the
// Proximity scene read the same segment.
func hullSegmentOf(r orbital.Vec3, g PortGeometry) (p0, p1 orbital.Vec3, width float64, ok bool) {
	if !g.Known || g.NoseM <= 0 {
		return orbital.Vec3{}, orbital.Vec3{}, 0, false
	}
	p0 = r.Sub(g.Axis.Scale(dockPortTipInsetM))
	p1 = r.Add(g.Axis.Scale(g.NoseM - dockPortTipInsetM))
	return p0, p1, g.WidthM, true
}

// HullDistanceM is the gap between two hulls' SURFACES: how far apart the
// vessels ARE, as opposed to how far apart their anchors are (State.R is the
// BOTTOM of a stack, so a 55 m stack's nose is 55 m from its anchor; ADR 0053
// decision 8's "inside 50 m" is a statement about the hulls, review finding
// 4). It is the one measure behind every "near another hull" rule: the warp
// cap, the bump scan, the Proximity ring, the need reminders and the chip's
// range: row, and it reads 0 when the hulls touch. Hulls within 45 degrees of
// parallel are flat-ended cylinders: the gap is the axial gap between their
// spans combined with the lateral gap between their sides (axis distance less
// the radii). Steeper pairs use the axis-segment distance less the radii. A
// hull whose shape is unknown (attitude uninitialised) falls back to the
// anchor distance, the best that can be said.
func HullDistanceM(aR orbital.Vec3, ga PortGeometry, bR orbital.Vec3, gb PortGeometry) float64 {
	a0, a1, wa, okA := hullSegmentOf(aR, ga)
	b0, b1, wb, okB := hullSegmentOf(bR, gb)
	if !okA || !okB {
		return aR.Sub(bR).Norm()
	}
	axial, lateral, _, n, parallel := hullRelation(a0, a1, wa, b0, b1, wb)
	if !parallel {
		return math.Max(0, n.Norm()-(wa+wb)/2)
	}
	return math.Hypot(axial, lateral)
}

// hullTouchEpsilonM is the micrometre the Proximity box rule uses: spans within
// it of touching are touching, not overlapping.
const hullTouchEpsilonM = 1e-6

// hullRelation is the geometry under HullDistanceM for two known hulls. n is
// the vector between the axis segments' closest points (yours minus theirs).
// parallel is true within hullNearParallelCos of parallel; only then are the
// other results meaningful: axialGap is the clamped gap between the axial
// spans, lateralGap the axis distance less the radii (clamped), and overlap
// is true when the spans STRICTLY overlap, so spans that exactly touch are
// not alongside (the same strictness as hullsTouch).
func hullRelation(a0, a1 orbital.Vec3, wa float64, b0, b1 orbital.Vec3, wb float64) (axialGap, lateralGap float64, overlap bool, n orbital.Vec3, parallel bool) {
	pa, pb := segmentClosest(a0, a1, b0, b1)
	n = pa.Sub(pb)
	ua, ub := a1.Sub(a0).Unit(), b1.Sub(b0).Unit()
	cos := ua.Dot(ub)
	if math.Abs(cos) < hullNearParallelCos {
		return 0, 0, false, n, false
	}
	if cos < 0 {
		ub = ub.Scale(-1)
	}
	u := ua.Add(ub).Unit()
	ta0, ta1 := a0.Sub(b0).Dot(u), a1.Sub(b0).Dot(u)
	tb1 := b1.Sub(b0).Dot(u)
	loA, hiA := math.Min(ta0, ta1), math.Max(ta0, ta1)
	loB, hiB := math.Min(0, tb1), math.Max(0, tb1)
	raw := math.Max(loA, loB) - math.Min(hiA, hiB)
	lateralGap = math.Max(0, n.Sub(u.Scale(n.Dot(u))).Norm()-(wa+wb)/2)
	return math.Max(0, raw), lateralGap, raw < -hullTouchEpsilonM, n, true
}

// craftHullDistanceM is HullDistanceM for two live vessels.
func craftHullDistanceM(a, b *spacecraft.Spacecraft) float64 {
	return HullDistanceM(a.State.R, VesselPortGeometry(a), b.State.R, VesselPortGeometry(b))
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
// DockingDistM (hull to hull, HullDistanceM) and bumps the ones whose hulls touch
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
			if craftHullDistanceM(a, b) > DockingDistM {
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
			w.LastBumpEvent = &BumpEvent{When: when, PartnerName: partner.Name, SpeedMS: s, Latched: dockPairGateHolds(a, b) && w.isLocalReArmed(a.ID, b.ID)}
			if ac := w.ActiveCraft(); ac == a || ac == b {
				w.recordBump(ac, partner.Name, s, w.LastBumpEvent.Latched)
			}
		}
	}
}

// activeNearOtherHull reports whether the active vessel is inside
// DockingDistM (hull to hull) of another non-debris hull in the same frame: the zone where
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
		// ADR 0059 decision 6: a batch does not clamp warp against its own
		// pieces for one orbit after the release. Foreign hulls still do.
		if a.BatchID != 0 && a.BatchID == b.BatchID && w.Clock != nil && w.Clock.SimTime.Before(a.BatchClampExemptUntil) {
			continue
		}
		if craftHullDistanceM(a, b) <= DockingDistM {
			return true
		}
	}
	return false
}
