package sim

// apollo_ascent_probe_test.go is a DIAGNOSTIC harness (not a CI assertion):
// it flies a gravity-turn ascent of the Apollo Stack through the real force
// models (physics.Accel + physics.DragAccel, RK4, real catalog masses / Isp
// / ballistic coefficients, serial staging) to a 200 km circular parking
// orbit and reports the S-IVB's remaining Δv at park — the number that
// decides whether TLI (≈3133 m/s) can complete.
//
// The ascent simulation itself lives in apollo_probe_helpers_test.go
// (flyApolloAscent / bestApolloAscent), shared with the lunar-arc budget
// probe so the two can't drift.
//
// Run: go test ./internal/sim -run TestApolloAscentBudgetProbe -v
// It always passes; read the t.Log output.

import (
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/physics"
)

func TestApolloAscentBudgetProbe(t *testing.T) {
	earth, ok := loadEarth()
	if !ok {
		t.Fatal("earth not found")
	}
	mu := earth.GravitationalParameter()
	Re := earth.RadiusMeters()
	targetAlt := 200e3
	rTarget := Re + targetAlt

	// Apollo-Stack lower stack, byte-identical to loadouts.go.
	stages := []apolloStage{
		{130000, 2160000, 35100000, 263, 8e-6}, // S-IC
		{40000, 440000, 5140000, 421, 2.5e-5},  // S-II
		{11000, 109000, 1023000, 421, 6.25e-5}, // S-IVB
	}
	const payload = 45300.0 // LM(Descent+Ascent) + CSM wet, dead mass above S-IVB

	t.Logf("Apollo Stack gravity-turn ascent to %.0f km circular (real force models):", targetAlt/1000)
	fullSIVB := rocketDv(stages[2].isp, stages[2].dry+stages[2].fuel+payload, stages[2].dry+payload)
	t.Logf("  full S-IVB Δv (pushing LM+CSM) = %.0f m/s; TLI from 200 km needs ≈ 3133 m/s", fullSIVB)

	best := apolloAscentResult{remain: -1}
	for vt := 1400.0; vt <= 3200.0; vt += 100.0 {
		res := flyApolloAscent(earth, stages, payload, vt, targetAlt)
		if res.reached {
			t.Logf("  vTarget %5.0f → park %6.1f km  S-IVB remaining Δv = %6.0f m/s  (TLI margin %+6.0f)", vt, res.apoKm, res.remain, res.remain-3133)
			if res.remain > best.remain {
				best = res
			}
		} else {
			t.Logf("  vTarget %5.0f → FAILED (ran dry; peak apo %.0f km)", vt, res.maxApoKm)
		}
	}
	if best.remain < 0 {
		t.Logf("RESULT: no swept ascent reached a 200 km park — stack is short before TLI even begins.")
		return
	}
	vOrb := math.Sqrt(mu / rTarget)
	rotBonus := physics.AtmosphereOmega(earth).Norm() * Re // equatorial co-rotation start speed
	losses := best.expended - (vOrb - rotBonus)
	t.Logf("BEST ASCENT (vTarget=%.0f): park %.0f km, S-IVB has %.0f m/s remaining.", best.vTarget, best.apoKm, best.remain)
	t.Logf("  ascent expended %.0f m/s to orbit; v_orbit=%.0f, rotation bonus=%.0f → gravity+drag+steering loss ≈ %.0f m/s", best.expended, vOrb, rotBonus, losses)
	t.Logf("  (textbook-optimal loss for a TWR≈1.2 vehicle is ~1600–1800 m/s; excess above that is this harness's open-loop guidance, not the stack)")
	t.Logf("  TLI needs ≈3133 m/s → margin %+.0f m/s (%s).", best.remain-3133,
		map[bool]string{true: "MAKES IT", false: "SHORT by this ascent"}[best.remain >= 3133])
}
