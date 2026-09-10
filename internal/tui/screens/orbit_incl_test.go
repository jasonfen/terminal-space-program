package screens

import (
	"math"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/render"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
	"github.com/jasonfen/terminal-space-program/internal/tui/readout"
)

// TestRelativeInclinationVariesOverSiderealDay — the v0.11.4 Δi fix
// replaced `|i_a − i_b|` (scalar subtraction) with the true 3D plane
// angle between (R × V) and the target body's orbit normal. The
// scalar form was invariant under Earth's spin — a Landed pad-locked
// craft's instantaneous inclination stayed numerically constant
// across a sidereal day, so the readout pinned at the floor (~9° for
// KSC→Moon) while the actual relative-plane geometry oscillated to
// ~48° at the wing of the day. Pin the variance, not the exact
// numbers: assert the readout visits values both well below and well
// above the floor over a 24-hour sweep.
func TestRelativeInclinationVariesOverSiderealDay(t *testing.T) {
	th := Theme{
		Primary: lipgloss.NewStyle(),
		Warning: lipgloss.NewStyle(),
		Alert:   lipgloss.NewStyle(),
		Dim:     lipgloss.NewStyle(),
		HUDBox:  lipgloss.NewStyle().Border(lipgloss.RoundedBorder()),
		Footer:  lipgloss.NewStyle(),
		Title:   lipgloss.NewStyle(),
	}
	v := NewOrbitView(th)
	// v0.13 (ADR 0010): the Δi readout is now a canvas Chip, so the
	// canvas must be sized for it to land — Resize gives the orbit view
	// its real dimensions (the app always calls this; the pre-chip HUD
	// column rendered independent of canvas size).
	v.Resize(200, 80)
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c, err := w.SpawnCraft(sim.SpawnSpec{
		LoadoutID:       spacecraft.LoadoutSaturnVID,
		ParentBodyID:    "earth",
		Launchpad:       true,
		Latitude:        sim.DefaultLaunchpadLatitude,
		LongitudeOffset: sim.DefaultLaunchpadLongitudeEast,
	})
	if err != nil {
		t.Fatalf("SpawnCraft: %v", err)
	}
	if !c.Landed {
		t.Fatal("setup: launchpad spawn should set Landed=true")
	}

	sys := w.System()
	moonIdx := -1
	for i, b := range sys.Bodies {
		if b.EnglishName == "Moon" || b.ID == "moon" {
			moonIdx = i
			break
		}
	}
	if moonIdx <= 0 {
		t.Fatalf("moon not found in default system")
	}
	w.SetTargetBody(moonIdx)

	primary := c.Primary
	radius := primary.RadiusMeters()
	t0 := w.Clock.SimTime

	rerender := func(at time.Time) string {
		w.Clock.SimTime = at
		dir := render.BodyFixedToWorld(primary, c.LaunchLatDeg, c.LaunchLonDeg, at)
		c.State.R = orbital.Vec3{
			X: radius * dir.X,
			Y: radius * dir.Y,
			Z: radius * dir.Z,
		}
		omegaR := render.BodySpinOmegaWorld(primary)
		omega := orbital.Vec3{X: omegaR.X, Y: omegaR.Y, Z: omegaR.Z}
		c.State.V = omega.Cross(c.State.R)
		c.State.M = c.TotalMass()
		return v.Render(w, 0, 200, 80)
	}

	diRe := regexp.MustCompile(`Δincl:\s+([0-9]+\.[0-9]+)°`)
	extract := func(out string) (float64, bool) {
		m := diRe.FindStringSubmatch(out)
		if m == nil {
			return 0, false
		}
		f, err := strconv.ParseFloat(m[1], 64)
		if err != nil {
			return 0, false
		}
		return f, true
	}

	samples := make([]float64, 0, 5)
	for _, hours := range []int{0, 6, 12, 18, 24} {
		out := rerender(t0.Add(time.Duration(hours) * time.Hour))
		if !strings.Contains(out, "Δincl:") {
			t.Fatalf("h=%d: Δincl row missing from HUD output:\n%s", hours, out)
		}
		v, ok := extract(out)
		if !ok {
			t.Fatalf("h=%d: could not parse Δi from HUD output:\n%s", hours, out)
		}
		samples = append(samples, v)
	}

	var lo, hi = samples[0], samples[0]
	for _, s := range samples {
		if s < lo {
			lo = s
		}
		if s > hi {
			hi = s
		}
	}
	// Pin the variance, not exact numbers: a working formula sweeps
	// the true Δi range (KSC→Moon visits ~18°↔45° across a sidereal
	// day at the J2000 epoch). A floor-of-range value < 25° at one
	// sample, a ceiling-of-range value > 35° at another, and a sweep
	// ≥ 15° together capture "the readout meaningfully varies." The
	// broken `|i_a − i_b|` form was constant at ~9° across all
	// samples (latitude-locked vs Moon-plane-locked terms both
	// invariant under Earth's spin).
	if lo > 25 {
		t.Errorf("expected Δi floor below 25° at some sample (~18° at the J2000 epoch); samples=%v", samples)
	}
	if hi < 35 {
		t.Errorf("expected Δi ceiling above 35° at some sample (~45° at the J2000 epoch); samples=%v", samples)
	}
	if hi-lo < 15 {
		t.Errorf("expected Δi sweep ≥ 15° over a sidereal day; got %.1f° (samples=%v)", hi-lo, samples)
	}
}

