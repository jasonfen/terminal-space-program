package screens

import (
	"strings"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/tui/readout"
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
	if row := chipLine(chip, "off-axis:"); row == "" {
		t.Errorf("no off-axis: row at 0.8 m from the port: %q", chip)
	}
	if row := chipLine(chip, "port:"); !strings.Contains(row, "need < 0.50") {
		t.Errorf("port: row %q lacks the (need < 0.50) reminder", row)
	}
	if got, want := chipLine(chip, "range:"), readout.Distance(0.8); !strings.Contains(got, want) {
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
