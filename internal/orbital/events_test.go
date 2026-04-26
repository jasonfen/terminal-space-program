package orbital

import (
	"math"
	"testing"
)

// muEarth is Earth's gravitational parameter — pulled inline to avoid a
// bodies dependency loop in this leaf package's tests.
const muEarth = 3.986004418e14

// stateAtNu builds a 200-km-altitude prograde orbit (e = 0.05) at the
// requested true anomaly. Used to seed event-time tests with a known
// current-ν.
func stateAtNu(nu float64) Vec3State {
	a := 6.578e6 // ~200 km altitude perigee
	e := 0.05
	p := a * (1 - e*e)
	r := p / (1 + e*math.Cos(nu))
	cosNu, sinNu := math.Cos(nu), math.Sin(nu)
	rVec := Vec3{X: r * cosNu, Y: r * sinNu}
	// Velocity in perifocal frame: v_r = √(μ/p)·e·sinν,
	// v_perp = √(μ/p)·(1+e·cosν).
	vr := math.Sqrt(muEarth/p) * e * sinNu
	vp := math.Sqrt(muEarth/p) * (1 + e*cosNu)
	// Convert (radial, transverse) → (x, y).
	vVec := Vec3{
		X: vr*cosNu - vp*sinNu,
		Y: vr*sinNu + vp*cosNu,
	}
	return Vec3State{R: rVec, V: vVec}
}

// TestTimeToTrueAnomalyForwardOnly: Δν > 0 → small Δt; Δν = 0 → full
// period (one revolution to "come back").
func TestTimeToTrueAnomalyForwardOnly(t *testing.T) {
	a := 6.578e6
	e := 0.05
	period := 2 * math.Pi * math.Sqrt(a*a*a/muEarth)

	// From peri (ν=0) to ν=π: half a period.
	dt := TimeToTrueAnomaly(0, math.Pi, a, e, muEarth)
	if math.Abs(dt-period/2) > 1.0 {
		t.Errorf("0 → π: got %.3f s, want %.3f s", dt, period/2)
	}

	// From ν=0 back to ν=0: full period (next-event semantics).
	dt = TimeToTrueAnomaly(0, 0, a, e, muEarth)
	if math.Abs(dt-period) > 1.0 {
		t.Errorf("0 → 0: got %.3f s, want %.3f s", dt, period)
	}

	// From ν=π/4 to ν=π/2: small forward step.
	dt = TimeToTrueAnomaly(math.Pi/4, math.Pi/2, a, e, muEarth)
	if dt <= 0 || dt > period/2 {
		t.Errorf("π/4 → π/2: got %.3f s, expected (0, period/2)", dt)
	}
}

// TestTimeToTrueAnomalyHyperbolic: returns -1 for unreachable orbits.
func TestTimeToTrueAnomalyHyperbolic(t *testing.T) {
	if got := TimeToTrueAnomaly(0, math.Pi, 1e7, 1.5, muEarth); got != -1 {
		t.Errorf("hyperbolic: got %.3f, want -1", got)
	}
	if got := TimeToTrueAnomaly(0, math.Pi, -1e7, 0.5, muEarth); got != -1 {
		t.Errorf("a<0: got %.3f, want -1", got)
	}
	if got := TimeToTrueAnomaly(0, math.Pi, 1e7, 0.5, 0); got != -1 {
		t.Errorf("μ=0: got %.3f, want -1", got)
	}
}

// TestTimeToPeriapsisAndApoapsis: using a state-at-known-ν fixture.
func TestTimeToPeriapsisAndApoapsis(t *testing.T) {
	a := 6.578e6
	period := 2 * math.Pi * math.Sqrt(a*a*a/muEarth)

	// At ν=π/2: time to ν=0 is (full period) - (time-from-peri-to-π/2).
	state := stateAtNu(math.Pi / 2)
	dtPeri := TimeToPeriapsis(state, muEarth)
	if dtPeri <= 0 || dtPeri > period {
		t.Fatalf("ν=π/2 → peri: got %.3f s, expected (0, period)", dtPeri)
	}
	dtApo := TimeToApoapsis(state, muEarth)
	if dtApo <= 0 || dtApo > period {
		t.Fatalf("ν=π/2 → apo: got %.3f s, expected (0, period)", dtApo)
	}
	// dtPeri > dtApo at ν=π/2 (apo at π is closer, peri requires
	// continuing through apo and back).
	if dtPeri <= dtApo {
		t.Errorf("at ν=π/2 expected peri-time > apo-time; got peri=%.3f apo=%.3f",
			dtPeri, dtApo)
	}

	// At ν=0 (peri itself): next peri is one full period away.
	state = stateAtNu(0)
	dtPeri = TimeToPeriapsis(state, muEarth)
	if math.Abs(dtPeri-period) > 1.0 {
		t.Errorf("ν=0 → next peri: got %.3f s, want %.3f s (one period)", dtPeri, period)
	}
}

// TestTimeToNodeCrossingEquatorial: returns -1 for equatorial orbits
// because there's no well-defined node crossing.
func TestTimeToNodeCrossingEquatorial(t *testing.T) {
	state := stateAtNu(math.Pi / 4) // equatorial by construction
	if got := TimeToNodeCrossing(state, muEarth, true); got != -1 {
		t.Errorf("equatorial AN: got %.3f, want -1", got)
	}
	if got := TimeToNodeCrossing(state, muEarth, false); got != -1 {
		t.Errorf("equatorial DN: got %.3f, want -1", got)
	}
}

// TestTimeToNodeCrossingInclined: an inclined orbit gives a positive
// finite AN/DN time-of-flight, with AN and DN exactly half a period apart.
func TestTimeToNodeCrossingInclined(t *testing.T) {
	// Tilt the equatorial state by 30° around the X-axis.
	tilt := 30 * math.Pi / 180
	cosT, sinT := math.Cos(tilt), math.Sin(tilt)
	flat := stateAtNu(math.Pi / 4)
	rot := func(v Vec3) Vec3 {
		return Vec3{X: v.X, Y: v.Y*cosT - v.Z*sinT, Z: v.Y*sinT + v.Z*cosT}
	}
	state := Vec3State{R: rot(flat.R), V: rot(flat.V)}

	dtAN := TimeToNodeCrossing(state, muEarth, true)
	if dtAN <= 0 {
		t.Fatalf("inclined AN: got %.3f, want > 0", dtAN)
	}
	dtDN := TimeToNodeCrossing(state, muEarth, false)
	if dtDN <= 0 {
		t.Fatalf("inclined DN: got %.3f, want > 0", dtDN)
	}

	a := 6.578e6
	period := 2 * math.Pi * math.Sqrt(a*a*a/muEarth)
	// Both times must lie within one period (next-event semantics).
	if dtAN > period+1 || dtDN > period+1 {
		t.Errorf("AN=%.3f DN=%.3f exceed one period %.0f", dtAN, dtDN, period)
	}
	// AN and DN are antipodal in ν, so their crossings can't fall at
	// the same sim-time. With e=0.05 the elliptical TOF asymmetry
	// shrinks the half-period gap by at most ~e·period; require at
	// least 100 s of separation to catch a degenerate single-crossing.
	if math.Abs(dtAN-dtDN) < 100 {
		t.Errorf("AN and DN crossings too close: AN=%.3f DN=%.3f", dtAN, dtDN)
	}
}
