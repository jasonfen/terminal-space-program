package serve

import (
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// tugIn replaces w's only vessel with an RCS Tug at the same orbit, nose along
// prograde (ADR 0053: a vessel needs a port to dock, the default S-IVB has
// none). Returns the tug.
func tugIn(t *testing.T, w *sim.World, id uint64) *spacecraft.Spacecraft {
	t.Helper()
	old := w.Crafts[0]
	st, ok := spacecraft.BuildStage("rcs-tug")
	if !ok {
		t.Fatal("no rcs-tug part")
	}
	tug := spacecraft.NewFromStages([]spacecraft.Stage{st})
	tug.Primary, tug.SystemIdx = old.Primary, old.SystemIdx
	tug.State = physics.StateVector{R: old.State.R, V: old.State.V, M: tug.TotalMass()}
	tug.CurrentAttitudeDir = old.State.V.Unit()
	tug.ID = id
	w.Crafts[0] = tug
	return tug
}

// arrangePortContact turns both worlds' vessels into RCS Tugs and sets the
// guest 0.4 m port to port ahead of the host's nose, drifting in at 0.05 m/s,
// 2 degrees off straight: inside the port gate (0.5 m / 0.1 m/s / 4 degrees),
// so the host's magnet starts on the first tick the host sees the guest's
// report. The host is the approacher (the one ticking first).
func arrangePortContact(t *testing.T, hw, gw *sim.World, guestID uint64) {
	t.Helper()
	arrangePortContactAt(t, hw, gw, guestID, 0.4)
}

// arrangePortContactAt is arrangePortContact with the port gap chosen.
func arrangePortContactAt(t *testing.T, hw, gw *sim.World, guestID uint64, gapM float64) {
	t.Helper()
	host := tugIn(t, hw, hw.Crafts[0].ID)
	guest := tugIn(t, gw, guestID)
	axis := host.CurrentAttitudeDir.Unit()
	off := sim.VesselPortGeometry(host).Ports[0].OffsetM
	gAxis := orbital.Rotate(axis.Scale(-1), orbital.Vec3{Z: 1}, 2*math.Pi/180).Unit()
	portHost := host.State.R.Add(axis.Scale(off))
	guest.State.R = portHost.Add(axis.Scale(gapM)).Sub(gAxis.Scale(off))
	guest.State.V = host.State.V.Sub(axis.Scale(0.05))
	guest.CurrentAttitudeDir = gAxis
	guest.SlewRateDegPerSec = 1e-9 // hold the placed pointing
}
