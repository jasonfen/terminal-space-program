package screens

import (
	"math"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
	"github.com/jasonfen/terminal-space-program/internal/tui/widgets"
)

// chipLine returns the first chip ROW (ANSI stripped, label first after the
// two-space indent) for label, so "port:" does not match the header's
// "port: CM".
func chipLine(chip []string, label string) string {
	for _, l := range chip {
		if s := ansi.Strip(l); strings.HasPrefix(s, "  "+label) {
			return s
		}
	}
	return ""
}

func stagesOf(t *testing.T, ids ...string) []spacecraft.Stage {
	t.Helper()
	var out []spacecraft.Stage
	for _, id := range ids {
		st, ok := spacecraft.BuildStage(id)
		if !ok {
			t.Fatalf("no catalog part %q", id)
		}
		out = append(out, st)
	}
	return out
}

// portedPairWorld puts the active vessel (stacks own) and the targeted
// vessel (stacks tgt) with their ports exactly offFromTgtPort (LVLH
// metres along/radial/cross) apart, noses opposed unless tiltDeg tips the
// active vessel about its port. The target's nose points along-track.
func portedPairWorld(t *testing.T, own, tgt []string, offFromTgtPort orbital.Vec3, tiltDeg float64) *sim.World {
	t.Helper()
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.SpawnCraft(sim.SpawnSpec{AltitudeM: 400e3}); err != nil {
		t.Fatal(err)
	}
	w.ActiveCraftIdx = 0
	a, b := w.Crafts[0], w.Crafts[1]
	b.Primary = a.Primary
	a.Stages, b.Stages = stagesOf(t, own...), stagesOf(t, tgt...)
	b.State = a.State
	frame, ok := orbital.LVLHBasis(b.State.R, b.State.V)
	if !ok {
		t.Fatal("no LVLH frame")
	}
	b.CurrentAttitudeDir = frame.AlongTrack
	r := tiltDeg * math.Pi / 180 // tilt in the radial/along plane, about the port
	a.CurrentAttitudeDir = frame.AlongTrack.Scale(-math.Cos(r)).Add(frame.RadialOut.Scale(math.Sin(r)))
	ga, gb := sim.VesselPortGeometry(a), sim.VesselPortGeometry(b)
	pb := b.State.R
	if gb.HasPort() {
		pb = gb.PortPoint(b.State.R, 0)
	}
	pa := pb.Add(frame.AlongTrack.Scale(offFromTgtPort.X)).
		Add(frame.RadialOut.Scale(offFromTgtPort.Y)).
		Add(frame.CrossTrack.Scale(offFromTgtPort.Z))
	ownOff := ga.NoseM
	if ga.HasPort() {
		ownOff = ga.Ports[0].OffsetM
	}
	a.State.R = pa.Sub(a.CurrentAttitudeDir.Scale(ownOff))
	a.State.V = b.State.V
	w.SetTargetCraft(1)
	w.ViewMode = sim.ViewProximity
	return w
}

