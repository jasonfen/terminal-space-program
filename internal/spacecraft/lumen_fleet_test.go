package spacecraft

import (
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
)

// Lumen-fleet eval (ADR 0031 / S11). The stripped-back fleet must (a) cover
// every role the real fleet has and (b) actually fly its budget: each launch
// vehicle reaches Kern orbit (~3.4 km/s) with lift-off TWR ≥ 1, sized to the
// stripped-back scale (not an over-fuelled interplanetary monster). Mirrors the
// apollo_lunar_budget_test.go staged-Δv pattern, but as a CI assertion.

// kernBody is a Kern fixture from systems/lumen.json (mass 5.2915158e22 kg,
// mean radius 600 km) so g and μ use the same physics the sim does. Kern has
// Earth-like surface gravity on a tiny radius — hence ~3.4 km/s to orbit.
func kernBody() bodies.CelestialBody {
	return bodies.CelestialBody{Mass: bodies.Mass{Value: 5.2915158, Exponent: 22}, MeanRadius: 600}
}

// stagedDeltaV is the total ideal Δv of a bottom-first stack: each stage burns
// the mass of every stage above it as payload, then is dropped (serial
// staging). Engineless / empty stages contribute nothing.
func stagedDeltaV(stages []Stage) float64 {
	const g0 = 9.80665
	wet := make([]float64, len(stages))
	for i, s := range stages {
		wet[i] = s.DryMass + s.FuelMass
	}
	dv := 0.0
	for i, s := range stages {
		if s.Thrust <= 0 || s.FuelMass <= 0 {
			continue
		}
		above := 0.0
		for j := i + 1; j < len(stages); j++ {
			above += wet[j]
		}
		dv += s.Isp * g0 * math.Log((wet[i]+above)/(s.DryMass+above))
	}
	return dv
}

// launchTWR is the lift-off thrust-to-weight of a loadout on body: bottom-stage
// thrust over full-stack weight (the same gate craftLiftsOff uses, ADR 0031/S9).
func launchTWR(l Loadout, body *bodies.CelestialBody) float64 {
	wet := SumDryMass(l.Stages) + SumFuelMass(l.Stages)
	g := body.GravitationalParameter() / (body.RadiusMeters() * body.RadiusMeters())
	return l.Thrust() / (wet * g)
}

// TestLumenLaunchersReachKernOrbit — every Lumen launch vehicle clears the
// ~3.4 km/s Kern-orbit budget with lift-off TWR ≥ 1, and stays in the
// stripped-back band (≤ 6 km/s — not accidentally over-fuelled to real scale).
func TestLumenLaunchersReachKernOrbit(t *testing.T) {
	kern := kernBody()
	const kernOrbitDV = 3400.0 // m/s, KSP/Kerbin-class figure (ADR 0014)
	for _, id := range []string{"Vector-V", "Raster-LS", "Packet-9"} {
		l, ok := Loadouts[id]
		if !ok {
			t.Fatalf("Lumen launcher %q missing from catalog", id)
		}
		if l.Scale() != bodies.ScaleStrippedBack {
			t.Errorf("%s should be stripped-back scale, got %q", id, l.Scale())
		}
		dv := stagedDeltaV(l.Stages)
		if dv < kernOrbitDV {
			t.Errorf("%s Δv = %.0f m/s, below the %.0f Kern-orbit budget", id, dv, kernOrbitDV)
		}
		if dv > 6000 {
			t.Errorf("%s Δv = %.0f m/s, too hot for stripped-back scale (≤6000 expected)", id, dv)
		}
		if twr := launchTWR(l, &kern); twr < 1.0 {
			t.Errorf("%s lift-off TWR = %.2f on Kern, must be ≥ 1", id, twr)
		}
	}
}

// TestSocketLanderCursorBudget — the Lumen lander can land on Cursor and return
// to orbit: its serial descent+ascent Δv covers the Cursor land-and-return
// budget (~1.2 km/s; Cursor orbital velocity ≈ 0.56 km/s).
func TestSocketLanderCursorBudget(t *testing.T) {
	l := Loadouts["Socket-Lander"]
	if dv := stagedDeltaV(l.Stages); dv < 1200 {
		t.Errorf("Socket Lander land+return Δv = %.0f m/s, want ≥ 1200 for Cursor", dv)
	}
}

// TestLumenFleetFullParity — every category the real (Sol) fleet populates also
// has a stripped-back (Lumen) counterpart, so the system-filtered Lumen picker
// mirrors Sol's roster role-for-role (ADR 0031 / S11, decision 5).
func TestLumenFleetFullParity(t *testing.T) {
	realCats := map[string]bool{}
	lumenCats := map[string]bool{}
	for _, id := range LoadoutOrder {
		l := Loadouts[id]
		if l.Category == "" {
			continue
		}
		if l.Scale() == bodies.ScaleStrippedBack {
			lumenCats[l.Category] = true
		} else {
			realCats[l.Category] = true
		}
	}
	for c := range realCats {
		if !lumenCats[c] {
			t.Errorf("category %q has real-fleet craft but no stripped-back Lumen counterpart", c)
		}
	}
}

// TestLumenFleetCrewParity — the Lumen crewed/uncrewed split mirrors the real
// fleet: the capsule analog is crewed, the lander analog is uncrewed.
func TestLumenFleetCrewParity(t *testing.T) {
	if !Loadouts["Token-Pod"].Crewed() {
		t.Error("Token Pod (Lumen capsule) should be crewed, like the real Capsule")
	}
	if Loadouts["Socket-Lander"].Crewed() {
		t.Error("Socket Lander should be uncrewed, like the real Lander")
	}
}