// TestLandedPadShowsHeadingAndInclFloorNotLocked pins ADR 0049 decisions
// 9-10 (#453): a Landed craft's LAUNCH HUD no longer shows "launch
// lat: N° (locked)". It shows `heading:` (the commanded azimuth) and
// `incl:` with an explicit `(min N°)` Inclination Floor tag instead:
// "(locked)" is gone entirely, since nothing on the pad is locked (an
// equatorial pad reads "(min 0.0°)", not a lock either). At the
// default heading (HeadingTrim==0, due east) incl equals the floor,
// matching the pre-control-era number the old "launch lat:" row used
// to show. This test pins the new presentation of that same fact.
func TestLandedPadShowsHeadingAndInclFloorNotLocked(t *testing.T) {
	th := Theme{
		Primary: lipgloss.NewStyle(),
		Warning: lipgloss.NewStyle(),
		Alert:   lipgloss.NewStyle(),
		Dim:     lipgloss.NewStyle(),
		HUDBox:  lipgloss.NewStyle().Border(lipgloss.RoundedBorder()),
		Footer:  lipgloss.NewStyle(),
		Title:   lipgloss.NewStyle(),
	}
	v := NewOrbitView(th)
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	if _, err := w.SpawnCraft(sim.SpawnSpec{
		LoadoutID:       spacecraft.LoadoutSaturnVID,
		ParentBodyID:    "earth",
		Launchpad:       true,
		Latitude:        sim.DefaultLaunchpadLatitude,
		LongitudeOffset: sim.DefaultLaunchpadLongitudeEast,
	}); err != nil {
		t.Fatalf("SpawnCraft: %v", err)
	}
	v.Resize(200, 80) // realistic canvas; the 80×24 default is too short for the bordered chip stack
	out := v.Render(w, 0, 200, 80)
	if strings.Contains(out, "launch lat:") {
		t.Errorf("expected Landed HUD to drop the old 'launch lat:' row; got:\n%s", out)
	}
	if strings.Contains(out, "(locked)") {
		t.Errorf("expected Landed HUD to never say '(locked)'; got:\n%s", out)
	}
	headingRe := regexp.MustCompile(`heading:\s+(\d+°)`)
	hm := headingRe.FindStringSubmatch(out)
	if hm == nil {
		t.Fatalf("expected Landed HUD to show a 'heading:' row; got:\n%s", out)
	}
	if hm[1] != "090°" {
		t.Errorf("expected Landed HUD's default heading to read '090°' (due east, zero trim); got %q", hm[1])
	}
	inclRe := regexp.MustCompile(`incl:\s+([0-9.]+)°\s+\(min ([0-9.]+)°\)`)
	m := inclRe.FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("expected Landed HUD's 'incl:' row to carry a '(min N°)' Inclination Floor tag; got:\n%s", out)
	}
	// At zero heading trim (due east), incl equals the floor exactly:
	// the same number the pre-control-era 'launch lat:' row showed.
	if m[1] != m[2] {
		t.Errorf("at due-east (zero trim) expected incl == floor, got incl=%s° floor=%s°", m[1], m[2])
	}
}

