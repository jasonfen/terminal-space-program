package sim

import (
	"errors"
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/planner"
)

// TestPlanVesselPlaneMatchOnNodeLine: a vessel sitting on (or a hair either
// side of) the line where its plane meets the target vessel's plane must
// still get a plane-match node, never "already at target inclination"
// (loose end 2 / 2026-10-12 handoff).
func TestPlanVesselPlaneMatchOnNodeLine(t *testing.T) {
	for _, offDeg := range []float64{0, 1e-9, -1e-9, 1e-6, -1e-6, 1e-3, -1e-3, 0.5, -0.5, 180, 180.0001, 179.9999} {
		w := mustWorld(t)
		if _, err := w.SpawnCraft(SpawnSpec{AltitudeM: 350e3}); err != nil {
			t.Fatalf("SpawnCraft: %v", err)
		}
		w.ActiveCraftIdx = 0
		active, target := w.Crafts[0], w.Crafts[1]
		mu := active.Primary.GravitationalParameter()
		frame := orbital.ReferenceFrameForPrimary(active.Primary)
		r := active.Primary.RadiusMeters() + 300e3
		active.State.R, active.State.V = circularStateAtIncRAAN(frame, mu, r, 51.6, 0)
		target.State.R, target.State.V = circularStateAtIncRAAN(frame, mu, r+50e3, 51.6, 90)
		active.Nodes, target.Nodes = nil, nil

		hA := active.State.R.Cross(active.State.V).Unit()
		hT := target.State.R.Cross(target.State.V).Unit()
		line := hA.Cross(hT).Unit() // mutual node line
		// Put the active vessel on the line, then slide it offDeg along its own orbit.
		v := math.Sqrt(mu / r)
		ang := offDeg * math.Pi / 180
		rHat := line.Scale(math.Cos(ang)).Add(hA.Cross(line).Scale(math.Sin(ang)))
		active.State.R = rHat.Scale(r)
		active.State.V = hA.Cross(rHat).Scale(v)

		w.SetTargetCraft(1)
		plan, err := w.PlanVesselPlaneMatch()
		if err != nil {
			if errors.Is(err, planner.ErrInclinationNoOp) {
				t.Errorf("off=%g°: refused as coplanar although planes differ", offDeg)
			} else {
				t.Errorf("off=%g°: %v", offDeg, err)
			}
			continue
		}
		post, _ := postBurnImpulsive(w, active.Nodes[0])
		if a := planeAngleDeg(post.R.Cross(post.V), target.State.R.Cross(target.State.V)); a > 0.5 {
			t.Errorf("off=%g°: post-burn plane %.2f° off target (offset %v dv %.0f)", offDeg, a, plan.OffsetTime, plan.DV)
		}
	}
}

// stateFromElements builds a primary-frame (R, V) for classical elements,
// expressed in the world frame (standard 3-1-3 rotation).
func stateFromElements(frame orbital.BodyFrame, mu, a, e, inc, raan, argp, nu float64) (orbital.Vec3, orbital.Vec3) {
	p := a * (1 - e*e)
	r := p / (1 + e*math.Cos(nu))
	rp := orbital.Vec3{X: r * math.Cos(nu), Y: r * math.Sin(nu)}
	k := math.Sqrt(mu / p)
	vp := orbital.Vec3{X: -k * math.Sin(nu), Y: k * (e + math.Cos(nu))}
	rot := func(v orbital.Vec3) orbital.Vec3 {
		cO, sO, ci, si, cw, sw := math.Cos(raan), math.Sin(raan), math.Cos(inc), math.Sin(inc), math.Cos(argp), math.Sin(argp)
		return orbital.Vec3{
			X: (cO*cw-sO*sw*ci)*v.X + (-cO*sw-sO*cw*ci)*v.Y,
			Y: (sO*cw+cO*sw*ci)*v.X + (-sO*sw+cO*cw*ci)*v.Y,
			Z: (sw*si)*v.X + (cw*si)*v.Y,
		}
	}
	return frame.ToWorld(rot(rp)), frame.ToWorld(rot(vp))
}

// TestPlanVesselPlaneMatchNeverFalseCoplanarSweep: across circular and
// eccentric active orbits and every phase, including the two node-line
// crossings, a >0.2 degree dihedral never reads as coplanar.
func TestPlanVesselPlaneMatchNeverFalseCoplanarSweep(t *testing.T) {
	w := mustWorld(t)
	if _, err := w.SpawnCraft(SpawnSpec{AltitudeM: 350e3}); err != nil {
		t.Fatal(err)
	}
	w.ActiveCraftIdx = 0
	active, target := w.Crafts[0], w.Crafts[1]
	mu := active.Primary.GravitationalParameter()
	frame := orbital.ReferenceFrameForPrimary(active.Primary)
	rp := active.Primary.RadiusMeters()
	target.State.R, target.State.V = stateFromElements(frame, mu, rp+400e3, 0, 0.9, 1.2, 0, 0.3)
	hT := target.State.R.Cross(target.State.V).Unit()
	w.SetTargetCraft(1)
	// Instrument check: a genuinely coplanar vessel must still be refused.
	active.State.R, active.State.V = stateFromElements(frame, mu, rp+300e3, 0, 0.9, 1.2, 0, 2.0)
	if _, err := w.PlanVesselPlaneMatch(); !errors.Is(err, planner.ErrInclinationNoOp) {
		t.Fatalf("coplanar vessel: err = %v, want ErrInclinationNoOp (instrument cannot return a positive)", err)
	}
	n, bad := 0, 0
	for _, e := range []float64{0, 0.05, 0.3} {
		for _, inc := range []float64{0.1, 0.9, 1.6, 2.5} {
			for _, raan := range []float64{0, 1.2, 3} {
				for _, argp := range []float64{0, 1, 4} {
					for k := 0; k < 12; k++ {
						nu := float64(k) * math.Pi / 6
						active.Nodes = nil
						active.State.R, active.State.V = stateFromElements(frame, mu, (rp+600e3)/(1-e), e, inc, raan, argp, nu)
						hA := active.State.R.Cross(active.State.V).Unit()
						if math.Acos(math.Max(-1, math.Min(1, hA.Dot(hT)))) < 0.2*math.Pi/180 {
							continue
						}
						n++
						if _, err := w.PlanVesselPlaneMatch(); err != nil {
							bad++
							if bad < 6 {
								t.Errorf("e=%g inc=%g raan=%g argp=%g nu=%g: %v", e, inc, raan, argp, nu, err)
							}
						}
					}
				}
			}
		}
	}
	if n < 300 {
		t.Fatalf("sweep only exercised %d states; instrument too narrow", n)
	}
	t.Logf("swept %d states, %d refused", n, bad)
}
