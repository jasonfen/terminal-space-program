package screens

import (
	"strings"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// startVesselTugWorld is the first dock a new player flies: the default Apollo
// TLI start vessel and an RCS Tug alongside, moved to portGapM off the CM
// port, straight and at rest, with the start vessel targeted in Proximity.
func startVesselTugWorld(t *testing.T, portGapM float64) *sim.World {
	t.Helper()
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatal(err)
	}
	home := w.Crafts[0]
	if _, err := w.SpawnCraft(sim.SpawnSpec{LoadoutID: "RCS-tug", Alongside: true}); err != nil {
		t.Fatal(err)
	}
	tug := w.ActiveCraft()
	gh, gt := sim.VesselPortGeometry(home), sim.VesselPortGeometry(tug)
	if !gh.HasPort() || !gt.HasPort() {
		t.Fatal("fixture: both vessels should carry a port")
	}
	portTug := gh.PortPoint(home.State.R, 0).Add(gh.Axis.Scale(portGapM))
	tug.CurrentAttitudeDir = gh.Axis.Scale(-1)
	tug.State.R = portTug.Sub(tug.CurrentAttitudeDir.Scale(gt.Ports[0].OffsetM))
	tug.State.V = home.State.V
	w.SetTargetCraft(0)
	w.ViewMode = sim.ViewProximity
	return w
}

// Review finding 4, the player's side: against the 55.5 m start vessel the
// stack bottoms are over 50 m apart when the ports are 0.8 m apart, and the
// chip must still show the ring-gated rows (off-axis:, the need reminder) and
// a range: that does not contradict the port: row.
func TestProximityChipInsideZoneAgainstTheStartVessel(t *testing.T) {
	w := startVesselTugWorld(t, 0.8)
	st, ok := w.ProximityState()
	if !ok {
		t.Fatal("no ProximityState")
	}
	if st.RangeM < 55 {
		t.Fatalf("fixture: anchors %.1f m apart, want > 55 (the point of the test)", st.RangeM)
	}
	if !proximityInsideRing(st) {
		t.Errorf("proximityInsideRing = false at a 0.8 m port gap (anchors %.1f m)", st.RangeM)
	}
	v := NewOrbitView(chipTestTheme())
	v.Resize(DesignWidth, DesignHeight)
	chip := v.buildProximityChip(w)
	if row := chipLine(chip, "lateral:"); row == "" {
		t.Errorf("no off-axis: row at 0.8 m from the port: %q", chip)
	}
	if row := chipLine(chip, "port:"); !strings.Contains(row, "need < 0.50") {
		t.Errorf("port: row %q lacks the (need < 0.50) reminder", row)
	}
	if got, want := chipLine(chip, "range:"), "0.8 m"; !strings.Contains(got, want) {
		t.Errorf("range: row = %q, want the 0.8 m hull gap (%q)", got, want)
	}
}

// The 50 m ring is centred on the target's PORT: the port is inside it.
func TestProximityGateRingIsCentredOnTheTargetPort(t *testing.T) {
	w := startVesselTugWorld(t, 0.8)
	st, _ := w.ProximityState()
	c := proximityGateRingCentre(st)
	port := st.TargetWorld.Add(st.Target.Axis.Scale(st.Target.Ports[0].OffsetM))
	if d := c.Sub(port).Norm(); d > 1e-6 {
		t.Errorf("ring centre %.3f m from the target's port, want on it", d)
	}
}

// #822: the view and its R/V cross are centred on the target's port, not on
// the bottom of a 55 m stack, so the part being flown to sits in the middle.
func TestProximityViewIsCentredOnTheTargetPort(t *testing.T) {
	w := startVesselTugWorld(t, 20)
	v := NewOrbitView(chipTestTheme())
	v.Resize(DesignWidth, DesignHeight)
	v.Render(w, 0, DesignWidth, DesignHeight)
	st, _ := w.ProximityState()
	port := st.TargetWorld.Add(st.Target.Axis.Scale(st.Target.Ports[0].OffsetM))
	if port.Sub(st.TargetWorld).Norm() < 10 {
		t.Fatal("fixture: the port should sit well off the stack bottom")
	}
	if d := v.canvas.CenterWorld().Sub(port).Norm(); d > 1e-6 {
		t.Errorf("view centre %.1f m from the target's port, want on it", d)
	}
}

// #822: centring on the port must still frame a chaser behind the stack's
// tail, which is farther from the port than the anchor-to-anchor range.
func TestProximityFitFramesAChaserBehindTheTail(t *testing.T) {
	w := startVesselTugWorld(t, 20)
	home, tug := w.Crafts[0], w.ActiveCraft()
	tug.State.R = home.State.R.Sub(sim.VesselPortGeometry(home).Axis.Scale(5))
	v := NewOrbitView(chipTestTheme())
	v.Resize(DesignWidth, DesignHeight)
	v.Render(w, 0, DesignWidth, DesignHeight)
	st, _ := w.ProximityState()
	if _, _, on := v.canvas.Project(st.CraftWorld); !on {
		t.Errorf("chaser 5 m behind the tail is off canvas at the entry fit (range %.0f m)", st.RangeM)
	}
}