// spawnLandedOnEarthAt28p6 is the shared setup for the SURFACE chip's
// pad-row tests below: a Saturn V Landed at the ADR 0049 worked
// example's 28.6° pad.
func spawnLandedOnEarthAt28p6(t *testing.T) (*sim.World, *spacecraft.Spacecraft) {
	t.Helper()
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c, err := w.SpawnCraft(sim.SpawnSpec{
		LoadoutID:       spacecraft.LoadoutSaturnVID,
		ParentBodyID:    "earth",
		Launchpad:       true,
		Latitude:        sim.DefaultLaunchpadLatitude,
		LongitudeOffset: sim.DefaultLaunchpadLongitudeEast,
	})
	if err != nil {
		t.Fatalf("SpawnCraft: %v", err)
	}
	if !c.Landed {
		t.Fatal("setup: launchpad spawn should set Landed=true")
	}
	return w, c
}

// TestLandedPadNoDeltaInclWithoutTarget: decision 11's third row is
// conditional on a target being set; buildLaunchChip must not print
// Δincl at all when Target.Kind == TargetNone (distinct from the
// TARGET chip, which simply doesn't render in that case; the SURFACE
// chip stays up either way and just drops the one row).
func TestLandedPadNoDeltaInclWithoutTarget(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	v.Resize(200, 80)
	w, _ := spawnLandedOnEarthAt28p6(t)
	if w.Target.Kind != sim.TargetNone {
		t.Fatalf("setup: expected no target, got %v", w.Target.Kind)
	}

	lines := v.buildLaunchChip(w)
	joined := strings.Join(lines, "\n")
	if strings.Contains(joined, "Δincl:") {
		t.Errorf("expected no Δincl row on the pad without a target set:\n%s", joined)
	}
}

// TestLandedPadShowsDeltaInclWithTarget: decision 11, while Landed
// with a body target set, the SURFACE chip's third row is Δincl, the
// plane angle a commanded-heading ascent lit now would leave to the
// target's plane. Distinguishes this row from the TARGET chip's own
// (pre-existing) Δincl by reading buildLaunchChip's lines directly
// rather than the whole composited HUD.
func TestLandedPadShowsDeltaInclWithTarget(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	v.Resize(200, 80)
	w, _ := spawnLandedOnEarthAt28p6(t)

	sys := w.System()
	moonIdx := -1
	for i, b := range sys.Bodies {
		if b.EnglishName == "Moon" || b.ID == "moon" {
			moonIdx = i
			break
		}
	}
	if moonIdx <= 0 {
		t.Fatalf("moon not found in default system")
	}
	w.SetTargetBody(moonIdx)

	lines := v.buildLaunchChip(w)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "Δincl:") {
		t.Fatalf("expected a Δincl row on the pad with a target set:\n%s", joined)
	}
}