// The port rows, at Design Size, read the numbers the sim derived.
func TestProximityChipPortRows(t *testing.T) {
	w := portedPairWorld(t, []string{"rcs-tug"}, []string{"nudge-tug"}, orbital.Vec3{X: 1.1, Y: 0.6, Z: 0.2}, 0)
	v := NewOrbitView(chipTestTheme())
	v.Resize(DesignWidth, DesignHeight)
	chip := v.buildProximityChip(w)

	if h := ansi.Strip(chip[0]); !strings.HasSuffix(h, "port: Nudge Tug") || !strings.HasPrefix(h, "PROXIMITY  ") {
		t.Errorf("header = %q, want PROXIMITY <name> port: Nudge Tug", h)
	}
	port := chipLine(chip, "port:")
	if !strings.Contains(port, "1.27 m") || !strings.Contains(port, "(need < 0.50)") {
		t.Errorf("port row = %q, want 1.27 m (|(1.1,0.6,0.2)|) with the gate reminder", port)
	}
	if got := chipLine(chip, "misalign:"); !strings.Contains(got, "0° off") || strings.Contains(got, "need") {
		t.Errorf("align row = %q, want 0° off with no reminder", got)
	}
	if got := chipLine(chip, "lateral:"); !strings.Contains(got, "0.6 m ↓") || !strings.Contains(got, "0.2 m ◂") {
		t.Errorf("off-axis row = %q, want 0.6 m ↓  0.2 m ◂ (their port is below and to the left)", got)
	}
	// Order: header, range, port, align, off-axis, rel speed, ...
	order := []string{"PROXIMITY", "range:", "port:", "misalign:", "lateral:", "rel speed:", "closing:"}
	last := -1
	for _, label := range order {
		idx := -1
		for i, l := range chip {
			if s := ansi.Strip(l); strings.HasPrefix(s, "  "+label) || (i == 0 && label == "PROXIMITY") {
				idx = i
				break
			}
		}
		if idx <= last {
			t.Errorf("row %q out of order (index %d after %d)", label, idx, last)
		}
		last = idx
	}
}

// (need < 0.50) and (need < 4°) appear exactly when the pair is inside
// 50 m and that term is failing.
func TestProximityNeedSuffixesTrackTheirTerm(t *testing.T) {
	type c struct {
		name      string
		off       orbital.Vec3 // port offset
		tiltDeg   float64
		wantPort  bool
		wantAlign bool
	}
	cases := []c{
		{"0.4 m aligned: both terms hold", orbital.Vec3{X: 0.4}, 0, false, false},
		{"1.1 m aligned: port fails", orbital.Vec3{X: 1.1}, 0, true, false},
		{"0.3 m, 6 deg: align fails", orbital.Vec3{X: 0.3}, 6, false, true},
		{"3 m, 20 deg: both fail", orbital.Vec3{X: 3}, 20, true, true},
		{"3.9 deg is inside 4", orbital.Vec3{X: 0.3}, 3.9, false, false},
	}
	for _, k := range cases {
		w := portedPairWorld(t, []string{"rcs-tug"}, []string{"nudge-tug"}, k.off, k.tiltDeg)
		v := NewOrbitView(chipTestTheme())
		v.Resize(DesignWidth, DesignHeight)
		chip := v.buildProximityChip(w)
		if got := strings.Contains(chipLine(chip, "port:"), "(need < 0.50)"); got != k.wantPort {
			t.Errorf("%s: port suffix = %v, want %v (%q)", k.name, got, k.wantPort, chipLine(chip, "port:"))
		}
		if got := strings.Contains(chipLine(chip, "misalign:"), "(need < 4°)"); got != k.wantAlign {
			t.Errorf("%s: align suffix = %v, want %v (%q)", k.name, got, k.wantAlign, chipLine(chip, "misalign:"))
		}
	}
	// Outside 50 m neither shows, however badly the terms fail; off-axis is hidden.
	far := portedPairWorld(t, []string{"rcs-tug"}, []string{"nudge-tug"}, orbital.Vec3{X: 120}, 0)
	v := NewOrbitView(chipTestTheme())
	v.Resize(DesignWidth, DesignHeight)
	chip := v.buildProximityChip(far)
	if strings.Contains(strings.Join(chip, "\n"), "need < 0.50") || strings.Contains(strings.Join(chip, "\n"), "need < 4°") {
		t.Errorf("a reminder shows outside 50 m:\n%s", strings.Join(chip, "\n"))
	}
	if chipLine(chip, "lateral:") != "" {
		t.Error("off-axis row must be hidden beyond 50 m")
	}
	if chipLine(chip, "misalign:") == "" {
		t.Error("align row should still read beyond 50 m")
	}
}

