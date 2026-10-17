package spacecraft

import (
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
)

// portLeanFixture: a vessel at (7000 km, 0, 0) moving +Y, a target 30 m
// away on a tilted nose. Returns the vessel, the target snapshot, the face
// basis the PORT pulses read, and the Port+ hold (-aT).
func portLeanFixture(t *testing.T) (s *Spacecraft, rT, vT, aT, up, right orbital.Vec3) {
	t.Helper()
	s = &Spacecraft{}
	s.State.R = orbital.Vec3{X: 7e6}
	s.State.V = orbital.Vec3{Y: 7.5e3}
	rT = orbital.Vec3{X: 7e6, Y: 30}
	vT = orbital.Vec3{Y: 7.5e3}
	aT = orbital.Vec3{X: 0.5, Y: 0.6, Z: 0.62}.Unit()
	f, ok := orbital.LVLHBasis(rT, vT)
	if !ok {
		t.Fatal("no frame")
	}
	var okb bool
	up, right, okb = orbital.PortFaceBasis(aT, f)
	if !okb {
		t.Fatal("no basis")
	}
	return
}

func asinDeg(c float64) float64 { return math.Asin(c) * 180 / math.Pi }

// ADR 0055 decision 4: the lean is a VECTOR lean in the port face. A
// combined 3 up + 2 left lean on Port+ reads (3, -2) on the face's two
// axes. Sabotage: ApplyTrims (surface pair) instead of PortLean, or one
// axis dropped, fails this.
func TestPortTrimLeansHoldAcrossThePortFace(t *testing.T) {
	cases := []struct{ up, right float64 }{{1, 0}, {0, 1}, {-1, 0}, {3, -2}, {-2.5, 4}}
	for _, c := range cases {
		s, rT, vT, aT, up, right := portLeanFixture(t)
		s.PortPitchTrim = c.up * math.Pi / 180
		s.PortYawTrim = c.right * math.Pi / 180
		d := s.BurnDirectionWithTargetAtt(BurnPortIn, rT, vT, aT)
		if math.Abs(d.Norm()-1) > 1e-12 {
			t.Fatalf("lean (%v,%v): |dir| = %v", c.up, c.right, d.Norm())
		}
		gu, gr := asinDeg(d.Dot(up)), asinDeg(d.Dot(right))
		// yaw is exact; pitch is scaled by cos(yaw) (<0.3% at 4 deg).
		if math.Abs(gu-c.up) > 0.02 || math.Abs(gr-c.right) > 1e-9 {
			t.Errorf("lean (%v up, %v right): axes read (%.4f, %.4f)", c.up, c.right, gu, gr)
		}
		// Total angle off -aT is the hypotenuse of the two.
		off := math.Acos(d.Dot(aT.Scale(-1))) * 180 / math.Pi
		if want := math.Hypot(c.up, c.right); math.Abs(off-want) > 0.05 {
			t.Errorf("lean (%v, %v): %.4f deg off Port+, want about %.4f", c.up, c.right, off, want)
		}
	}
}

// The lean is applied AFTER the hold: the untrimmed direction (the navball's
// hold glyph) is unmoved, and with zero trims the direction is the hold.
func TestPortTrimLeavesUntrimmedHoldAlone(t *testing.T) {
	s, rT, vT, aT, _, _ := portLeanFixture(t)
	s.PortPitchTrim = 0.05
	s.PortYawTrim = -0.03
	u := s.BurnDirectionUntrimmedWithTargetAtt(BurnPortIn, rT, vT, aT)
	if u.Sub(aT.Scale(-1)).Norm() > 1e-12 {
		t.Errorf("untrimmed hold moved: %v", u)
	}
	s.PortPitchTrim, s.PortYawTrim = 0, 0
	d := s.BurnDirectionWithTargetAtt(BurnPortIn, rT, vT, aT)
	if d.Sub(aT.Scale(-1)).Norm() > 1e-12 {
		t.Errorf("zero trim moved the hold: %v", d)
	}
}

// The lean is relative to the hold wherever it points, not to Port+ only.
func TestPortTrimRidesAnyHold(t *testing.T) {
	s, rT, vT, aT, up, _ := portLeanFixture(t)
	s.PortPitchTrim = 2 * math.Pi / 180
	base := s.BurnDirectionUntrimmedWithTargetAtt(BurnPrograde, rT, vT, aT)
	d := s.BurnDirectionWithTargetAtt(BurnPrograde, rT, vT, aT)
	if ang := math.Acos(d.Dot(base)) * 180 / math.Pi; math.Abs(ang-2) > 1e-6 {
		t.Errorf("lean on Prograde = %v deg, want 2", ang)
	}
	_ = up
}

// No port axis (aT zero): no frame to lean in, so the trim does nothing.
func TestPortTrimNeedsAPortAxis(t *testing.T) {
	s, rT, vT, _, _, _ := portLeanFixture(t)
	s.PortPitchTrim = 0.1
	a := s.BurnDirectionWithTargetAtt(BurnPrograde, rT, vT, orbital.Vec3{})
	s.PortPitchTrim = 0
	b := s.BurnDirectionWithTargetAtt(BurnPrograde, rT, vT, orbital.Vec3{})
	if a.Sub(b).Norm() > 1e-12 {
		t.Errorf("port trim leaned a hold with no port axis")
	}
}

// The InstantSAS thrust closure reads the same lean as the slew path.
func TestPortTrimReachesInstantSASThrust(t *testing.T) {
	s, rT, vT, aT, up, right := portLeanFixture(t)
	s.Thrust = 1000
	s.Stages = []Stage{{DryMass: 100, FuelMass: 100, Thrust: 1000, Isp: 300}}
	s.SyncFields()
	s.PortPitchTrim = 3 * math.Pi / 180
	s.PortYawTrim = -2 * math.Pi / 180
	fn := s.ThrustAccelFnAtWithTargetAtt(BurnPortIn, 0, 1, rT, vT, aT)
	acc := fn(s.State.R, s.State.V, 0)
	d := acc.Unit()
	if gu, gr := asinDeg(d.Dot(up)), asinDeg(d.Dot(right)); math.Abs(gu-3) > 0.05 || math.Abs(gr+2) > 0.05 {
		t.Errorf("closure thrust reads (%.3f, %.3f), want (3, -2)", gu, gr)
	}
}
