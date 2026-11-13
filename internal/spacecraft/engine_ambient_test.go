package spacecraft

// New file: engine_ambient.go is new in ADR 0070 and its guards belong together.

import (
	"math"
	"testing"
)

func TestIspAtBlendAndClamp(t *testing.T) {
	for _, c := range []struct{ sl, vac, pr, want float64 }{
		{282, 311, 0, 311}, // vacuum: the vacuum figure, exactly
		{282, 311, 1, 282}, // p = p_sl: the sea-level figure, exactly (the equal case)
		{282, 311, 0.5, 296.5},
		{200, 421, 92, 0},     // Venus's surface: past any engine, clamps at zero
		{3000, 3000, 7, 3000}, // electric: flat
	} {
		if got := IspAt(c.sl, c.vac, c.pr); math.Abs(got-c.want) > 1e-9 {
			t.Errorf("IspAt(%v, %v, %v) = %v, want %v", c.sl, c.vac, c.pr, got, c.want)
		}
	}
}

func merlinStage() Stage {
	s, _ := BuildStage(StageModuleF9S1ID)
	return s
}

// Fixed mass flow: thrust follows Isp, so mdot = F / (g0 Isp) is the same at every pressure.
func TestSetAmbientKeepsMassFlowFixed(t *testing.T) {
	st := merlinStage()
	mdot0 := st.Thrust / (st.Isp * g0)
	for _, pr := range []float64{1, 0.5, 0.01, 0} {
		st.SetAmbient(pr)
		if got := st.Thrust / (st.Isp * g0); math.Abs(got-mdot0) > 1e-9*mdot0 {
			t.Errorf("pr %v: mdot %v, want %v", pr, got, mdot0)
		}
	}
	st.SetAmbient(1)
	if st.Isp != 282 || math.Abs(st.Thrust-7607000) > 1e-6 {
		t.Errorf("sea level: %v s %v N, want the rated 282 s 7607000 N", st.Isp, st.Thrust)
	}
	st.SetAmbient(0)
	if st.Isp != 311 || math.Abs(st.Thrust-7607000.0*311/282) > 1e-3 {
		t.Errorf("vacuum: %v s %v N, want 311 s and thrust x 311/282", st.Isp, st.Thrust)
	}
}

// A vacuum-rated upper stage fired low gets its sea-level figure, thrust and Isp both.
func TestSetAmbientVacuumEngineAtSeaLevel(t *testing.T) {
	st, _ := BuildStage(StageModuleSIVBID)
	st.SetAmbient(1)
	if st.Isp != 200 || math.Abs(st.Thrust-1023000.0*200/421) > 1e-3 {
		t.Errorf("J-2 at sea level: %v s %v N, want 200 s and thrust x 200/421", st.Isp, st.Thrust)
	}
	st.SetAmbient(1e9) // far past zero: never negative
	if st.Isp != 0 || st.Thrust != 0 {
		t.Errorf("crushing pressure: %v s %v N, want 0 and 0", st.Isp, st.Thrust)
	}
}

func TestSetAmbientLeavesFlatEnginesAlone(t *testing.T) {
	st := Stage{Thrust: 1000, Isp: 300, FuelMass: 1}
	if st.SetAmbient(1) || st.Thrust != 1000 || st.Isp != 300 {
		t.Errorf("flat stage moved: %v N %v s", st.Thrust, st.Isp)
	}
	ion, _ := BuildStage("ion-keeper")
	if ion.SetAmbient(1) || ion.Isp != 3000 {
		t.Errorf("electric engine moved with pressure: %v s", ion.Isp)
	}
}

// The SuperDraco swap keeps working: the swapped-in engine is flat, and the
// Dracos come back at the pressure they had.
func TestSetAmbientAndSuperDracoSwap(t *testing.T) {
	st, _ := BuildModule("dragon-crew")
	c := &Spacecraft{Stages: st}
	c.SyncFields()
	c.SetAmbient(1)
	if math.Abs(c.Thrust-1600*165/300.0) > 1e-9 || c.Isp != 165 {
		t.Fatalf("Dracos at sea level %v N %v s, want 880 N 165 s", c.Thrust, c.Isp)
	}
	c.OnLandingBrake(func() {
		if c.Thrust != 568000 || c.Isp != 235 {
			t.Errorf("inside the swap %v N %v s, want the SuperDracos' 568000 N 235 s at any pressure", c.Thrust, c.Isp)
		}
	})
	if math.Abs(c.Thrust-880) > 1e-9 || c.Isp != 165 {
		t.Errorf("after the swap %v N %v s, want the Dracos back at 880 N 165 s", c.Thrust, c.Isp)
	}
}