// A port-less target says so, and the numbers it cannot have are not drawn.
func TestProximityChipPortlessTarget(t *testing.T) {
	cases := []struct {
		name, head, row string
		tgt             []string
	}{
		{"bare S-IVB", "no port", "none (S-IVB has no port)", []string{"s-ivb-1"}},
		{"loaded carrier", "no port", "none (top is Relay Comsat)", []string{"ntr-tug", "relay-comsat"}},
		// longest port-less part name: still inside the chip, shortened.
		{"long name", "no port", "none (Ground Station)", []string{"ground-station-lander"}},
	}
	for _, c := range cases {
		w := portedPairWorld(t, []string{"rcs-tug"}, c.tgt, orbital.Vec3{X: 5}, 0)
		v := NewOrbitView(chipTestTheme())
		v.Resize(DesignWidth, DesignHeight)
		chip := v.buildProximityChip(w)
		if h := ansi.Strip(chip[0]); !strings.HasSuffix(h, c.head) {
			t.Errorf("%s: header %q, want suffix %q", c.name, h, c.head)
		}
		if got := chipLine(chip, "port:"); !strings.Contains(got, c.row) {
			t.Errorf("%s: port row %q, want %q", c.name, got, c.row)
		}
		if chipLine(chip, "misalign:") != "" || chipLine(chip, "lateral:") != "" {
			t.Errorf("%s: align/off-axis must not be drawn without a port", c.name)
		}
	}
}

// The chip stays inside the 41-cell box (39 inside) with every row drawn,
// including the worst-case header and port-less notes.
func TestProximityChipWidthWithEveryRow(t *testing.T) {
	check := func(name string, w *sim.World) {
		t.Helper()
		v := NewOrbitView(chipTestTheme())
		v.Resize(DesignWidth, DesignHeight)
		for i, l := range v.buildProximityChip(w) {
			if got := lipgloss.Width(l); got > proximityChipInnerWidth {
				t.Errorf("%s: row %d is %d cells, want <= %d: %q", name, i, got, proximityChipInnerWidth, ansi.Strip(l))
			}
		}
	}
	// Every row on: ported pair inside 50 m, failing every term, speed over.
	w := portedPairWorld(t, []string{"rcs-tug"}, []string{"heartbeat-keeper"}, orbital.Vec3{X: 3.2, Y: 0.6, Z: 0.2}, 20)
	w.Crafts[0].Name = "A Very Long Vessel Name-12"
	w.Crafts[1].Name = "Another Very Long Vessel Name-7"
	w.Crafts[0].State.V = w.Crafts[0].State.V.Add(orbital.Vec3{X: 0.9})
	check("every row", w)
	check("portless long names", portedPairWorld(t, []string{"rcs-tug"}, []string{"ground-station-lander"}, orbital.Vec3{X: 5}, 0))
	check("yours portless", portedPairWorld(t, []string{"s-ic"}, []string{"heartbeat-keeper"}, orbital.Vec3{X: 5}, 0))

	// And as a placed chip at Design Size the box is 41 cells at most.
	v := NewOrbitView(chipTestTheme())
	v.Resize(DesignWidth, DesignHeight)
	v.Render(w, 0, DesignWidth, DesignHeight)
	found := false
	for _, r := range v.chipRects {
		if r.id != "" { // the PROXIMITY chip is the always-on one
			continue
		}
		found = true
		if wcells := r.colEnd - r.colStart + 1; wcells > 41 {
			t.Errorf("the placed PROXIMITY chip is %d cells wide, want <= 41", wcells)
		}
	}
	if !found {
		t.Error("no always-on chip rect found: the check above measured nothing")
	}
}

