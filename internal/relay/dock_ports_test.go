package relay

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// ADR 0053 slice E, the wire and the cross-player capture: the report carries
// the hull, the ghost decodes it, an old-shape report cannot be captured, and
// the Claim waits for the magnet.

const friendFP = "SHA256:friend"

// tugWorld is a world whose only vessel is an RCS Tug, nose along +Y (its
// prograde), on the default orbit.
func tugWorld(t *testing.T) (*sim.World, *spacecraft.Spacecraft) {
	t.Helper()
	w := newWorld(t)
	old := w.Crafts[0]
	st, ok := spacecraft.BuildStage("rcs-tug")
	if !ok {
		t.Fatal("no rcs-tug part")
	}
	tug := spacecraft.NewFromStages([]spacecraft.Stage{st})
	tug.Name = "Viewer"
	tug.Primary, tug.SystemIdx = old.Primary, old.SystemIdx
	tug.State = physics.StateVector{R: old.State.R, V: old.State.V, M: tug.TotalMass()}
	tug.CurrentAttitudeDir = old.State.V.Unit()
	tug.ID = old.ID
	w.Crafts[0] = tug
	return w, tug
}

// A report carries the attitude, hull length, hull width and port list the
// vessel derives for itself, and they survive JSON.
func TestReportCarriesAttitudeNoseWidthAndPort(t *testing.T) {
	w, tug := tugWorld(t)
	rep := reportFor(t, w, friendFP)
	cs := rep.Crafts[0]
	want := sim.VesselPortGeometry(tug)
	if !want.Known || !want.HasPort() {
		t.Fatal("fixture: the tug should have a known shape and a port")
	}
	if cs.Attitude != want.Axis {
		t.Errorf("attitude = %v, want %v", cs.Attitude, want.Axis)
	}
	if cs.NoseM != want.NoseM || cs.WidthM != want.WidthM {
		t.Errorf("hull = %v x %v, want %v x %v", cs.NoseM, cs.WidthM, want.NoseM, want.WidthM)
	}
	if len(cs.Ports) != 1 || cs.Ports[0].OffsetM != want.Ports[0].OffsetM || cs.Ports[0].Name != want.Ports[0].Name {
		t.Errorf("ports = %+v, want %+v", cs.Ports, want.Ports)
	}
	b, err := json.Marshal(cs)
	if err != nil {
		t.Fatal(err)
	}
	var back CraftState
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if back.Attitude != cs.Attitude || back.NoseM != cs.NoseM || back.WidthM != cs.WidthM || len(back.Ports) != 1 || back.Ports[0] != cs.Ports[0] {
		t.Errorf("round trip lost the hull: %+v -> %+v", cs, back)
	}
}

// A landed craft, or one with no attitude yet, sends no shape, and the keys
// are absent from the JSON (additive, omitempty).
func TestReportWithoutShapeOmitsTheKeys(t *testing.T) {
	w, tug := tugWorld(t)
	tug.CurrentAttitudeDir = orbital.Vec3{}
	rep := reportFor(t, w, friendFP)
	b, err := json.Marshal(rep.Crafts[0])
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"attitude", "nose_m", "width_m", "ports"} {
		if strings.Contains(string(b), `"`+key+`"`) {
			t.Errorf("a craft with no attitude wrote %q: %s", key, b)
		}
	}
	// A pre-slice-E report decodes to the zero shape.
	var old CraftState
	if err := json.Unmarshal([]byte(`{"id":7,"name":"Old","system":"Sol","primary":"earth","r":{"X":1,"Y":2,"Z":3},"v":{"X":4,"Y":5,"Z":6}}`), &old); err != nil {
		t.Fatal(err)
	}
	if old.Attitude != (orbital.Vec3{}) || old.NoseM != 0 || old.WidthM != 0 || old.Ports != nil {
		t.Errorf("a report without the fields decoded to a shape: %+v", old)
	}
}

