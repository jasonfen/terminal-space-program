package sim

import (
	"math"
	"strconv"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/missions"
	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// batchFixture spawns the catalog Falcon 9 + Starlink x24 at 500 km, drops the
// spent F9-S1 (so the carrier is the 8-row F9-S2 the ADR describes), empties
// its tanks when dry, and turns the stack to angleDeg off the velocity in the
// orbital plane (90 = radial). Returns the world and the carrier's slate index.
func batchFixture(t *testing.T, angleDeg float64, dry bool) (*World, int) {
	t.Helper()
	return batchFixtureAt(t, 500e3, angleDeg, dry)
}

// batchFixtureAt is batchFixture at altM.
func batchFixtureAt(t *testing.T, altM, angleDeg float64, dry bool) (*World, int) {
	t.Helper()
	w, err := NewWorld()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.SpawnCraft(SpawnSpec{LoadoutID: "Falcon-9-Starlink-24", ParentBodyID: "earth", AltitudeM: altM}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := w.StageActive(w.ActiveCraftIdx); err != nil {
		t.Fatal(err)
	}
	c := w.ActiveCraft()
	c.ReconcileDockedComponents()
	if dry {
		c.Stages[0].FuelMass = 0 // only the carrier's own tanks; the sats keep their argon
		c.SyncFields()
		c.State.M = c.TotalMass()
	}
	th := angleDeg * math.Pi / 180
	c.CurrentAttitudeDir = c.State.V.Unit().Scale(math.Cos(th)).Add(radialOutUnit(c).Scale(math.Sin(th)))
	c.AttitudeMode = spacecraft.BurnRadialOut
	if angleDeg < 45 {
		c.AttitudeMode = spacecraft.BurnPrograde
	}
	return w, w.ActiveCraftIdx
}

// releasedFan releases a fresh fixture and returns the world, the lead's slate
// index, the composite's velocity before the release and the 24 pieces.
func releasedFan(t *testing.T, angleDeg float64, dry bool) (*World, int, orbital.Vec3, []*spacecraft.Spacecraft) {
	t.Helper()
	w, idx := batchFixture(t, angleDeg, dry)
	v0 := w.Crafts[idx].State.V
	if !w.ReleaseBatch(idx) {
		t.Fatalf("ReleaseBatch refused at %v degrees", angleDeg)
	}
	return w, idx, v0, w.Crafts[len(w.Crafts)-24:]
}

// The fan, dry carrier, stack radial (ADR 0059 guard 13). The hulls are where
// Undock draws them: the carrier (F9-S2, 8 rows) spans -0.75..11.25 m from its
// anchor and sat 1 (row 9, 13.5 m) spans 12.75..14.25, so d_1 is 13.5 - 5.25 =
// 8.25 m. The ADR writes 6.75 (its own 13.5 + 0.75 - 6 is 8.25); every number
// below is from 8.25: d_i = 8.25 + 3 (i - 1), d_24 = 77.25 m, d_cm = 35.2 m.
func TestBatchFanSpeedsMomentumAndCentre(t *testing.T) {
	w, idx, v0, sats := releasedFan(t, 90, true)
	lead := w.Crafts[idx]
	cmS := func(a, b *spacecraft.Spacecraft) float64 { return a.State.V.Sub(b.State.V).Norm() * 100 }

	if got := cmS(sats[1], sats[0]); math.Abs(got-0.75) > 0.01 {
		t.Errorf("neighbour step = %.3f cm/s, want 0.75", got)
	}
	if got := cmS(sats[0], lead); math.Abs(got-2.0625) > 0.01 {
		t.Errorf("sat 1 to carrier = %.3f cm/s, want 2.06 (2.5e-3 x 8.25)", got)
	}
	if got := cmS(sats[23], lead); math.Abs(got-19.3125) > 0.01 {
		t.Errorf("sat 24 to carrier = %.3f cm/s, want 19.31 (2.5e-3 x 77.25)", got)
	}

	// Momentum is zero about the pre-release velocity, and the carrier's
	// recoil is omega x d_cm with d_cm from the formula over the real masses.
	all := append([]*spacecraft.Spacecraft{lead}, sats...)
	var m, md float64
	var p orbital.Vec3
	for i, c := range all {
		d := 0.0
		if i > 0 {
			d = 8.25 + 3*float64(i-1)
		}
		m += c.TotalMass()
		md += c.TotalMass() * d
		p = p.Add(c.State.V.Sub(v0).Scale(c.TotalMass()))
	}
	cm := md / m
	if math.Abs(cm-35.2) > 0.1 {
		t.Errorf("d_cm = %.2f m, want 35.2 +- 0.1", cm)
	}
	if p.Norm() > 1e-6 {
		t.Errorf("momentum sum = %.3g kg m/s, want 0 (1e-6)", p.Norm())
	}
	if recoil := lead.State.V.Sub(v0).Norm() / BatchFanRateRadPerS; math.Abs(recoil-cm) > 1e-6 {
		t.Errorf("carrier recoil / omega = %.4f m, want d_cm %.4f", recoil, cm)
	}

	// The card prints the two attitude-free facts.
	w2, idx2 := batchFixture(t, 90, true)
	if name, n, note, ok := w2.BatchReleaseCard(idx2); !ok || name != "Starlink" || n != 24 ||
		note != "they fan out sideways, 0.8 cm/s a seam, top 19" {
		t.Errorf("card = %q %d %q %v", name, n, note, ok)
	}
}

// Two releases of the same start give the same 25 state vectors, bit for bit
// (ADR 0059 guard 13: the fan is geometry, no random number).
func TestBatchFanIsByteIdentical(t *testing.T) {
	wa, ia, _, _ := releasedFan(t, 45, false)
	wb, ib, _, _ := releasedFan(t, 45, false)
	a, b := wa.Crafts[ia:], wb.Crafts[ib:]
	if len(a) != len(b) || len(a) < 25 {
		t.Fatalf("slates %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i].State != b[i].State {
			t.Fatalf("piece %d differs: %+v vs %+v", i, a[i].State, b[i].State)
		}
	}
}

// ReleaseBatch's bookkeeping (guard 2): numbered bottom first, the lead in
// place and still active, the 24 appended, a deploy per piece and no undock,
// one batch id, the one-orbit exemption stamped, and every pair latched.
func TestReleaseBatchSlateAndBookkeeping(t *testing.T) {
	w, idx := batchFixture(t, 90, false)
	before := len(w.Crafts)
	lead0 := w.Crafts[idx]
	now := w.Clock.SimTime
	period := 2 * math.Pi * math.Sqrt(math.Pow(lead0.State.R.Norm(), 3)/lead0.Primary.GravitationalParameter())
	if !w.ReleaseBatch(idx) {
		t.Fatal("refused")
	}
	if got, want := len(w.Crafts), before-1+25; got != want {
		t.Fatalf("slate = %d, want %d", got, want)
	}
	if w.Crafts[idx] == lead0 || w.Crafts[idx].Name != "F9-S2" || w.ActiveCraftIdx != idx {
		t.Errorf("lead %q at %d active %d, want the F9-S2 in place and active", w.Crafts[idx].Name, idx, w.ActiveCraftIdx)
	}
	sats := w.Crafts[len(w.Crafts)-24:]
	id := w.Crafts[idx].BatchID
	if id == 0 || !w.Crafts[idx].BatchLead {
		t.Errorf("lead batch id %d lead %v", id, w.Crafts[idx].BatchLead)
	}
	for i, s := range sats {
		if want := "Starlink-" + strconv.Itoa(i+1); s.Name != want {
			t.Errorf("sat %d named %q, want %q", i, s.Name, want)
		}
		if s.BatchID != id || s.BatchLead {
			t.Errorf("%s batch id %d lead %v", s.Name, s.BatchID, s.BatchLead)
		}
		if d := s.BatchClampExemptUntil.Sub(now).Seconds() - period; math.Abs(d) > 1 {
			t.Errorf("%s exempt for %.1f s, want one period %.1f", s.Name, s.BatchClampExemptUntil.Sub(now).Seconds(), period)
		}
	}
	var deploys, undocks int
	for _, a := range w.recentActions {
		switch a {
		case missions.ActionDeploy:
			deploys++
		case missions.ActionUndock:
			undocks++
		}
	}
	if deploys != 24 || undocks != 0 {
		t.Errorf("actions: %d deploy %d undock, want 24 and 0", deploys, undocks)
	}
	if got := len(w.localReArms); got != 300 { // C(25,2): the lead is in the ring too (ADR said 276 = C(24,2))
		t.Errorf("re-arm latches = %d, want 300", got)
	}
}

// flightMinGap flies the world at 100x for orbits periods of the carrier and
// returns the smallest hull distance over every same-batch pair, the smallest
// sat 1 to carrier distance, and whether any bump fired. The pieces keep the
// stack's pointing mode, so the hulls fly as released.
func flightMinGap(t *testing.T, w *World, lead *spacecraft.Spacecraft, orbits float64) (minAll, minFlank float64, bumped bool) {
	t.Helper()
	period := orbitalPeriod(lead.State, lead.Primary.GravitationalParameter())
	end := w.Clock.SimTime.Add(time.Duration(orbits * period * float64(time.Second)))
	w.Clock.WarpIdx = 2 // 100x
	minAll, minFlank = math.Inf(1), math.Inf(1)
	sat1 := w.Crafts[len(w.Crafts)-24]
	for w.Clock.SimTime.Before(end) {
		w.Tick()
		if w.LastBumpEvent != nil {
			bumped = true
		}
		for i := 0; i < len(w.Crafts); i++ {
			for j := i + 1; j < len(w.Crafts); j++ {
				if a, b := w.Crafts[i], w.Crafts[j]; a.BatchID != 0 && a.BatchID == b.BatchID {
					minAll = math.Min(minAll, craftHullDistanceM(a, b))
				}
			}
		}
		minFlank = math.Min(minFlank, craftHullDistanceM(lead, sat1))
	}
	return
}

// No contact (guard 14): three orbits at 100x, bumps checked every tick, at
// 10 degrees off the velocity and with the stack radial. Sabotage: omega
// 1.5e-3 rad/s sends both rows red (a bump on the first tick).
func TestBatchFanNeverTouches(t *testing.T) {
	for _, c := range []struct {
		name  string
		angle float64
	}{{"ten degrees off the velocity", 10}, {"radial", 90}} {
		t.Run(c.name, func(t *testing.T) {
			w, idx := batchFixture(t, c.angle, true)
			if !w.ReleaseBatch(idx) {
				t.Fatal("refused")
			}
			minAll, minFlank, bumped := flightMinGap(t, w, w.Crafts[idx], 3)
			if bumped {
				t.Error("a BumpEvent fired")
			}
			if minAll <= 0 {
				t.Errorf("closest pair = %.3f m, want above 0", minAll)
			}
			if minFlank < 0.3 {
				t.Errorf("sat 1 to carrier closest = %.3f m, want at least 0.3", minFlank)
			}
		})
	}
}

// The attitude refusal (guard 15): inside 10 degrees of the velocity line,
// forward or backward, nothing is released; at 10 and 11 it goes.
func TestBatchFanAttitudeRefusal(t *testing.T) {
	for _, c := range []struct {
		deg     float64
		reverse bool
		refused bool
	}{{9, false, true}, {9, true, true}, {10, false, false}, {10, true, false}, {11, false, false}, {11, true, false}, {0, false, true}} {
		w, idx := batchFixture(t, c.deg, false)
		lead := w.Crafts[idx]
		if c.reverse {
			lead.CurrentAttitudeDir = lead.CurrentAttitudeDir.Scale(-1)
		}
		before := len(w.Crafts)
		got := w.ReleaseBatch(idx)
		if got == c.refused {
			t.Errorf("%v deg reverse=%v: released=%v, want %v", c.deg, c.reverse, got, !c.refused)
		}
		if c.refused {
			if r := BatchFanRefusal(lead); r != "release: stack along the velocity; hold radial (q) first" {
				t.Errorf("%v deg: refusal %q", c.deg, r)
			}
			if len(w.Crafts) != before || w.Crafts[idx] != lead {
				t.Errorf("%v deg: the stack was touched by a refused release", c.deg)
			}
		}
	}
}

// A pair is not a batch (guard 1): U keeps the spring, the in-place insert and
// no card. The existing TestUndockRestoresComponents covers the spring.
func TestAPairIsNotABatch(t *testing.T) {
	w, idx := batchFixture(t, 90, false)
	c := w.Crafts[idx]
	c.DockedComponents = c.DockedComponents[:2]
	c.Stages = c.Stages[:len(c.DockedComponents[0].Stages)+len(c.DockedComponents[1].Stages)]
	if w.IsBatch(idx) {
		t.Error("a carrier with one payload is a batch")
	}
	if _, _, _, ok := w.BatchReleaseCard(idx); ok {
		t.Error("a pair gets a batch card")
	}
	before := len(w.Crafts)
	if !w.Undock(idx) || len(w.Crafts) != before+1 {
		t.Fatalf("pair undock: slate %d -> %d, want +1 in place", before, len(w.Crafts))
	}
	if w.Crafts[idx].BatchID != 0 || w.Crafts[idx+1].BatchID != 0 {
		t.Error("a pair's pieces carry a batch id")
	}
}
