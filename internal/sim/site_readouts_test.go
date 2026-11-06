package sim

import (
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
)

const moonR = 1737400.0

func TestApollo12ToSurveyor3(t *testing.T) {
	// ADR 0066 decision 9 worked example (LROC coordinates).
	lat1, lon1, lat2, lon2 := -3.01239, -23.42157, -3.0159, -23.4181
	d := HaversineM(lat1, lon1, lat2, lon2, moonR)
	az := ForwardAzimuthDeg(lat1, lon1, lat2, lon2)
	if math.Abs(d-150) > 1 {
		t.Errorf("ground = %.1f m, want about 150", d)
	}
	if math.Abs(az-135) > 1 {
		t.Errorf("bearing = %.1f deg, want about 135", az)
	}
	// the equal point: zero distance (bearing is dashed by LandBesideReading)
	if got := HaversineM(lat1, lon1, lat1, lon1, moonR); got != 0 {
		t.Errorf("equal point distance = %v, want 0", got)
	}
}

func TestImpactOffsetSignFlipsAcrossSite(t *testing.T) {
	// flying due east along the equator, sub-point at lon 0, site at lon 1.
	m := func(impLon, impLat float64) (float64, float64, bool) {
		return ImpactOffsetM(0, 0, impLat, impLon, 0, 1, moonR)
	}
	long, cross, ok := m(1.1, 0)
	if !ok || long <= 0 || math.Abs(cross) > 1 {
		t.Errorf("beyond: long=%v cross=%v ok=%v, want long>0 cross~0", long, cross, ok)
	}
	if long, _, _ := m(0.9, 0); long >= 0 {
		t.Errorf("short of the site: long=%v, want <0", long)
	}
	if long, cross, ok := m(1, 0); !ok || long != 0 || cross != 0 {
		t.Errorf("equal point: long=%v cross=%v ok=%v, want 0 0 true", long, cross, ok)
	}
	// south of an eastbound track is the right-hand side
	if _, cross, _ := m(1, -0.1); cross <= 0 {
		t.Errorf("right of track: cross=%v, want >0", cross)
	}
	if _, cross, _ := m(1, 0.1); cross >= 0 {
		t.Errorf("left of track: cross=%v, want <0", cross)
	}
	if _, _, ok := ImpactOffsetM(0, 0, 0, 0, 0, 1, moonR); ok {
		t.Error("impact at the sub-point has no track, want ok=false")
	}
}

func TestLookDownWorkedExample(t *testing.T) {
	// 4.0 km up, 11.9 km out, nose 10 deg off the zenith: LOS 18.6 below, lpd 8.6 -> 9.
	r := orbital.Vec3{Z: moonR + 4000}
	site := r.Add(orbital.Vec3{X: 11900, Z: -4000})
	ten := 10 * math.Pi / 180
	got, ok := LookDownDeg(r, site, orbital.Vec3{X: math.Sin(ten), Z: math.Cos(ten)})
	if !ok || math.Round(got) != 9 || math.Abs(got-8.6) > 0.1 {
		t.Errorf("lpd = %v ok=%v, want 8.6 (reads 9)", got, ok)
	}
	// equal case: nose on the zenith, lpd is the bare line-of-sight depression
	got, _ = LookDownDeg(r, site, orbital.Vec3{Z: 1})
	if math.Abs(got-math.Atan(4.0/11.9)/rad) > 1e-9 {
		t.Errorf("zenith nose lpd = %v, want atan(4/11.9)", got)
	}
}

// Ground and bearing read from orbit; miss, cross and lpd dash until the
// trajectory meets the ground (decision 10).
func TestLandBesideReadingDashesUntilGroundFound(t *testing.T) {
	w, lander, _ := siteWorld(t)
	lander.Landed = false
	lander.Crashed = false
	// circular, level: never meets the ground inside the horizon
	r := lander.Primary.RadiusMeters() + 100000
	mu := lander.Primary.GravitationalParameter()
	lander.State.R = orbital.Vec3{X: r}
	lander.State.V = orbital.Vec3{Y: math.Sqrt(mu / r)}
	lb, ok := w.LandBesideReading()
	if ok {
		t.Fatalf("no target yet, reading must be absent: %+v", lb)
	}
	w.SetTargetSite(w.Sites[0].ID)
	lb, ok = w.LandBesideReading()
	if !ok || lb.GroundM <= 0 || !lb.BearingOK {
		t.Fatalf("ground/bearing must read from orbit: %+v ok=%v", lb, ok)
	}
	if lb.ImpactOK || lb.LPDOK || lb.MissOK {
		t.Errorf("miss/cross/lpd must be dashes before the ground is found: %+v", lb)
	}
	// drop the speed: the path now meets the ground, and the rows fill in
	lander.State.V = orbital.Vec3{Y: 0.2 * math.Sqrt(mu/r)}
	lander.CurrentAttitudeDir = orbital.Vec3{X: 1}
	lb, _ = w.LandBesideReading()
	if !lb.ImpactOK || !lb.MissOK || !lb.LPDOK {
		t.Errorf("miss/cross/lpd must fill in once PredictImpact finds the ground: %+v", lb)
	}
}

func TestLandBesideEqualPointHasNoBearing(t *testing.T) {
	w, lander, _ := siteWorld(t)
	lander.Landed = false
	w.SetTargetSite(w.Sites[0].ID)
	rs, _ := surfacePointState(lander.Primary, w.Sites[0].LatDeg, w.Sites[0].LonDeg, w.Clock.SimTime)
	lander.State.R = rs.Scale((rs.Norm() + 100000) / rs.Norm()) // straight above the site
	lander.State.V = orbital.Vec3{}
	lb, ok := w.LandBesideReading()
	if !ok || lb.GroundM > 1e-3 || lb.BearingOK {
		t.Errorf("above the site: ground=%v bearingOK=%v, want 0 and a dash", lb.GroundM, lb.BearingOK)
	}
}
