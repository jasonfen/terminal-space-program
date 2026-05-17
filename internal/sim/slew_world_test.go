package sim

import (
	"math"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

func slewAngle(a, b orbital.Vec3) float64 {
	c := a.Unit().Dot(b.Unit())
	if c > 1 {
		c = 1
	} else if c < -1 {
		c = -1
	}
	return math.Acos(c)
}

// Slew must integrate the same total attitude per total sim-time
// regardless of how that sim-time is delivered (one big tick vs many
// small ticks) — the warp-invariance guarantee. Uses BurnNormalPlus on
// the default equatorial circular orbit, whose orbit normal is an
// inertially-fixed commanded direction, so the only variable is
// elapsed sim-time.
func TestSlewWarpInvariant(t *testing.T) {
	mkCraft := func() (*World, *spacecraft.Spacecraft) {
		w, err := NewWorld()
		if err != nil {
			t.Fatalf("NewWorld: %v", err)
		}
		c := w.Crafts[0]
		c.AttitudeMode = spacecraft.BurnNormalPlus
		c.SlewRateDegPerSec = 2                 // 2°/s → 20° over 10 s (won't converge)
		c.CurrentAttitudeDir = c.State.V.Unit() // ⟂ orbit normal: 90° off
		return w, c
	}

	wA, cA := mkCraft()
	wA.integrateOneCraft(cA, 10*time.Second)

	wB, cB := mkCraft()
	for i := 0; i < 10; i++ {
		wB.integrateOneCraft(cB, 1*time.Second)
	}

	if d := slewAngle(cA.CurrentAttitudeDir, cB.CurrentAttitudeDir); d > 1e-6 {
		t.Errorf("warp-variant slew: 1×10s vs 10×1s differ by %.3e rad\n  A=%+v\n  B=%+v",
			d, cA.CurrentAttitudeDir, cB.CurrentAttitudeDir)
	}
	// Sanity: it actually slewed but did NOT converge (still rate-limited).
	moved := math.Pi/2 - slewAngle(cA.CurrentAttitudeDir, normalOf(cA))
	if moved < 0.30 || moved > 0.40 { // ~20° ≈ 0.349 rad
		t.Errorf("expected ~0.349 rad of slew over 10 s, got %.4f", moved)
	}
}

func normalOf(c *spacecraft.Spacecraft) orbital.Vec3 {
	return c.State.R.Cross(c.State.V).Unit()
}

// The slew integrator must run BEFORE the Kepler fast-path gate: a
// coasting craft at warp>1 takes the analytic early-return, so a slew
// integrator placed after the sub-step loop would never run and the
// nose would freeze mid-slew. This locks the placement.
func TestSlewRunsDuringKeplerCoast(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.Crafts[0]
	w.Clock.WarpIdx = 2 // 100× → Warp() > 1, no burn ⇒ canKeplerStep true

	if !w.canKeplerStep(c, 10*time.Second) {
		t.Fatal("precondition failed: expected canKeplerStep true (coasting, warp>1)")
	}

	c.AttitudeMode = spacecraft.BurnNormalPlus
	c.SlewRateDegPerSec = 2
	c.CurrentAttitudeDir = c.State.V.Unit() // 90° off the orbit normal
	before := slewAngle(c.CurrentAttitudeDir, normalOf(c))

	w.integrateOneCraft(c, 10*time.Second) // takes the Kepler early-return

	after := slewAngle(c.CurrentAttitudeDir, normalOf(c))
	if after >= before-1e-6 {
		t.Fatalf("slew did not run on the Kepler coast path: angle %.4f -> %.4f", before, after)
	}
	if after < 1e-6 {
		t.Fatalf("slew snapped (not rate-limited) on coast path: angle %.4f", after)
	}
	// ~20° of progress (2°/s × 10 s), not a snap.
	if d := (before - after) - 0.349; math.Abs(d) > 0.02 {
		t.Errorf("coast slew progress %.4f rad, want ≈0.349 (20°)", before-after)
	}
}

// thrustDV isolates the thrust contribution over one integrate by
// subtracting a gravity-only baseline run of an identical craft.
func thrustDV(t *testing.T, dir orbital.Vec3, instant bool, dt time.Duration) orbital.Vec3 {
	t.Helper()
	// Burning craft.
	wB, _ := NewWorld()
	cB := wB.Crafts[0]
	wB.InstantSAS = instant
	cB.AttitudeMode = spacecraft.BurnPrograde
	cB.SlewRateDegPerSec = 1e-9 // freeze: nose stays put for the tick
	cB.CurrentAttitudeDir = dir.Unit()
	v0 := cB.State.V
	wB.StartManualBurn()
	wB.integrateOneCraft(cB, dt)
	dvBurn := cB.State.V.Sub(v0)

	// Gravity-only baseline (no burn), identical start.
	wG, _ := NewWorld()
	cG := wG.Crafts[0]
	vg0 := cG.State.V
	wG.integrateOneCraft(cG, dt)
	dvGrav := cG.State.V.Sub(vg0)

	return dvBurn.Sub(dvGrav)
}

// Default (slew) thrust must follow the craft's physical nose
// (CurrentAttitudeDir), not the commanded BurnMode — set the nose
// orthogonal to prograde and confirm Δv goes along the nose.
func TestBurnThrustsAlongCurrentAttitudeDir(t *testing.T) {
	w, _ := NewWorld()
	c := w.Crafts[0]
	nose := c.State.R.Cross(c.State.V).Unit() // orbit normal ⟂ prograde

	dv := thrustDV(t, nose, false, 1*time.Second)
	if dv.Norm() < 1.0 {
		t.Fatalf("no measurable thrust Δv: %+v", dv)
	}
	if a := slewAngle(dv, nose); a > 0.02 { // ≈1°
		t.Errorf("thrust Δv not along nose: %.4f rad off (dv=%+v nose=%+v)", a, dv, nose)
	}
}

// Burning while the nose lags the commanded direction bleeds Δv to
// cosine loss: along-commanded component scales as cos(lag).
func TestCosineLossWhenNoseLags(t *testing.T) {
	w, _ := NewWorld()
	c := w.Crafts[0]
	pro := c.State.V.Unit()
	nrm := c.State.R.Cross(c.State.V).Unit()
	// 60° off prograde, in the prograde–normal plane.
	lag := orbital.Rotate(pro, nrm, 60*math.Pi/180)

	aligned := thrustDV(t, pro, false, 1*time.Second).Dot(pro)
	lagged := thrustDV(t, lag, false, 1*time.Second).Dot(pro)

	want := aligned * math.Cos(60*math.Pi/180) // ≈ 0.5 × aligned
	if math.Abs(lagged-want) > 0.02*aligned {
		t.Errorf("cosine loss off: lagged along-prograde=%.3f, want ≈%.3f (aligned=%.3f)",
			lagged, want, aligned)
	}
}

// InstantSAS must ignore CurrentAttitudeDir and thrust along the
// commanded mode (legacy behaviour) even when the nose points
// elsewhere.
func TestInstantSASIgnoresCurrentAttitudeDir(t *testing.T) {
	w, _ := NewWorld()
	c := w.Crafts[0]
	pro := c.State.V.Unit()
	nrm := c.State.R.Cross(c.State.V).Unit() // bogus nose ⟂ prograde

	dv := thrustDV(t, nrm, true /*InstantSAS*/, 1*time.Second)
	if a := slewAngle(dv, pro); a > 0.02 {
		t.Errorf("InstantSAS thrust not along commanded prograde: %.4f rad off", a)
	}
}

// Lead-compensated node firing: with rate-limited slew the craft must
// auto-orient BEFORE BurnStart so it is aligned at ignition and the
// planted node delivers its full Δv (no cosine loss) — i.e. the slew
// outcome matches the legacy instant path. A 180° flip (prograde →
// retrograde node) at the 5°/s default needs 36 s; the lead window
// (1.25·36 ≈ 45 s) opens well before BurnStart.
func TestLeadCompNodeAlignedAtT0(t *testing.T) {
	mk := func(instant bool) *World {
		w, _ := NewWorld()
		w.InstantSAS = instant
		c := w.Crafts[0]
		c.CurrentAttitudeDir = c.State.V.Unit() // start prograde (180° off retro)
		now := w.Clock.SimTime
		w.PlanNode(ManeuverNode{
			TriggerTime: now.Add(200 * time.Second),
			Mode:        spacecraft.BurnRetrograde,
			DV:          30,
			Duration:    20 * time.Second,
			PrimaryID:   c.Primary.ID,
		})
		w.Clock.WarpIdx = 2 // 100× → ~5 s/tick
		return w
	}

	wSlew := mk(false)
	wInst := mk(true)

	var atFireAngle = math.Pi // sentinel
	firedSlew := false
	for i := 0; i < 400; i++ {
		// Capture the slew craft's nose-vs-retrograde angle on the
		// tick the burn first becomes active (ignition).
		cs := wSlew.Crafts[0]
		preActive := cs.ActiveBurn != nil
		wSlew.Tick()
		wInst.Tick()
		if !preActive && cs.ActiveBurn != nil && !firedSlew {
			retro := cs.State.V.Unit().Scale(-1)
			atFireAngle = slewAngle(cs.CurrentAttitudeDir, retro)
			firedSlew = true
		}
		if wSlew.Crafts[0].Nodes == nil && wInst.Crafts[0].Nodes == nil &&
			wSlew.Crafts[0].ActiveBurn == nil && wInst.Crafts[0].ActiveBurn == nil &&
			firedSlew && i > 20 {
			break
		}
	}

	if !firedSlew {
		t.Fatal("node never fired within tick budget")
	}
	if atFireAngle > 6*math.Pi/180 { // converged within ~6° at ignition
		t.Errorf("not lead-aligned at ignition: %.2f° off retrograde",
			atFireAngle*180/math.Pi)
	}

	// Δv preserved: the slew outcome must match the legacy instant
	// (perfectly-aligned) burn. Compare specific orbital energy.
	en := func(w *World) float64 {
		c := w.Crafts[0]
		mu := c.Primary.GravitationalParameter()
		return c.State.V.Norm()*c.State.V.Norm()/2 - mu/c.State.R.Norm()
	}
	eS, eI := en(wSlew), en(wInst)
	if rel := math.Abs(eS-eI) / math.Abs(eI); rel > 2e-3 {
		t.Errorf("lead-comp Δv not preserved vs instant baseline: "+
			"energy rel diff %.2e (slew=%.1f instant=%.1f)", rel, eS, eI)
	}
}

// InstantSAS opt-out must skip the slew integrator entirely (legacy
// behaviour: CurrentAttitudeDir untouched by the integrator).
func TestInstantSASSkipsSlew(t *testing.T) {
	w, _ := NewWorld()
	c := w.Crafts[0]
	w.InstantSAS = true
	c.AttitudeMode = spacecraft.BurnNormalPlus
	c.SlewRateDegPerSec = 2
	start := c.State.V.Unit()
	c.CurrentAttitudeDir = start

	w.integrateOneCraft(c, 10*time.Second)

	if d := c.CurrentAttitudeDir.Sub(start).Norm(); d > 1e-12 {
		t.Errorf("InstantSAS still slewed CurrentAttitudeDir (Δ=%.3e)", d)
	}
}