// stackRows/parity: the sim's row arithmetic (which sim cannot take from
// this package) must equal what the sprite composer draws, for every
// catalog loadout and for stages derived the way the VAB derives them.
func TestDockPortNoseRowsMatchDrawnSprite(t *testing.T) {
	check := func(name string, stages []spacecraft.Stage) {
		t.Helper()
		if got, want := sim.StackRows(stages), composedStackRows(stages); got != want {
			t.Errorf("%s: sim.StackRows = %d, drawn composedStackRows = %d", name, got, want)
		}
		if got, want := sim.StackWidthPx(stages), composedStackMaxWidth(stages); got != want {
			t.Errorf("%s: sim.StackWidthPx = %d, drawn composedStackMaxWidth = %d", name, got, want)
		}
	}
	for id, l := range spacecraft.Loadouts {
		check("loadout "+id, l.Stages)
		// Every prefix too: the stack after each staging press.
		for n := 1; n < len(l.Stages); n++ {
			check(id+" prefix", l.Stages[:n])
			check(id+" suffix", l.Stages[n:])
		}
	}
	// VAB-style derived stages: arbitrary rows/widths, a zero-row stage
	// skipped by the composer, tall pairs that grow a taper.
	derived := []spacecraft.Stage{
		{LaunchSpriteRowsPx: 9, LaunchSpriteWidthPx: 3, LaunchSpriteDerived: true},
		{LaunchSpriteRowsPx: 0, LaunchSpriteWidthPx: 3},
		{LaunchSpriteRowsPx: 28, LaunchSpriteWidthPx: 5, LaunchSpriteDerived: true},
		{LaunchSpriteRowsPx: 3},
		{LaunchSpriteRowsPx: 6, LaunchSpriteWidthPx: 1},
		{LaunchSpriteRowsPx: 7, LaunchSpriteWidthPx: 2},
	}
	check("derived", derived)
	// Hand-worked pin (the parity above would pass for any shared number):
	// SLS = core 34 rows + taper + separator + ICPS 8 = 44; the boosters
	// stand beside the core and add nothing; extent 2*4 offset + 2 wide = 10.
	sls := spacecraft.Loadouts["SLS-Block1"].Stages
	if got := sim.StackRows(sls); got != 44 {
		t.Errorf("SLS sim.StackRows = %d, want 44", got)
	}
	if got := sim.StackWidthPx(sls); got != 10 {
		t.Errorf("SLS sim.StackWidthPx = %d, want 10", got)
	}
	// The metres line (ADR 0061): the published envelope, hand-written from
	// the ADR's "Numbers" (SLS = 8.4 core + 2 x 3.7 boosters + 2 x 1.5 m gaps).
	for id, want := range map[string]float64{"SLS-Block1": 18.8, "Apollo-TLI": 6.6, "Dragon": 4.0} {
		if got := sim.StackWidthM(spacecraft.Loadouts[id].Stages); math.Abs(got-want) > 1e-9 {
			t.Errorf("%s sim.StackWidthM = %v, want %v", id, got, want)
		}
	}
}

// The mark sits where the sprite's nose is drawn. Measured, not asserted
// from the formula: compose the sprite at several in-plane attitudes and
// compare its farthest extent along the axis with the port's drawn offset.
func TestProximityPortMarkLandsOnDrawnNose(t *testing.T) {
	basis := widgets.Basis{X: orbital.Vec3{X: 1}, Y: orbital.Vec3{Y: 1}}
	stack := stagesOf(t, "s-ivb-1", "apollo-descent", "apollo-ascent", "apollo-sm", "command-module")
	g := sim.PortGeometryOf(stack, orbital.Vec3{Y: 1})
	if !g.HasPort() {
		t.Fatal("setup: Apollo TLI stack must have a port")
	}
	for _, deg := range []float64{0, 17, 45, 90, 135, 200, 310} {
		r := deg * math.Pi / 180
		att := orbital.Vec3{X: math.Sin(r), Y: math.Cos(r)}
		sprite := ComposeLaunchSprite(stack, att, basis, vesselSubPixelM)
		if len(sprite) == 0 {
			t.Fatal("no sprite")
		}
		sx, sy := stackDirScreen(att, basis)
		axis := orbital.Vec3{X: sx, Y: sy}
		minAlong, maxAlong := math.Inf(1), math.Inf(-1)
		for _, p := range sprite {
			a := p.OffsetWorld.Dot(axis)
			minAlong, maxAlong = math.Min(minAlong, a), math.Max(maxAlong, a)
		}
		// Dots are the centres of 1.5 m cells: the drawn hull ends half a
		// cell beyond the extreme dots, at both ends.
		drawnTop := maxAlong + vesselSubPixelM/2
		drawnBottom := minAlong - vesselSubPixelM/2
		got := proximityPortWorld(orbital.Vec3{}, att, basis, g.Ports[0].OffsetM).Dot(axis)
		if math.Abs(got-drawnTop) > 1e-6 {
			t.Errorf("attitude %g deg: port drawn at %.3f m along the axis, sprite's nose edge is at %.3f m", deg, got, drawnTop)
		}
		if math.Abs((drawnTop-drawnBottom)-g.NoseM) > 1e-6 {
			t.Errorf("attitude %g deg: drawn hull is %.3f m long, sim says %.3f m", deg, drawnTop-drawnBottom, g.NoseM)
		}
	}
}


