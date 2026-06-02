package sim

// apollo_lunar_budget_test.go is a DIAGNOSTIC harness (not a CI assertion):
// it evaluates the FULL post-transposition lunar arc per stage and reports
// each stage's Δv margin, so a mass rebalance can be swept against real
// numbers (ADR 0009).
//
// The arc, after transposition makes the CSM/SM the firing core with the LM
// as a releasable nose payload:
//
//   S-IVB : TLI solo (from the parking orbit the ascent helper delivers)
//   SM    : mid-course corrections + LOI (pushing CSM+LM) THEN TEI (bare CSM)
//   DPS   : lunar descent (pushing the LM down)
//   APS   : lunar ascent + rendezvous (pushing the ascent stage to orbit)
//   CM    : passive parachute re-entry (no Δv)
//
// The SM budget is PHASED: corrections+LOI burn at the heavy LM-attached
// mass, but TEI burns at the light bare-CSM mass after the LM is gone — so
// the SPS is far less constrained than a single "pushing the LM" figure
// suggests.
//
// Run: go test ./internal/sim -run TestApolloLunarBudgetProbe -v
// It always passes; read the t.Log output.

import (
	"math"
	"testing"
)

// apolloReqs are the per-maneuver Δv requirements (m/s), game values.
type apolloReqs struct {
	tli, corrections, loi, descent, ascentRdv, tei float64
}

// apolloArcMargins are the per-stage margins (avail − need), m/s.
type apolloArcMargins struct {
	sivbRemain, tliMargin   float64
	smTEIAvail, smMargin    float64
	smLOIFuelOK             bool
	dpsAvail, dpsMargin     float64
	apsAvail, apsMargin     float64
	loiFuelUsed, teiFuelRem float64
}

// computeArc computes the post-transposition lunar-arc margins analytically
// given the ascent helper's S-IVB-at-park figure.
func computeArc(sivb, dps, aps, csm apolloStage, reqs apolloReqs, sivbRemain float64) apolloArcMargins {
	lm := dps.dry + dps.fuel + aps.dry + aps.fuel
	csmWet := csm.dry + csm.fuel

	m := apolloArcMargins{}
	m.sivbRemain = sivbRemain
	m.tliMargin = sivbRemain - reqs.tli

	// SM phased budget: corrections+LOI at LM-attached mass, TEI bare CSM.
	mLOIStart := csmWet + lm
	dvHeavy := reqs.corrections + reqs.loi
	// fuel burned for the heavy phase via rocket equation.
	mAfterHeavy := mLOIStart * math.Exp(-dvHeavy/(csm.isp*apolloG0))
	fuelHeavy := mLOIStart - mAfterHeavy
	m.loiFuelUsed = fuelHeavy
	m.smLOIFuelOK = fuelHeavy <= csm.fuel
	fuelRem := csm.fuel - fuelHeavy
	if fuelRem < 0 {
		fuelRem = 0
	}
	m.teiFuelRem = fuelRem
	mTEIStart := csm.dry + fuelRem // LM undocked, ascent stage jettisoned
	m.smTEIAvail = rocketDv(csm.isp, mTEIStart, csm.dry)
	m.smMargin = m.smTEIAvail - reqs.tei

	// DPS: descent pushing the whole LM down (burns descent fuel).
	m.dpsAvail = rocketDv(dps.isp, dps.dry+dps.fuel+aps.dry+aps.fuel, dps.dry+aps.dry+aps.fuel)
	m.dpsMargin = m.dpsAvail - reqs.descent

	// APS: ascent stage to orbit + rendezvous.
	m.apsAvail = rocketDv(aps.isp, aps.dry+aps.fuel, aps.dry)
	m.apsMargin = m.apsAvail - reqs.ascentRdv

	return m
}

