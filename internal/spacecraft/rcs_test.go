package spacecraft

import (
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
)

// TestApplyRCSPulseDeliversQuantumDV: a single pulse should change
// |v| by RCSDvQuantum (in the direction sign of the burn mode).
func TestApplyRCSPulseDeliversQuantumDV(t *testing.T) {
	systems, _ := bodies.LoadAll()
	earth := systems[0].FindBody("Earth")
	sc := NewInLEO(*earth)
	v0 := sc.OrbitalSpeed()

	if !sc.ApplyRCSPulse(BurnPrograde) {
		t.Fatalf("ApplyRCSPulse(prograde) returned false on a fully-fueled craft")
	}
	got := sc.OrbitalSpeed()
	if math.Abs(got-(v0+RCSDvQuantum)) > 1e-9 {
		t.Errorf("post-prograde-pulse |v| = %.6f, want %.6f", got, v0+RCSDvQuantum)
	}
}

// TestApplyRCSPulseFineLevelScalesDV: a pulse at fine level N must
// change |v| by RCSDvQuantum/10^N, and an out-of-range level falls
// back to the coarse quantum.
func TestApplyRCSPulseFineLevelScalesDV(t *testing.T) {
	systems, _ := bodies.LoadAll()
	earth := systems[0].FindBody("Earth")

	cases := []struct {
		level  int
		wantDV float64
	}{
		{0, 0.1},
		{1, 0.01},
		{2, 0.001},
		{RCSFineLevels, 0.1}, // out of range → coarse fallback
		{-1, 0.1},            // negative → coarse fallback
	}
	for _, c := range cases {
		sc := NewInLEO(*earth)
		sc.RCSFineLevel = c.level
		if got := sc.RCSPulseDV(); math.Abs(got-c.wantDV) > 1e-12 {
			t.Errorf("level %d: RCSPulseDV() = %g, want %g", c.level, got, c.wantDV)
		}
		v0 := sc.OrbitalSpeed()
		if !sc.ApplyRCSPulse(BurnPrograde) {
			t.Fatalf("level %d: ApplyRCSPulse returned false on a fueled craft", c.level)
		}
		if got := sc.OrbitalSpeed(); math.Abs(got-(v0+c.wantDV)) > 1e-9 {
			t.Errorf("level %d: Δ|v| = %g, want %g", c.level, got-v0, c.wantDV)
		}
	}
}

// TestApplyRCSPulseConsumesMonoprop: every pulse should debit the
// monoprop pool by some positive amount and never go negative.
func TestApplyRCSPulseConsumesMonoprop(t *testing.T) {
	systems, _ := bodies.LoadAll()
	earth := systems[0].FindBody("Earth")
	sc := NewInLEO(*earth)
	m0 := sc.Monoprop

	for i := 0; i < 10; i++ {
		if !sc.ApplyRCSPulse(BurnPrograde) {
			t.Fatalf("pulse %d returned false unexpectedly", i)
		}
	}
	if sc.Monoprop >= m0 {
		t.Errorf("monoprop didn't decrease: %.6f → %.6f", m0, sc.Monoprop)
	}
	if sc.Monoprop < 0 {
		t.Errorf("monoprop went negative: %.6f", sc.Monoprop)
	}
}

// TestApplyRCSPulseExhaustionStops: once monoprop is empty, further
// pulses must be no-ops (return false, |v| unchanged).
func TestApplyRCSPulseExhaustionStops(t *testing.T) {
	systems, _ := bodies.LoadAll()
	earth := systems[0].FindBody("Earth")
	sc := NewInLEO(*earth)
	sc.Monoprop = 0

	v0 := sc.OrbitalSpeed()
	if sc.ApplyRCSPulse(BurnPrograde) {
		t.Error("ApplyRCSPulse on empty tank returned true")
	}
	if sc.OrbitalSpeed() != v0 {
		t.Errorf("|v| changed on no-op pulse: %v → %v", v0, sc.OrbitalSpeed())
	}
}

// TestRCSDeltaVMatchesRocketEquation: with full monoprop, RCSDeltaV
// must equal Isp·g₀·ln((m_dry+m_fuel+m_mono)/(m_dry+m_fuel)).
func TestRCSDeltaVMatchesRocketEquation(t *testing.T) {
	systems, _ := bodies.LoadAll()
	earth := systems[0].FindBody("Earth")
	sc := NewInLEO(*earth)

	mFloor := sc.DryMass + sc.Fuel
	mTop := mFloor + sc.Monoprop
	want := sc.RCSIsp * 9.80665 * math.Log(mTop/mFloor)
	got := sc.RCSDeltaV()
	if math.Abs(got-want) > 1e-6 {
		t.Errorf("RCSDeltaV = %.6f, want %.6f", got, want)
	}
}

// TestDefaultRCSLoadoutLinearScaling: a craft with twice the dry
// mass should get twice the monoprop capacity and twice the thrust;
// Isp stays fixed.
func TestDefaultRCSLoadoutLinearScaling(t *testing.T) {
	mp1, cap1, thr1, isp1 := DefaultRCSLoadout(11000)
	mp2, cap2, thr2, isp2 := DefaultRCSLoadout(22000)

	if math.Abs(cap2-2*cap1) > 1e-9 {
		t.Errorf("capacity scaling: %.3f vs 2·%.3f", cap2, cap1)
	}
	if math.Abs(thr2-2*thr1) > 1e-9 {
		t.Errorf("thrust scaling: %.3f vs 2·%.3f", thr2, thr1)
	}
	if mp1 != cap1 || mp2 != cap2 {
		t.Error("monoprop should ship full")
	}
	if isp1 != isp2 {
		t.Errorf("Isp should be invariant: %.3f vs %.3f", isp1, isp2)
	}
}