func TestEngineCurveMatchesEngineAt(t *testing.T) {
	c := NewFromLoadout("Falcon-9")
	f := c.EngineCurve()
	for _, pr := range []float64{0, 0.2, 1} {
		t1, i1 := c.EngineAt(pr)
		t2, i2 := f(pr)
		if math.Abs(t1-t2) > 1e-6*t1 || math.Abs(i1-i2) > 1e-9 {
			t.Errorf("pr %v: EngineAt %v/%v, curve %v/%v", pr, t1, i1, t2, i2)
		}
	}
	if _, isp := c.EngineAt(1); isp != 282 {
		t.Errorf("sea level Isp = %v, want 282", isp)
	}
	if _, isp := c.EngineAt(0); isp != 311 {
		t.Errorf("vacuum Isp = %v, want 311", isp)
	}
}

// A save keeps only the live pair; RestoreEngine brings the catalog pair back
// from the stage's name and rebuilds the mass flow from what was saved.
func TestRestoreEngineFromLiveFigures(t *testing.T) {
	st := merlinStage()
	want := st
	st.SetAmbient(0.3)
	live := st
	live.ThrustVac, live.IspSL, live.IspVac = 0, 0, 0 // what a load gives back
	live.RestoreEngine()
	if live.IspSL != 282 || live.IspVac != 311 || math.Abs(live.ThrustVac-want.Thrust*311/282) > 1e-3 {
		t.Errorf("restored %v/%v, vac thrust %v", live.IspSL, live.IspVac, live.ThrustVac)
	}
	half := live
	half.Thrust /= 2 // a booster half is saved with its halved thrust
	half.ThrustVac, half.IspSL, half.IspVac = 0, 0, 0
	half.RestoreEngine()
	if math.Abs(half.ThrustVac-live.ThrustVac/2) > 1e-3 {
		t.Errorf("half: vac thrust %v, want %v", half.ThrustVac, live.ThrustVac/2)
	}
	odd := live // an Isp outside the catalog pair is not the catalog's engine
	odd.ThrustVac, odd.IspSL, odd.IspVac, odd.Isp = 0, 0, 0, 500
	odd.RestoreEngine()
	if odd.IspVac != 0 {
		t.Errorf("a 500 s stage was given the catalog pair")
	}
}

// The data states the rated Isp (isp_s) as the figure at the thrust's own
// condition; a drift between the two would fly the wrong engine.
func TestCatalogRatedIspMatchesItsCondition(t *testing.T) {
	parts, _, err := LoadCatalog()
	if err != nil {
		t.Fatal(err)
	}
	for id, p := range parts {
		if p.IspSLS == 0 && p.IspVacS == 0 {
			continue
		}
		if len(p.Components) > 0 {
			continue
		}
		want := p.IspVacS
		if p.ThrustAt == "sl" {
			want = p.IspSLS
		}
		if p.IspS != want {
			t.Errorf("part %s: isp_s %v, but thrust_at %q figure is %v", id, p.IspS, p.ThrustAt, want)
		}
		if p.IspSLS > p.IspVacS {
			t.Errorf("part %s: sea-level Isp %v above vacuum %v", id, p.IspSLS, p.IspVacS)
		}
	}
}

func TestComposedEngineClusterBlendsByMassFlow(t *testing.T) {
	comps := map[string]Component{
		"a": {ID: "a", Kind: ComponentEngine, ThrustN: 1000, IspS: 300, IspSLS: 300, IspVacS: 340, ThrustAt: "sl", FuelType: "kerolox"},
		"b": {ID: "b", Kind: ComponentEngine, ThrustN: 500, IspS: 400, IspSLS: 200, IspVacS: 400, ThrustAt: "vac", FuelType: "kerolox"},
	}
	out, err := composePart(Part{ID: "x", Components: []string{"a", "b"}}, comps)
	if err != nil {
		t.Fatal(err)
	}
	// vacuum thrust 1000*340/300 + 500; flow a 1133.33/340, b 500/400
	tv := 1000.0*340/300 + 500
	if math.Abs(out.ThrustVacN-tv) > 1e-9 {
		t.Errorf("ThrustVacN = %v, want %v", out.ThrustVacN, tv)
	}
	fa, fb := tv-500, 500.0
	flow := fa/340 + fb/400
	if math.Abs(out.IspVacS-tv/flow) > 1e-9 || math.Abs(out.IspSLS-(fa/340*300+fb/400*200)/flow) > 1e-9 {
		t.Errorf("cluster Isp sl/vac = %v/%v", out.IspSLS, out.IspVacS)
	}
}
