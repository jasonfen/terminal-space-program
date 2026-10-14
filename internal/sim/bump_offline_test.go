package sim

import (
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
)

// tiltedNoseFirst builds two RCS tugs nose to nose on A's +Y axis, B closing
// at closeMS, with B's nose tipped tiltDeg about its BASE (State.R, the way a
// live attitude hold drifts: not about the port) and laterally missing A's
// axis by missM at the nose. noseGapM is the axial gap between the noses. Both
// slews are pinned so the fixture holds its pointing, and the pair is
// stepped by hand with checkBumps (no Tick re-pointing).
func tiltedNoseFirst(t *testing.T, tiltDeg, missM, noseGapM, closeMS float64) *portPair {
	t.Helper()
	p := newPortPair(t, "rcs-tug", "rcs-tug", 30, 0, 0)
	a, b := p.a, p.b
	a.SlewRateDegPerSec = 1e-9
	b.SlewRateDegPerSec = 1e-9
	ga := VesselPortGeometry(a)
	gb := VesselPortGeometry(b)
	noseA := a.State.R.Add(p.axis.Scale(ga.NoseM - dockPortTipInsetM))
	bAxis := orbital.Rotate(p.axis.Scale(-1), orbital.Vec3{Z: 1}, tiltDeg*math.Pi/180).Unit()
	b.CurrentAttitudeDir = bAxis
	// B's BASE sits missM off A's axis (X) and its nose noseGapM above A's
	// nose in Y; the tilt then swings the nose sideways about the base.
	ln := gb.NoseM - dockPortTipInsetM
	baseB := noseA.Add(orbital.Vec3{X: missM}).Add(p.axis.Scale(noseGapM))
	baseB = baseB.Sub(p.axis.Scale(bAxis.Dot(p.axis) * ln))
	b.State = physics.StateVector{
		R: baseB,
		V: a.State.V.Sub(p.axis.Scale(closeMS)),
		M: b.TotalMass(),
	}
	return p
}

// stepHand moves both vessels on their velocities and runs the bump scan.
func stepHand(p *portPair, dt float64) {
	p.a.State.R = p.a.State.R.Add(p.a.State.V.Scale(dt))
	p.b.State.R = p.b.State.R.Add(p.b.State.V.Scale(dt))
	p.w.checkBumps()
}

// noseOverlapM is how far B's nose has crossed A's nose along A's axis
// (positive = interpenetrating).
func noseOverlapM(p *portPair) float64 {
	_, a1, _, _ := hullSegment(p.a)
	_, b1, _, _ := hullSegment(p.b)
	return a1.Sub(b1).Dot(p.axis)
}

// ADR 0053 decision 8's headline cases, "too crooked, or not at the ports":
// a nose-first hit that is a little crooked (tilted about the base) or
// laterally off the line must bump with the TRUE closing speed and open, not
// pass through with a 0.00 m/s caption (review finding 1).
func TestBumpNoseFirstOffLineBouncesWithTrueClosingSpeed(t *testing.T) {
	const closeMS = 0.3
	for _, c := range []struct {
		name        string
		tilt, missM float64
	}{
		{"coaxial", 0, 0},
		{"1deg about the base", 1, 0},
		{"2deg about the base", 2, 0},
		{"1m lateral miss", 0, 1},
		{"2m lateral miss", 0, 2},
		{"2deg plus 1m miss", 2, 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			p := tiltedNoseFirst(t, c.tilt, c.missM, 0.4, closeMS)
			bumped := false
			deepest := 0.0
			for i := 0; i < 1200; i++ {
				stepHand(p, 0.05)
				if p.w.LastBumpEvent != nil {
					bumped = true
				}
				deepest = math.Max(deepest, noseOverlapM(p))
			}
			if !bumped {
				t.Fatalf("no bump: hulls passed through (deepest overlap %.2f m)", deepest)
			}
			if got := p.w.LastBumpEvent.SpeedMS; math.Abs(got-closeMS) > 0.03 {
				t.Errorf("caption speed = %.3f m/s, want the true closing speed %.2f", got, closeMS)
			}
			if deepest > 0.3 {
				t.Errorf("hulls interpenetrated %.2f m; a 0.3 m/s hit should stop within a step", deepest)
			}
			if open := p.b.State.V.Sub(p.a.State.V).Dot(p.axis); math.Abs(open-closeMS/2) > 0.03 {
				t.Errorf("opening speed after the bump = %.3f m/s, want %.2f (half the contact speed)", open, closeMS/2)
			}
		})
	}
}

// Side hits keep the lateral normal: a hull sweeping into another's flank is
// not turned into an axial bounce by the nose-first rule.
func TestBumpSideHitKeepsLateralNormal(t *testing.T) {
	w, a, b := bumpWorld(t, 4, -0.6) // parallel hulls side by side, closing along X
	w.checkBumps()
	if w.LastBumpEvent == nil {
		t.Fatal("side hit did not bump")
	}
	if open := b.State.V.Sub(a.State.V); math.Abs(open.X-0.3) > 0.03 || math.Abs(open.Y) > 1e-6 {
		t.Errorf("side hit opened %+v, want +0.3 m/s along X only", open)
	}
}