// The ghost decodes the reported hull; a report without one yields a ghost
// whose shape is unknown (diamond, "port: unknown", never capturable).
func TestGhostDecodesReportedShape(t *testing.T) {
	wOwner, tug := tugWorld(t)
	rep := reportFor(t, wOwner, friendFP)
	viewer := newWorld(t)
	viewer.Clock.SimTime = rep.SubspaceTime
	ghosts := GhostsFor(viewer, []CraftReport{rep}, nil)
	if len(ghosts) != 1 {
		t.Fatalf("%d ghosts, want 1", len(ghosts))
	}
	g := ghosts[0]
	want := sim.VesselPortGeometry(tug)
	if g.Attitude != want.Axis || g.NoseM != want.NoseM || g.WidthM != want.WidthM {
		t.Errorf("ghost hull = %v %v x %v, want %v %v x %v", g.Attitude, g.NoseM, g.WidthM, want.Axis, want.NoseM, want.WidthM)
	}
	if len(g.Ports) != 1 || g.Ports[0].OffsetM != want.Ports[0].OffsetM || g.Ports[0].Name != want.Ports[0].Name {
		t.Errorf("ghost ports = %+v, want %+v", g.Ports, want.Ports)
	}
	if !sim.GhostPortGeometry(g).HasPort() {
		t.Error("ghost with a reported port has no port")
	}

	// The same report, minus the hull: an older build.
	rep.Crafts[0].Attitude, rep.Crafts[0].NoseM, rep.Crafts[0].WidthM, rep.Crafts[0].Ports = orbital.Vec3{}, 0, 0, nil
	g = GhostsFor(viewer, []CraftReport{rep}, nil)[0]
	if pg := sim.GhostPortGeometry(g); pg.Known || pg.HasPort() {
		t.Errorf("an old-shape report decoded to a known shape / port: %+v", pg)
	}
}

// contactWorld is a viewer tug and a friend's tug reported port to port,
// gapM apart along the viewer's nose, drifting together at relMS, the friend
// tilted tiltDeg off straight. Returns the viewer world, the friend's report
// and the friend's craft ID.
func contactWorld(t *testing.T, gapM, relMS, tiltDeg float64, withShape bool) (*sim.World, CraftReport) {
	t.Helper()
	w, tug := tugWorld(t)
	axis := tug.CurrentAttitudeDir.Unit()
	geo := sim.VesselPortGeometry(tug)
	portA := tug.State.R.Add(axis.Scale(geo.Ports[0].OffsetM))
	// The friend's nose points back at the viewer, tilted about the orbit
	// normal (world Z for this equatorial start).
	fAxis := orbital.Rotate(axis.Scale(-1), orbital.Vec3{Z: 1}, tiltDeg*math.Pi/180).Unit()
	fR := portA.Add(axis.Scale(gapM)).Sub(fAxis.Scale(geo.Ports[0].OffsetM))
	fV := tug.State.V.Sub(axis.Scale(relMS))
	cs := CraftState{
		ID: 42, Name: "Friend", System: w.System().Name, Primary: tug.Primary.ID,
		R: fR, V: fV,
	}
	if withShape {
		cs.Attitude, cs.NoseM, cs.WidthM = fAxis, geo.NoseM, geo.WidthM
		cs.Ports = []PortState{{OffsetM: geo.Ports[0].OffsetM, Name: geo.Ports[0].Name}}
	}
	w.Clock.WarpIdx = 0
	return w, CraftReport{Owner: friendFP, SubspaceTime: w.Clock.SimTime, Crafts: []CraftState{cs}}
}

// fly ticks the viewer's world and re-evaluates the ghost slate each tick,
// the way the serve layer does (Tick, refresh ghosts, detect), returning the
// sim seconds at which DetectGuestContact first reported contact, or -1.
func fly(w *sim.World, rep CraftReport, coupled map[string]bool, seconds float64) (at float64, owner string, id uint64) {
	start := w.Clock.SimTime
	for w.Clock.SimTime.Sub(start) < time.Duration(seconds*float64(time.Second)) {
		w.Tick()
		w.Ghosts = GhostsFor(w, []CraftReport{rep}, nil)
		if o, i, ok := DetectGuestContact(w, coupled); ok {
			return w.Clock.SimTime.Sub(start).Seconds(), o, i
		}
	}
	return -1, "", 0
}

