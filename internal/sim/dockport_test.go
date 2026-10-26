package sim

import (
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

func stageOf(t *testing.T, partID string) spacecraft.Stage {
	t.Helper()
	st, ok := spacecraft.BuildStage(partID)
	if !ok {
		t.Fatalf("no catalog part %q", partID)
	}
	return st
}

func near(a, b, tol float64) bool { return math.Abs(a-b) <= tol }

// Row counts are counted by hand from parts.json rows (+1 separator per
// boundary between drawn stages, +1 taper where both neighbours are >= 6
// rows), not read back from the function under test. ADR 0053 decision 2
// lists the single-part lengths: RCS Tug 6 m, NTR tug 12 m, Endpoint
// Station 7.5 m, S-IVB 18 m.
func TestDockPortStackRowsHandCounted(t *testing.T) {
	cases := []struct {
		name  string
		parts []string
		rows  int
	}{
		{"S-IVB", []string{"s-ivb-1"}, 12},             // 12
		{"RCS Tug", []string{"rcs-tug"}, 4},            // 4
		{"NTR tug", []string{"ntr-tug"}, 8},            // 8
		{"Endpoint Station", []string{"endpoint-station"}, 5},
		// Descent 5 + sep 1 + Ascent 3 (no taper: both under 6).
		{"Lander", []string{"lander-descent", "lander-ascent"}, 9},
		// S-IC 24, S-II 20, S-IVB 12, Descent 5, Ascent 3, SM 6, CM 6 = 76;
		// 6 separators; tapers S-IC/S-II, S-II/S-IVB and SM/CM = 3.
		{"Apollo-Stack", []string{"s-ic", "s-ii", "s-ivb", "apollo-descent", "apollo-ascent", "apollo-sm", "command-module"}, 85},
	}
	for _, c := range cases {
		var stages []spacecraft.Stage
		for _, id := range c.parts {
			stages = append(stages, stageOf(t, id))
		}
		if got := StackRows(stages); got != c.rows {
			t.Errorf("%s: StackRows = %d, want %d", c.name, got, c.rows)
		}
		g := PortGeometryOf(stages, orbital.Vec3{Y: 1})
		if want := float64(c.rows) * 1.5; !near(g.NoseM, want, 1e-9) {
			t.Errorf("%s: NoseM = %g, want %g", c.name, g.NoseM, want)
		}
	}
}

// The port point is the nose measured from the stack BOTTOM along the
// axis (R + (L - half a row)*axis; see dockPortTipInsetM for why not
// exactly L). A vector test with a tilted axis, so a "centre +- half
// height" reading (the grill's wrong claim) cannot pass.
func TestDockPortPointIsNoseFromStackBottom(t *testing.T) {
	g := PortGeometryOf([]spacecraft.Stage{stageOf(t, "rcs-tug")}, orbital.Vec3{X: 3, Y: 4}) // not unit on purpose
	if !g.HasPort() {
		t.Fatal("RCS Tug must carry a port")
	}
	r := orbital.Vec3{X: 1000, Y: -2000, Z: 50}
	p := g.PortPoint(r, 0)
	want := r.Add(orbital.Vec3{X: 0.6 * 5.25, Y: 0.8 * 5.25}) // 6 m hull, nose edge 0.75 m in from L
	if d := p.Sub(want).Norm(); d > 1e-9 {
		t.Errorf("port point off by %g m: got %+v want %+v", d, p, want)
	}
}

// A vessel has a port when its TOP stage's part does.
func TestDockPortFollowsTopStage(t *testing.T) {
	bare := PortGeometryOf([]spacecraft.Stage{stageOf(t, "s-ivb-1")}, orbital.Vec3{Y: 1})
	if bare.HasPort() {
		t.Error("bare S-IVB has no port")
	}
	loaded := PortGeometryOf([]spacecraft.Stage{stageOf(t, "ntr-tug"), stageOf(t, "relay-comsat")}, orbital.Vec3{Y: 1})
	if loaded.HasPort() {
		t.Error("a carrier with a payload on top has no port")
	}
	if !loaded.Stacked || loaded.TopName != "Relay Comsat" {
		t.Errorf("loaded carrier: Stacked=%v TopName=%q", loaded.Stacked, loaded.TopName)
	}
	deployed := PortGeometryOf([]spacecraft.Stage{stageOf(t, "ntr-tug")}, orbital.Vec3{Y: 1})
	if !deployed.HasPort() {
		t.Error("the tug has its port back once the payloads are gone")
	}
	apollo := PortGeometryOf([]spacecraft.Stage{stageOf(t, "s-ivb-1"), stageOf(t, "apollo-descent"), stageOf(t, "apollo-ascent"), stageOf(t, "apollo-sm"), stageOf(t, "command-module")}, orbital.Vec3{Y: 1})
	if !apollo.HasPort() || apollo.Ports[0].Name != "CM" {
		t.Errorf("TLI stack port = %+v, want the CM's", apollo.Ports)
	}
}

func TestPortFaceBasis(t *testing.T) {
	f := orbital.LVLHFrame{
		AlongTrack: orbital.Vec3{X: 1},
		RadialOut:  orbital.Vec3{Y: 1},
		CrossTrack: orbital.Vec3{Z: 1},
	}
	// Axis along-track: up is radial, right is cross-track.
	up, right, ok := PortFaceBasis(orbital.Vec3{X: 1}, f)
	if !ok || up.Sub(f.RadialOut).Norm() > 1e-9 || right.Sub(f.CrossTrack).Norm() > 1e-9 {
		t.Errorf("along-track axis: up=%+v right=%+v", up, right)
	}
	// Axis tipped 30 degrees toward radial: up stays perpendicular to it.
	axis := orbital.Vec3{X: math.Cos(30 * math.Pi / 180), Y: math.Sin(30 * math.Pi / 180)}
	up, right, _ = PortFaceBasis(axis, f)
	for name, v := range map[string]orbital.Vec3{"up": up, "right": right} {
		if !near(v.Norm(), 1, 1e-9) || !near(v.Dot(axis), 0, 1e-9) {
			t.Errorf("%s not a unit vector across the face: %+v", name, v)
		}
	}
	if !near(up.Dot(right), 0, 1e-9) {
		t.Errorf("up.right = %g, want 0", up.Dot(right))
	}
	if right.Dot(f.CrossTrack) <= 0 {
		t.Error("right must agree with cross-track")
	}
	// Axis within 10 degrees of radial: up switches to along-track.
	up, _, _ = PortFaceBasis(orbital.Vec3{Y: 1, X: 0.05}, f)
	if math.Abs(up.Dot(f.AlongTrack)) < 0.99 {
		t.Errorf("near-radial axis: up = %+v, want along-track", up)
	}
	if _, _, ok := PortFaceBasis(orbital.Vec3{}, f); ok {
		t.Error("zero axis must report not ok")
	}
}

// dockPair builds a world whose active vessel (idx 0, stages own) and
// target (idx 1, stages tgt) sit with their ports exactly where the test
// says: the target's port at its own nose, the active's port offsetFromTgtPort
// away (LVLH metres: along, radial, cross), the noses pointing at each
// other unless the caller tilts ownAxis.
func dockPair(t *testing.T, own, tgt []string, offsetFromTgtPort orbital.Vec3, ownAxis orbital.Vec3) *World {
	t.Helper()
	w := mustWorld(t)
	if _, err := w.SpawnCraft(SpawnSpec{AltitudeM: 400e3}); err != nil {
		t.Fatalf("SpawnCraft: %v", err)
	}
	w.ActiveCraftIdx = 0
	a, b := w.Crafts[0], w.Crafts[1]
	b.Primary = a.Primary
	a.Stages, b.Stages = nil, nil
	for _, id := range own {
		a.Stages = append(a.Stages, stageOf(t, id))
	}
	for _, id := range tgt {
		b.Stages = append(b.Stages, stageOf(t, id))
	}
	// Target keeps its spawn state; its nose points along-track so the
	// port face's up is radial and right is cross-track.
	b.State = a.State
	frame, ok := orbital.LVLHBasis(b.State.R, b.State.V)
	if !ok {
		t.Fatal("no LVLH frame")
	}
	b.CurrentAttitudeDir = frame.AlongTrack
	if ownAxis.Norm() == 0 {
		ownAxis = frame.AlongTrack.Scale(-1)
	}
	a.CurrentAttitudeDir = ownAxis.Unit()
	gb := VesselPortGeometry(b)
	ga := VesselPortGeometry(a)
	pb := b.State.R
	if gb.HasPort() {
		pb = gb.PortPoint(b.State.R, 0)
	}
	pa := pb.
		Add(frame.AlongTrack.Scale(offsetFromTgtPort.X)).
		Add(frame.RadialOut.Scale(offsetFromTgtPort.Y)).
		Add(frame.CrossTrack.Scale(offsetFromTgtPort.Z))
	ownOff := ga.NoseM
	if ga.HasPort() {
		ownOff = ga.Ports[0].OffsetM
	}
	a.State.R = pa.Sub(a.CurrentAttitudeDir.Scale(ownOff))
	a.State.V = b.State.V
	w.SetTargetCraft(1)
	return w
}

func TestProximityStatePortRangeAlignAndOffAxis(t *testing.T) {
	// Active port 0.6 m above and 0.2 m to the cross-track side of the
	// target's port, 1.0 m out along its axis: range = |(1.0, 0.6, 0.2)|.
	w := dockPair(t, []string{"rcs-tug"}, []string{"nudge-tug"}, orbital.Vec3{X: 1.0, Y: 0.6, Z: 0.2}, orbital.Vec3{})
	st, ok := w.ProximityState()
	if !ok {
		t.Fatal("ProximityState refused")
	}
	if !st.HasPort || !st.OwnHasPort || !st.TargetHasPort || st.PortUnknown {
		t.Fatalf("flags: HasPort=%v own=%v target=%v unknown=%v", st.HasPort, st.OwnHasPort, st.TargetHasPort, st.PortUnknown)
	}
	if st.PortName != "Nudge Tug" {
		t.Errorf("PortName = %q, want the target's top component", st.PortName)
	}
	want := math.Sqrt(1.0 + 0.36 + 0.04)
	if !near(st.PortRangeM, want, 1e-6) {
		t.Errorf("PortRangeM = %g, want %g", st.PortRangeM, want)
	}
	if !near(st.AlignDeg, 0, 1e-6) {
		t.Errorf("AlignDeg = %g, want 0 for opposed noses", st.AlignDeg)
	}
	// Their port relative to mine: 0.6 below and 0.2 to the left (cross-track
	// is "right"), so the offsets read negative.
	if !near(st.OffAxisUpM, -0.6, 1e-6) || !near(st.OffAxisRightM, -0.2, 1e-6) {
		t.Errorf("off-axis up/right = %g / %g, want -0.6 / -0.2", st.OffAxisUpM, st.OffAxisRightM)
	}
	// The centre-to-centre range row is a different, larger number (the
	// hulls are 6 m and 4 m long): the port row must not be reading it.
	if st.PortRangeM >= st.RangeM {
		t.Errorf("PortRangeM %g should be well under hull-to-hull RangeM %g", st.PortRangeM, st.RangeM)
	}
}

func TestProximityStateAlignmentIsAngleToOpposedNose(t *testing.T) {
	w := dockPair(t, []string{"rcs-tug"}, []string{"nudge-tug"}, orbital.Vec3{X: 0.3}, orbital.Vec3{})
	tgt := w.Crafts[1]
	frame, _ := orbital.LVLHBasis(tgt.State.R, tgt.State.V)
	for _, deg := range []float64{0, 2, 6, 25} {
		r := deg * math.Pi / 180
		axis := frame.AlongTrack.Scale(-math.Cos(r)).Add(frame.RadialOut.Scale(math.Sin(r)))
		w.Crafts[0].CurrentAttitudeDir = axis
		st, _ := w.ProximityState()
		if !near(st.AlignDeg, deg, 1e-6) {
			t.Errorf("tilt %g deg: AlignDeg = %g", deg, st.AlignDeg)
		}
	}
	// Pointing the same way as the target is 180 off, not 0.
	w.Crafts[0].CurrentAttitudeDir = frame.AlongTrack
	if st, _ := w.ProximityState(); !near(st.AlignDeg, 180, 1e-6) {
		t.Errorf("same-way noses: AlignDeg = %g, want 180", st.AlignDeg)
	}
}

func TestProximityStatePortlessTargetsSayWhy(t *testing.T) {
	cases := []struct {
		name string
		tgt  []string
		note string
	}{
		{"bare stage", []string{"s-ivb-1"}, "none (S-IVB has no port)"},
		{"loaded carrier", []string{"ntr-tug", "relay-comsat"}, "none (top is Relay Comsat)"},
	}
	for _, c := range cases {
		w := dockPair(t, []string{"rcs-tug"}, c.tgt, orbital.Vec3{X: 5}, orbital.Vec3{})
		st, ok := w.ProximityState()
		if !ok {
			t.Fatalf("%s: refused", c.name)
		}
		if st.HasPort || st.TargetHasPort {
			t.Errorf("%s: reads ported (HasPort=%v target=%v)", c.name, st.HasPort, st.TargetHasPort)
		}
		if st.PortNote != c.note {
			t.Errorf("%s: PortNote = %q, want %q", c.name, st.PortNote, c.note)
		}
		if st.PortName != "" {
			t.Errorf("%s: PortName = %q, want empty", c.name, st.PortName)
		}
		if st.PortRangeM != 0 || st.AlignDeg != 0 {
			t.Errorf("%s: numbers must be undefined (zero) with no port, got %g / %g", c.name, st.PortRangeM, st.AlignDeg)
		}
	}
}

func TestProximityStateOwnPortlessStillNamesTargetPort(t *testing.T) {
	w := dockPair(t, []string{"s-ivb-1"}, []string{"rcs-tug"}, orbital.Vec3{X: 5}, orbital.Vec3{})
	st, _ := w.ProximityState()
	if st.HasPort || st.OwnHasPort || !st.TargetHasPort || st.PortName != "RCS Tug" {
		t.Errorf("own port-less: HasPort=%v own=%v target=%v name=%q", st.HasPort, st.OwnHasPort, st.TargetHasPort, st.PortName)
	}
	if st.PortNote != "none (yours: S-IVB has no port)" {
		t.Errorf("PortNote = %q", st.PortNote)
	}
}

// A ghost carries no shape (ADR 0053 decision 9, slice E fills it): the
// chip reads "unknown", never a guess.
func TestProximityStateGhostPortUnknown(t *testing.T) {
	w := mustWorld(t)
	if _, err := w.SpawnCraft(SpawnSpec{AltitudeM: 400e3}); err != nil {
		t.Fatal(err)
	}
	w.ActiveCraftIdx = 0
	a := w.ActiveCraft()
	w.Ghosts = []Ghost{{Owner: "SHA256:g", CraftID: 9, Handle: "pal", Name: "Tug", PrimaryID: a.Primary.ID,
		Pos: w.BodyPosition(a.Primary).Add(a.State.R).Add(orbital.Vec3{X: 40}), Vel: a.State.V}}
	w.SetTargetGhost("SHA256:g", 9)
	st, ok := w.ProximityState()
	if !ok {
		t.Fatal("refused")
	}
	if !st.PortUnknown || st.HasPort || st.PortNote != "unknown" {
		t.Errorf("ghost: unknown=%v HasPort=%v note=%q", st.PortUnknown, st.HasPort, st.PortNote)
	}
}

// ADR 0055 A6: PortAxialM is their port ahead of the plane through yours,
// along your axis. dockPair's X offset is your port out along the target's
// axis, and your axis points back along it, so X > 0 is "ahead".
func TestProximityStatePortAxialSign(t *testing.T) {
	for _, c := range []struct {
		name string
		x    float64
		want float64
	}{{"3 m ahead", 3, 3}, {"2 m past", -2, -2}} {
		w := dockPair(t, []string{"rcs-tug"}, []string{"nudge-tug"}, orbital.Vec3{X: c.x, Y: 0.6}, orbital.Vec3{})
		st, ok := w.ProximityState()
		if !ok || !st.HasPort {
			t.Fatalf("%s: no port scene", c.name)
		}
		if !near(st.PortAxialM, c.want, 1e-6) {
			t.Errorf("%s: PortAxialM = %g, want %g", c.name, st.PortAxialM, c.want)
		}
	}
}

// ADR 0061: a hull is as wide as its widest part's published diameter; the
// sprite rule (max(px, 4) x 1.5 m) only answers for a stage with no number.
func TestStackWidthMReadsTheWidestPartInMetres(t *testing.T) {
	sprite := func(rows, px int) spacecraft.Stage {
		return spacecraft.Stage{LaunchSpriteRowsPx: rows, LaunchSpriteWidthPx: px}
	}
	cases := []struct {
		name   string
		stages []spacecraft.Stage
		want   float64
	}{
		{"lone Dragon", []spacecraft.Stage{stageOf(t, "dragon-crew")}, 4.0},
		{"lone RCS Tug", []spacecraft.Stage{stageOf(t, "rcs-tug")}, 1.0},
		{"2 px, no diameter: floored to 4 px", []spacecraft.Stage{sprite(6, 2)}, 6.0},
		{"4 px, no diameter", []spacecraft.Stage{sprite(6, 4)}, 6.0},
		{"diameter 6.0 agrees with the floored sprite", []spacecraft.Stage{{LaunchSpriteRowsPx: 6, LaunchSpriteWidthPx: 2, DiameterM: 6.0}}, 6.0},
		{"5 px, no diameter", []spacecraft.Stage{sprite(6, 5)}, 7.5},
		{"undrawn stage is ignored", []spacecraft.Stage{{DiameterM: 9}, stageOf(t, "rcs-tug")}, 1.0},
		{"Apollo TLI: the S-IVB is the widest", []spacecraft.Stage{stageOf(t, "s-ivb-tli"), stageOf(t, "apollo-descent"), stageOf(t, "apollo-ascent"), stageOf(t, "apollo-sm"), stageOf(t, "orion-cm")}, 6.6},
		{"Falcon 9 + Starlink: the payload is wider than the stage", []spacecraft.Stage{stageOf(t, "f9-s1"), stageOf(t, "f9-s2"), stageOf(t, "starlink")}, 4.1},
	}
	for _, c := range cases {
		if got := StackWidthM(c.stages); !near(got, c.want, 1e-9) {
			t.Errorf("%s: StackWidthM = %g, want %g", c.name, got, c.want)
		}
	}
	// Hand-written from ADR 0061 "Numbers"; booster pair = core + 2 x booster + 2 x 1.5 m.
	want := map[string]float64{
		"Apollo-TLI": 6.6, "Saturn-V": 10.06, "SLS-Block1": 18.8, "SLS-Orion": 18.8, "Raster-LS": 18.8,
		"Falcon-9": 3.66, "Dragon": 4.0, "Falcon-9-Dragon": 4.0, "Falcon-9-Starlink-24": 4.1,
		"RCS-tug": 1.0, "Endpoint-Station": 4.76,
	}
	for id, w := range want {
		c := spacecraft.NewFromLoadout(id)
		if len(c.Stages) == 0 {
			t.Errorf("%s: no such loadout", id)
			continue
		}
		if got := StackWidthM(c.Stages); !near(got, w, 1e-9) {
			t.Errorf("%s: StackWidthM = %g, want %g", id, got, w)
		}
	}
}

func TestPortGeometryWidthIsStackWidthM(t *testing.T) {
	for _, id := range spacecraft.LoadoutOrder {
		c := spacecraft.NewFromLoadout(id)
		if got, want := VesselPortGeometry(c).WidthM, StackWidthM(c.Stages); got != want {
			t.Errorf("%s: WidthM = %g, StackWidthM = %g", id, got, want)
		}
	}
}
