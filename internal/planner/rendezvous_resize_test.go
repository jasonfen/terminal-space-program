package planner

import (
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/physics"
)

// G4 Q4 (#407): you at 500 km, partner at 700 km, both circular. The ladder
// has rows (no size-mismatch refusal), each Ok row is one tangential burn in
// the raising sense, and each row's own re-propagation (both vessels, Kepler,
// to the row's arrival instant) closes to well inside the Ok gate.
func TestRecommendRendezvousLadder_DifferentSizes_RaiseHasRows(t *testing.T) {
	mu := muEarth
	rMover := 6.371e6 + 500e3
	rHolder := 6.371e6 + 700e3
	mover := circularStateAtRadius(rMover, 0, mu)
	holder := circularStateAtRadius(rHolder, 200*math.Pi/180, mu)

	ladder, err := RecommendRendezvousLadder(mover, holder, bodies.CelestialBody{}, mu, RendezvousTheirOrbit, 4*3600, -1)
	if err != nil {
		t.Fatalf("different-size pair refused: %v", err)
	}
	ok := 0
	for _, row := range ladder.Rows {
		if !row.Ok {
			continue
		}
		ok++
		if row.BurnDir.Dot(mover.V.Unit()) <= 0 {
			t.Errorf("laps=%d: raising to a higher partner must burn prograde, dir=%+v", row.Laps, row.BurnDir)
		}
		if row.DV < 54 {
			t.Errorf("laps=%d: dv %.1f m/s is below the 500-to-700 km Hohmann first burn (~54 m/s)", row.Laps, row.DV)
		}
		// Independent check: burn, coast, and measure at the arrival
		// instant (not the row's own AchievableCA field).
		v := mover.V.Unit().Scale(mover.V.Norm() + row.DV)
		m, _ := physics.KeplerStep(physics.StateVector{R: mover.R, V: v}, mu, row.TArrival)
		h, _ := physics.KeplerStep(physics.StateVector{R: holder.R, V: holder.V}, mu, row.TArrival)
		if d := m.R.Sub(h.R).Norm(); d > 1_000 {
			t.Errorf("laps=%d: re-propagated miss %.0f m, want < 1 km", row.Laps, d)
		}
	}
	if ok == 0 {
		t.Fatalf("no Ok rows: %+v", ladder.Rows)
	}
}

// Lowering is the mirror: retrograde, to a lower partner.
func TestRecommendRendezvousLadder_DifferentSizes_LowerBurnsRetrograde(t *testing.T) {
	mu := muEarth
	mover := circularStateAtRadius(6.371e6+700e3, 0, mu)
	holder := circularStateAtRadius(6.371e6+500e3, 270*math.Pi/180, mu)
	ladder, err := RecommendRendezvousLadder(mover, holder, bodies.CelestialBody{}, mu, RendezvousTheirOrbit, 4*3600, -1)
	if err != nil {
		t.Fatalf("different-size pair refused: %v", err)
	}
	for _, row := range ladder.Rows {
		if row.DV > 0 && row.BurnDir.Dot(mover.V.Unit()) >= 0 {
			t.Errorf("laps=%d: lowering must burn retrograde, dir=%+v", row.Laps, row.BurnDir)
		}
	}
}

// "Your orbit" (the partner moves) goes through the same solve with the
// roles swapped, so a 500/700 km pair has rows there too.
func TestRecommendRendezvousLadder_DifferentSizes_YourOrbitHasRows(t *testing.T) {
	mu := muEarth
	a := circularStateAtRadius(6.371e6+500e3, 0, mu)
	b := circularStateAtRadius(6.371e6+700e3, 200*math.Pi/180, mu)
	ladder, err := RecommendRendezvousLadder(a, b, bodies.CelestialBody{}, mu, RendezvousYourOrbit, 4*3600, -1)
	if err != nil {
		t.Fatalf("your orbit refused: %v", err)
	}
	if len(ladder.Rows) == 0 {
		t.Fatal("no rows")
	}
}

// Q5: a stretched orbit on either side is a different SHAPE, out of scope.
// The refusal names circularize [C]; [H] only plans transfers to bodies, so
// pointing a vessel-target pilot at it is a dead end.
func TestRecommendRendezvousLadder_DifferentShape_RefusesPointingAtCircularize(t *testing.T) {
	mu := muEarth
	round := circularStateAtRadius(6.371e6+500e3, 0, mu)
	stretched := ellipseState(6.371e6+900e3, 0.2, 0, 1.0, mu) // periapsis above the round orbit

	_, err := RecommendRendezvousLadder(round, stretched, bodies.CelestialBody{}, mu, RendezvousTheirOrbit, 4*3600, -1)
	if !errors.Is(err, ErrRendezvousShapeMismatch) {
		t.Fatalf("stretched holder: err = %v, want ErrRendezvousShapeMismatch", err)
	}
	_, err = RecommendRendezvousLadder(stretched, round, bodies.CelestialBody{}, mu, RendezvousTheirOrbit, 4*3600, -1)
	if !errors.Is(err, ErrRendezvousShapeMismatch) {
		t.Fatalf("stretched mover: err = %v, want ErrRendezvousShapeMismatch", err)
	}
	msg := ErrRendezvousShapeMismatch.Error()
	if !strings.Contains(msg, "[C]") || strings.Contains(msg, "[H]") {
		t.Errorf("shape refusal %q must point at [C] and never at [H]", msg)
	}
}