// A ghost reported at 0.4 m / 0.05 m/s / 2 degrees starts the magnet; the
// contact (and so the Claim) arrives when the five-second pull completes, not
// before, and the ledger holds the resulting record.
func TestGuestContactClaimsWhenTheMagnetCompletes(t *testing.T) {
	w, rep := contactWorld(t, 0.4, 0.05, 2, true)
	at, owner, id := fly(w, rep, map[string]bool{friendFP: true}, 8)
	if at < 0 {
		t.Fatal("no contact in 8 s with the gate held")
	}
	if at < 4.9 || at > 5.3 {
		t.Errorf("contact after %.2f s, want ~5 (the magnet), not the tick the gate first held", at)
	}
	if owner != friendFP || id != 42 {
		t.Errorf("contact on %q/%d, want %q/42", owner, id, friendFP)
	}
	l := NewDockLedger()
	rec, claimed := l.Claim("me", "viewer", w.ActiveCraft().ID, owner, "friend", id)
	if !claimed || rec == nil || rec.GuestCraftID != 42 || rec.Phase != DockPending {
		t.Fatalf("claim on contact: %+v claimed=%v", rec, claimed)
	}
	// The ghost was never moved by the pull: the approacher's port is on the
	// friend's reported port.
	g := w.Ghosts[0]
	ga, gg := sim.VesselPortGeometry(w.ActiveCraft()), sim.GhostPortGeometry(g)
	if gap := gg.PortPoint(g.RelPos, 0).Sub(ga.PortPoint(w.ActiveCraft().State.R, 0)).Norm(); gap > 0.05 {
		t.Errorf("port gap at the Claim = %.3f m, want ~0", gap)
	}
}

// The old click: 25 m apart at 0.01 m/s, noses any way, would have claimed.
func TestGuestContactNoClickAtDistance(t *testing.T) {
	w, rep := contactWorld(t, 25, 0.01, 0, true)
	if at, _, _ := fly(w, rep, map[string]bool{friendFP: true}, 12); at >= 0 {
		t.Fatalf("claimed a ghost 25 m away at %.2f s", at)
	}
}

// An old-shape report (no attitude, hull or port) sitting exactly on the port
// at rest cannot be captured: the diamond has no port to dock at.
func TestGuestContactOldShapeReportCannotBeCaptured(t *testing.T) {
	w, rep := contactWorld(t, 0.2, 0, 0, false)
	if at, _, _ := fly(w, rep, map[string]bool{friendFP: true}, 12); at >= 0 {
		t.Fatalf("captured a ghost with no reported shape at %.2f s", at)
	}
}

// Only a co-warp coupled owner is dockable.
func TestGuestContactNeedsCoWarpCouple(t *testing.T) {
	w, rep := contactWorld(t, 0.4, 0.05, 2, true)
	if at, _, _ := fly(w, rep, map[string]bool{}, 8); at >= 0 {
		t.Fatalf("claimed an uncoupled ghost at %.2f s", at)
	}
}

// A turned nose reaches the partner before the heartbeat; sub-degree jitter
// and a flurry inside a second do not flood the wire.
func TestReporterReportsATurnedNoseBeforeTheHeartbeat(t *testing.T) {
	store := NewStore()
	ch, cancel := store.Subscribe()
	defer cancel()
	w, tug := tugWorld(t)
	rep := NewReporter(store, friendFP)
	now := time.Now()
	rep.Tick(w, now)
	if got := drain(ch); got != 1 {
		t.Fatalf("first tick: %d reports, want 1", got)
	}
	turn := func(deg float64) {
		tug.CurrentAttitudeDir = orbital.Rotate(tug.CurrentAttitudeDir, orbital.Vec3{Z: 1}, deg*math.Pi/180).Unit()
	}
	turn(0.3)
	rep.Tick(w, now.Add(2*time.Second))
	if got := drain(ch); got != 0 {
		t.Errorf("a 0.3 degree jitter fired %d reports", got)
	}
	turn(5)
	rep.Tick(w, now.Add(200*time.Millisecond)) // inside the rate limit
	if got := drain(ch); got != 0 {
		t.Errorf("a turn inside one second of the last report fired %d reports", got)
	}
	rep.Tick(w, now.Add(2*time.Second))
	if got := drain(ch); got != 1 {
		t.Errorf("a 5 degree turn: %d reports, want 1 before the heartbeat", got)
	}
}