// TestLandedPadDeltaInclTicksWithCommandedHeading proves the pad's
// Δincl row is actually reading the COMMANDED heading rather than
// silently pinned to the due-east co-rotation state (the exact bug
// #453 filed against the old 'launch lat: (locked)' row, one level
// up): a nonzero HeadingTrim must change the value versus the
// due-east baseline. This is the "prove the instrument returns a
// positive before trusting a negative" check: without it, a
// regression back to c.State.R.Cross(c.State.V) (which is
// heading-blind while Landed) would pass every other test in this
// file silently.
func TestLandedPadDeltaInclTicksWithCommandedHeading(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	v.Resize(200, 80)
	w, c := spawnLandedOnEarthAt28p6(t)

	sys := w.System()
	moonIdx := -1
	for i, b := range sys.Bodies {
		if b.EnglishName == "Moon" || b.ID == "moon" {
			moonIdx = i
			break
		}
	}
	if moonIdx <= 0 {
		t.Fatalf("moon not found in default system")
	}
	w.SetTargetBody(moonIdx)

	diRe := regexp.MustCompile(`Δincl:\s+([0-9.]+)°`)
	extract := func() float64 {
		lines := v.buildLaunchChip(w)
		joined := strings.Join(lines, "\n")
		m := diRe.FindStringSubmatch(joined)
		if m == nil {
			t.Fatalf("Δincl row missing:\n%s", joined)
		}
		f, err := strconv.ParseFloat(m[1], 64)
		if err != nil {
			t.Fatalf("could not parse Δincl: %v", err)
		}
		return f
	}

	dueEast := extract()
	// Due south (180°, offset +90°) rather than due west: west is
	// retrograde of east but in the SAME orbital plane, and Δincl is a
	// plane-to-plane angle that's deliberately symmetric under a
	// prograde/retrograde flip in one plane (confirmed by trying west
	// first: it read identically to east, correctly, since the fold
	// in relativePlaneAngleDeg treats a normal and its negation as the
	// same plane). South launches into a genuinely different plane.
	c.HeadingTrim = math.Pi / 2 // due south (180°)
	dueSouth := extract()

	if math.Abs(dueEast-dueSouth) < 1 {
		t.Errorf("Δincl did not change with commanded heading: due-east=%.4f° due-south=%.4f°", dueEast, dueSouth)
	}
}

// TestAirborneHeadingRejoinsTrimRowInclReverts pins decision 10: once
// airborne, the pad's dedicated heading:/incl: rows are gone; heading
// rejoins the trim: row instead, and incl: goes back to being the
// live orbital element (no '(min ...)' floor tag; that concept is
// pad-only).
func TestAirborneHeadingRejoinsTrimRowInclReverts(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	v.Resize(200, 80)
	w, c := spawnLandedOnEarthAt28p6(t)
	c.HeadingTrim = 5 * math.Pi / 180 // +5°, matches a single `}` tap
	c.Landed = false                  // simulate liftoff without re-deriving ascent state
	// A non-degenerate velocity so ElementsFromStateInFrame yields a
	// finite inclination rather than NaN/Inf (buildLaunchChip's "—"
	// fallback would make the row-presence assertions vacuous).
	c.State.V = orbital.Vec3{Y: 7800}

	lines := v.buildLaunchChip(w)
	joined := strings.Join(lines, "\n")
	if strings.Contains(joined, "(min") {
		t.Errorf("expected the Inclination Floor tag to disappear once airborne:\n%s", joined)
	}
	trimHeadingRe := regexp.MustCompile(`trim:\s+\S+\s+heading:\s+(\d+°)`)
	m := trimHeadingRe.FindStringSubmatch(joined)
	if m == nil {
		t.Fatalf("expected heading: to ride beside trim: once airborne:\n%s", joined)
	}
	if m[1] != "095°" {
		t.Errorf("airborne heading readout = %q, want %q (+5° from due east)", m[1], "095°")
	}
	// The pad's own dedicated "heading:" row (chipRow's own prefix, "  heading:")
	// must not additionally appear on its own line once airborne.
	for _, l := range lines {
		if strings.HasPrefix(strings.TrimLeft(l, " "), "heading:") {
			t.Errorf("expected no standalone 'heading:' row once airborne, found: %q", l)
		}
	}
}