// Compact Form (80x24): port: and align: survive the shrink only inside
// 50 m and out of gate; off-axis: never does.
func TestProximityCompactFormPortRows(t *testing.T) {
	compact := func(w *sim.World) []string {
		v := newProximityTestView(t, 80, 24)
		return v.buildProximityChipCompact(w)
	}
	// Inside 50 m, 1.1 m, 6 deg off: out of gate.
	out := compact(portedPairWorld(t, []string{"rcs-tug"}, []string{"nudge-tug"}, orbital.Vec3{X: 1.1}, 6))
	if chipLine(out, "port:") == "" || chipLine(out, "misalign:") == "" {
		t.Errorf("out-of-gate Compact Form must keep port: and align:, got %q", out)
	}
	if chipLine(out, "lateral:") != "" {
		t.Errorf("off-axis: must be dropped in Compact Form, got %q", out)
	}
	if got := chipLine(out, "port:"); !strings.Contains(got, "(need < 0.50)") {
		t.Errorf("Compact port row lost its reminder: %q", got)
	}
	// Beyond 50 m: name and range only.
	if far := compact(portedPairWorld(t, []string{"rcs-tug"}, []string{"nudge-tug"}, orbital.Vec3{X: 120}, 6)); len(far) != 2 {
		t.Errorf("Compact Form beyond 50 m = %q, want name and range only", far)
	}
	// In gate (0.3 m, aligned, matched velocity): name and range only.
	if in := compact(portedPairWorld(t, []string{"rcs-tug"}, []string{"nudge-tug"}, orbital.Vec3{X: 0.3}, 0)); len(in) != 2 {
		t.Errorf("Compact Form in gate = %q, want name and range only", in)
	}
	// Latched in the gate: the port row comes back with the latched line, since
	// the latch is the one reason a pair in the gate does not dock (#794).
	lw := latchedPortedWorld(t)
	for _, l := range compact(lw) {
		if lipgloss.Width(l) > proximityChipInnerWidth {
			t.Errorf("Compact latched row %q wider than %d cells", l, proximityChipInnerWidth)
		}
	}
	if got := strings.Join(compact(lw), "\n"); !strings.Contains(got, "latched: back 5 m or "+ReArmKeyLabel) || chipLine(compact(lw), "port:") == "" {
		t.Errorf("latched Compact Form = %q, want the port: row and the latched line", got)
	}
	// A port-less target inside 50 m always says why.
	if pl := compact(portedPairWorld(t, []string{"rcs-tug"}, []string{"s-ivb-1"}, orbital.Vec3{X: 20}, 0)); !strings.Contains(chipLine(pl, "port:"), "none (S-IVB has no port)") {
		t.Errorf("port-less Compact Form = %q, want the port: none row", pl)
	}
	// Every Compact line fits the chip.
	for i, l := range out {
		if lipgloss.Width(l) > proximityChipInnerWidth {
			t.Errorf("compact row %d is %d cells: %q", i, lipgloss.Width(l), ansi.Strip(l))
		}
	}
}

