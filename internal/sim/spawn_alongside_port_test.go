package sim

import (
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// alongsideWorld is a world whose active vessel is a ported RCS Tug in orbit,
// nose set, ready to spawn a second ported vessel alongside.
func alongsideWorld(t *testing.T) *World {
	t.Helper()
	w, err := NewWorld()
	if err != nil {
		t.Fatal(err)
	}
	old := w.Crafts[0]
	tug := spacecraft.NewFromLoadout("RCS-tug")
	tug.Primary = old.Primary
	tug.State = old.State
	tug.State.M = tug.TotalMass()
	tug.CurrentAttitudeDir = tug.State.V.Unit()
	tug.Name = "Home"
	w.Crafts[0] = tug
	w.stampCraftID(tug)
	return w
}

// An Alongside spawn used to fuse 0.6 s after `enter`: 25 m is inside the old
// 50 m click. It now lands port to port 25 m apart, outside the gate, and
// stays two vessels.
func TestAlongsideSpawnStaysTwoVesselsForTenSeconds(t *testing.T) {
	w := alongsideWorld(t)
	if _, err := w.SpawnCraft(SpawnSpec{LoadoutID: "RCS-tug", Alongside: true}); err != nil {
		t.Fatal(err)
	}
	if len(w.Crafts) != 2 {
		t.Fatalf("spawn left %d vessels", len(w.Crafts))
	}
	tickFor(w, 10)
	if len(w.Crafts) != 2 {
		t.Fatalf("slate = %d vessels 10 s after an Alongside spawn, want 2: it fused", len(w.Crafts))
	}
}

// The partner spawns ahead of the active vessel's nose: ports 25 m apart,
// noses facing, same velocity.
func TestAlongsideSpawnLandsPortToPortStraight(t *testing.T) {
	w := alongsideWorld(t)
	home := w.Crafts[0]
	if _, err := w.SpawnCraft(SpawnSpec{LoadoutID: "RCS-tug", Alongside: true}); err != nil {
		t.Fatal(err)
	}
	partner := w.ActiveCraft() // the new vessel becomes active
	if partner == home {
		t.Fatal("active vessel did not move to the spawn")
	}
	ga, gb := VesselPortGeometry(home), VesselPortGeometry(partner)
	if !ga.HasPort() || !gb.HasPort() {
		t.Fatal("fixture: both vessels should carry a port")
	}
	gap := gb.PortPoint(partner.State.R, 0).Sub(ga.PortPoint(home.State.R, 0)).Norm()
	if gap < 24.999 || gap > 25.001 {
		t.Errorf("port to port = %.4f m, want 25", gap)
	}
	if al := PortAlignmentDeg(ga.Axis, gb.Axis); al > 1e-6 {
		t.Errorf("noses are %.4f deg off facing each other, want 0", al)
	}
	// Straight: the line between the ports runs along home's axis.
	dir := gb.PortPoint(partner.State.R, 0).Sub(ga.PortPoint(home.State.R, 0)).Unit()
	if d := dir.Sub(ga.Axis).Norm(); d > 1e-9 {
		t.Errorf("ports not in line with the active vessel's nose (off by %.2e)", d)
	}
}

// #795: the partner spawns rigid in the orbital frame (co-orbiting), so it
// holds still in the Proximity picture: the port gap does not move and the
// partner does not drift. Port rel speed then reads the gap times the orbit
// rate (about 0.028 m/s at 25 m, measured on a tug pair and a CSM against an
// S-IVB+LM stack), under the 0.10 gate and falling to ~0.0004 at 0.4 m. The
// equal-inertial-velocity spawn read 0.012 / 0.052 and drifted 2.4 / 4.8 m a
// minute.
func TestAlongsideSpawnIsRigidInTheOrbitalFrame(t *testing.T) {
	for _, c := range []struct {
		name        string
		home, spawn []string
	}{
		{"tug and tug", []string{"rcs-tug"}, []string{"rcs-tug"}},
		{"CSM and S-IVB+LM", []string{"csm"}, []string{"s-ivb-1", "apollo-descent", "apollo-ascent"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			w, err := NewWorld()
			if err != nil {
				t.Fatal(err)
			}
			old := w.Crafts[0]
			stagesOf := func(ids []string) []spacecraft.Stage {
				var out []spacecraft.Stage
				for _, id := range ids {
					out = append(out, stageOf(t, id))
				}
				return out
			}
			home := spacecraft.NewFromStages(stagesOf(c.home))
			home.Primary, home.State = old.Primary, old.State
			home.State.M = home.TotalMass()
			home.CurrentAttitudeDir = home.State.V.Unit()
			home.Name = "Home"
			w.Crafts[0] = home
			w.stampCraftID(home)
			if _, err := w.SpawnCraft(SpawnSpec{CustomStages: stagesOf(c.spawn), Alongside: true}); err != nil {
				t.Fatal(err)
			}
			w.SetTargetCraft(0)
			tickFor(w, 0.15) // settle: the first slew tick snaps an uninitialised nose
			st, ok := w.ProximityState()
			if !ok || !st.HasPort {
				t.Fatalf("fixture: no port pair (ok=%v hasPort=%v)", ok, st.HasPort)
			}
			omega := home.State.R.Cross(home.State.V).Norm() / home.State.R.Dot(home.State.R)
			want := omega * st.PortRangeM
			if want < 0.026 || want > 0.030 {
				t.Fatalf("fixture: omega x gap = %.4f, expected about 0.028", want)
			}
			if math.Abs(st.VRelMS-want) > 0.002 {
				t.Errorf("port rel speed at spawn = %.4f m/s, want omega x gap = %.4f (about 0.028), under %.2f", st.VRelMS, want, DockingVMS)
			}
			p0 := st.Frame.ToFrame(st.RelPos)
			gap0 := st.PortRangeM
			tickFor(w, 60)
			st, _ = w.ProximityState()
			if d := math.Abs(st.PortRangeM - gap0); d > 0.02 {
				t.Errorf("port gap moved %.3f m in 60 s, want a few cm at most", d)
			}
			if d := st.Frame.ToFrame(st.RelPos).Sub(p0).Norm(); d > 0.05 {
				t.Errorf("partner drifted %.3f m in the picture in 60 s, want it to hold still", d)
			}
		})
	}
}