// TestLaunchChipPadRowsAlignToColumn14 pins item4-B review finding 5:
// the pad's heading:/incl:/Δincl: rows must land their values at the
// same display column every other SURFACE row uses. Checked
// structurally, comparing where each row's value actually starts
// (accounting for each label's own display width via lipgloss.Width)
// against the pre-existing Pe: row's own column, rather than
// hardcoding degree values, so this survives unrelated numeric
// changes to the captured inclination/Δincl figures.
func TestLaunchChipPadRowsAlignToColumn14(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	v.Resize(200, 80)
	w, _ := spawnLandedOnEarthAt28p6(t)

	sys := w.System()
	moonIdx := -1
	for i, b := range sys.Bodies {
		if b.EnglishName == "Moon" || b.ID == "moon" {
			moonIdx = i
			break
		}
	}
	if moonIdx <= 0 {
		t.Fatalf("moon not found in default system")
	}
	w.SetTargetBody(moonIdx)

	lines := v.buildLaunchChip(w)

	rowFor := func(label string) string {
		t.Helper()
		for _, l := range lines {
			if strings.HasPrefix(l, "  "+label) {
				return l
			}
		}
		t.Fatalf("no row found with label %q in:\n%s", label, strings.Join(lines, "\n"))
		return ""
	}
	valueColumn := func(row, label string) int {
		t.Helper()
		prefix := "  " + label
		if !strings.HasPrefix(row, prefix) {
			t.Fatalf("row %q does not start with prefix %q", row, prefix)
		}
		rest := row[len(prefix):]
		spaces := 0
		for _, r := range rest {
			if r != ' ' {
				break
			}
			spaces++
		}
		return lipgloss.Width(prefix) + spaces
	}

	peRow := rowFor("Pe:")
	wantCol := valueColumn(peRow, "Pe:")
	if wantCol != launchChipValueCol {
		t.Fatalf("setup: Pe: row's own value column = %d, want %d (launchChipValueCol)", wantCol, launchChipValueCol)
	}

	for _, label := range []string{"heading:", readout.LabelIncl, readout.LabelDeltaIncl} {
		row := rowFor(label)
		gotCol := valueColumn(row, label)
		if gotCol != wantCol {
			t.Errorf("%s row's value column = %d, want %d (matching Pe:'s column): row=%q", label, gotCol, wantCol, row)
		}
	}
}

// TestInclinationFloorUsesCurrentSurfaceLatNotLaunchLat pins item4-B
// review finding 6: the pad's "(min N°)" Inclination Floor must read
// the vessel's CURRENT surface latitude (SurfaceLatLon, which prefers
// LandedLatDeg once a soft landing has set it) rather than the
// spawn-only LaunchLatDeg. A vessel that flew from the 28.6° pad and
// soft-landed at 5°N must read a 5° floor, not a stale 28.6° one sitting
// above its own incl: value (the review's own reproduction: "incl:
// 5.00° (min 28.61°)", nonsense since incl: can never be below its own
// floor).
func TestInclinationFloorUsesCurrentSurfaceLatNotLaunchLat(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	v.Resize(200, 80)
	w, c := spawnLandedOnEarthAt28p6(t)

	// Simulate a soft landing away from the launch pad: LandedLatDeg
	// set (SurfaceLatLon then prefers it over LaunchLatDeg per its own
	// doc comment), State.R repositioned to match so incl: itself also
	// reads the new position rather than the old one.
	const landedLat = 5.0
	const landedLon = 0.0
	c.LandedLatDeg = landedLat
	c.LandedLonDeg = landedLon
	radius := c.Primary.RadiusMeters()
	dir := render.BodyFixedToWorld(c.Primary, landedLat, landedLon, w.Clock.SimTime)
	c.State.R = orbital.Vec3{X: radius * dir.X, Y: radius * dir.Y, Z: radius * dir.Z}

	lines := v.buildLaunchChip(w)
	joined := strings.Join(lines, "\n")
	inclRe := regexp.MustCompile(`incl:\s+[0-9.]+°\s+\(min ([0-9.]+)°\)`)
	m := inclRe.FindStringSubmatch(joined)
	if m == nil {
		t.Fatalf("expected an 'incl: ... (min N°)' row:\n%s", joined)
	}
	floor, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		t.Fatalf("could not parse floor: %v", err)
	}
	if math.Abs(floor-landedLat) > 0.01 {
		t.Errorf("floor = %.2f°, want %.2f° (the current surface latitude, not the %.2f° launch latitude)",
			floor, landedLat, sim.DefaultLaunchpadLatitude)
	}
}