// Both marks and the line's length label survive on the canvas together
// even when the gap is only a few cells: the label must not sit on a mark.
func TestProximityPortLabelAndMarksCoexist(t *testing.T) {
	w := portedPairWorld(t, []string{"rcs-tug"}, []string{"nudge-tug"}, orbital.Vec3{X: 2, Y: 0.6, Z: 0.2}, 6)
	v := newProximityTestView(t, DesignWidth, DesignHeight)
	v.Render(w, 0, DesignWidth, DesignHeight)
	st, _ := w.ProximityState()
	v.userZoom = 3
	v.panOffset = st.Frame.AlongTrack.Scale(6)
	out := ansi.Strip(v.Render(w, 0, DesignWidth, DesignHeight))
	if !v.proxOwnSpriteShown || !v.proxTargetSpriteShown {
		t.Fatal("setup: both hulls must be drawn as sprites")
	}
	if got := strings.Count(out, "○"); got != 2 {
		t.Errorf("port marks on screen = %d, want 2 (one per hull)", got)
	}
	// Once in the chip's port: row, once as the line's label, once on the
	// Centerline ring (ADR 0055 slice 2).
	if got := strings.Count(out, "2.10 m"); got != 3 {
		t.Errorf("\"2.10 m\" appears %d times, want 3 (chip row + the port line's label + the Centerline ring's):\n%s", got, out)
	}

	// Dragons 0.3 m apart along track and 0.6 m up put one mark on the label's
	// row: the label used to be written under it and read `3.0○ m` (#759, ADR
	// 0055 B6). It must now sit clear of both marks.
	w = portedPairWorld(t, []string{"dragon-crew"}, []string{"dragon-cargo"}, orbital.Vec3{X: 0.3, Y: 0.6, Z: 3}, 0)
	v = newProximityTestView(t, DesignWidth, DesignHeight)
	v.Render(w, 0, DesignWidth, DesignHeight)
	v.userZoom = 3
	out = ansi.Strip(v.Render(w, 0, DesignWidth, DesignHeight))
	if got := strings.Count(out, "○"); got != 2 {
		t.Errorf("port marks on screen = %d, want 2", got)
	}
	if strings.Contains(out, "3.0○") || strings.Contains(out, "○ m") || strings.Contains(out, "3.○") {
		var rows []string
		for _, l := range strings.Split(out, "\n") {
			if strings.Contains(l, "○") {
				rows = append(rows, l)
			}
		}
		t.Errorf("a port mark sits inside the port line's label:\n%s", strings.Join(rows, "\n"))
	}
	if !strings.Contains(out, "3.0") {
		t.Error("the port line's label is gone")
	}
}

// A ghost has no known shape yet (slice E): the chip says so rather than
// guess, draws no align/off-axis rows, and the hull stays a glyph.
func TestProximityGhostTargetPortUnknown(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.SpawnCraft(sim.SpawnSpec{AltitudeM: 400e3}); err != nil {
		t.Fatal(err)
	}
	w.ActiveCraftIdx = 0
	a := w.ActiveCraft()
	w.Ghosts = []sim.Ghost{{Owner: "SHA256:g", CraftID: 9, Handle: "pal", Name: "Tug", PrimaryID: a.Primary.ID,
		Pos: w.BodyPosition(a.Primary).Add(a.State.R).Add(orbital.Vec3{X: 40}), Vel: a.State.V}}
	w.SetTargetGhost("SHA256:g", 9)
	w.ViewMode = sim.ViewProximity
	v := NewOrbitView(chipTestTheme())
	v.Resize(DesignWidth, DesignHeight)
	chip := v.buildProximityChip(w)
	if h := ansi.Strip(chip[0]); !strings.HasSuffix(h, "port: unknown") {
		t.Errorf("ghost header = %q, want it to end in port: unknown", h)
	}
	if got := chipLine(chip, "port:"); !strings.Contains(got, "unknown") {
		t.Errorf("ghost port row = %q", got)
	}
	if chipLine(chip, "misalign:") != "" || chipLine(chip, "lateral:") != "" {
		t.Error("a ghost with no known shape must not draw align/off-axis")
	}
}