func TestApolloLunarBudgetProbe(t *testing.T) {
	earth, ok := loadEarth()
	if !ok {
		t.Fatal("earth not found")
	}
	const targetAlt = 200e3

	// Lower stack (fixed: byte-identical Saturn V).
	sic := apolloStage{130000, 2160000, 35100000, 263, 8e-6}
	sii := apolloStage{40000, 440000, 5140000, 421, 2.5e-5}

	reqs := apolloReqs{tli: 3133, corrections: 50, loi: 900, descent: 2000, ascentRdv: 1850, tei: 1000}

	type config struct {
		name           string
		sivb, dps, aps apolloStage
		csm            apolloStage
	}
	// Baseline = current catalog (single fused CSM stands in for SM+CM mass;
	// the SPS engine/fuel is what the budget cares about).
	baseSIVB := apolloStage{11000, 109000, 1023000, 421, 6.25e-5}
	baseDPS := apolloStage{2500, 9500, 45000, 311, 0}
	baseAPS := apolloStage{1200, 1800, 16000, 311, 0}
	baseCSM := apolloStage{11900, 18400, 91000, 314, 0}

	configs := []config{
		{"baseline (current catalog)", baseSIVB, baseDPS, baseAPS, baseCSM},
		// LEAN trim: size the LM to just clear nominal + a thin buffer
		// (DPS cap ~2200, APS cap ~2000). Maximises the S-IVB's TLI margin.
		{"lean trim (DPS~2200, APS~2000)", baseSIVB,
			apolloStage{2500, 5087, 45000, 311, 0},
			apolloStage{1200, 1112, 16000, 311, 0},
			baseCSM},
		// REALISTIC reserve: DPS carries the real abort-to-orbit margin
		// (cap ~2500), APS ~2200. Heavier LM → less TLI help for the S-IVB.
		{"realistic reserve (DPS~2500, APS~2200)", baseSIVB,
			apolloStage{2500, 6310, 45000, 311, 0},
			apolloStage{1200, 1269, 16000, 311, 0},
			baseCSM},
		// REALISTIC reserve + shave the SPS surplus (TEI is well over) to
		// claw back some S-IVB margin without touching the LM reserves.
		{"realistic reserve + SPS shave", baseSIVB,
			apolloStage{2500, 6310, 45000, 311, 0},
			apolloStage{1200, 1269, 16000, 311, 0},
			apolloStage{11900, 16000, 91000, 314, 0}},
	}

	t.Logf("Apollo lunar-arc per-stage budget (post-transposition). Requirements:")
	t.Logf("  TLI %.0f · corrections %.0f · LOI %.0f · descent %.0f · ascent+rdv %.0f · TEI %.0f",
		reqs.tli, reqs.corrections, reqs.loi, reqs.descent, reqs.ascentRdv, reqs.tei)

	for _, c := range configs {
		payload := (c.dps.dry + c.dps.fuel) + (c.aps.dry + c.aps.fuel) + (c.csm.dry + c.csm.fuel)
		best := bestApolloAscent(earth, []apolloStage{sic, sii, c.sivb}, payload, targetAlt)
		t.Logf("")
		t.Logf("── %s ──  (payload above S-IVB = %.0f kg)", c.name, payload)
		if !best.reached {
			t.Logf("   ASCENT FAILED — no park reached; stack is short before TLI.")
			continue
		}
		m := computeArc(c.sivb, c.dps, c.aps, c.csm, reqs, best.remain)
		t.Logf("   S-IVB  park %.0f km, remaining %.0f → TLI margin %+.0f   %s",
			best.apoKm, m.sivbRemain, m.tliMargin, pass(m.tliMargin >= 0))
		t.Logf("   SM     LOI burns %.0f kg (of %.0f), %.0f kg left → TEI avail %.0f → TEI margin %+.0f   %s",
			m.loiFuelUsed, c.csm.fuel, m.teiFuelRem, m.smTEIAvail, m.smMargin, pass(m.smMargin >= 0 && m.smLOIFuelOK))
		t.Logf("   DPS    descent avail %.0f → margin %+.0f   %s", m.dpsAvail, m.dpsMargin, pass(m.dpsMargin >= 0))
		t.Logf("   APS    ascent avail %.0f → margin %+.0f   %s", m.apsAvail, m.apsMargin, pass(m.apsMargin >= 0))
		all := m.tliMargin >= 0 && m.smMargin >= 0 && m.smLOIFuelOK && m.dpsMargin >= 0 && m.apsMargin >= 0
		t.Logf("   ⇒ mission closes: %v", all)
	}
}

func pass(ok bool) string {
	if ok {
		return "OK"
	}
	return "SHORT"
}
