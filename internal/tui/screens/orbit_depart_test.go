package screens

import (
	"math"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/render"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
	"github.com/jasonfen/terminal-space-program/internal/tui/readout"
)

// departRe matches a `depart:` row's bare angle and optional `(best
// N°)` suffix, e.g. "depart:     22.13° (best 5.17°)" or a bare
// "depart:     28.61°" in orbit (decision 4: no suffix in flight).
var departRe = regexp.MustCompile(`depart:\s+([0-9]+\.[0-9]+)°(?:\s+\(best ([0-9]+\.[0-9]+)°\))?`)

func extractDepart(t *testing.T, joined string) (value float64, best float64, hasBest bool) {
	t.Helper()
	m := departRe.FindStringSubmatch(joined)
	if m == nil {
		t.Fatalf("expected a 'depart:' row; got:\n%s", joined)
	}
	v, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		t.Fatalf("could not parse depart value %q: %v", m[1], err)
	}
	if m[2] == "" {
		return v, 0, false
	}
	b, err := strconv.ParseFloat(m[2], 64)
	if err != nil {
		t.Fatalf("could not parse depart best %q: %v", m[2], err)
	}
	return v, b, true
}

// TestPadDepartRowSwingAndBest pins ADR 0050 decisions 1, 2, 4 per pad: the
// `depart:` row (measured against the plane the pad's body itself travels in
// around its primary, decision 2) reads somewhere inside the ADR's table swing
// and always carries `(best N°)`, the low end of that swing, since waiting can
// only improve a launch window, never worsen it. Earth's 5.17..52.05 is the
// closed form (obliquity 23.44 composed with the 28.6083 pad's launch
// inclination), verified against a 3600-sample sweep (cmd/verifydepart). Luna
// and Glyph are airless pads, whose row sits on the same NAVIGATION box (ADR
// 0050 insertion point 9a). Glyph, like Kern, only resolves via ParentBodyID
// once the view has browsed to Lumen; Mars is in the Sol system.
func TestPadDepartRowSwingAndBest(t *testing.T) {
	spawn := func(t *testing.T, w *sim.World, loadout, parent string) *spacecraft.Spacecraft {
		t.Helper()
		c, err := w.SpawnCraft(sim.SpawnSpec{
			LoadoutID:       loadout,
			ParentBodyID:    parent,
			Launchpad:       true,
			Latitude:        sim.DefaultLaunchpadLatitude,
			LongitudeOffset: sim.DefaultLaunchpadLongitudeEast,
		})
		if err != nil {
			t.Fatalf("SpawnCraft: %v", err)
		}
		if c.Primary.ID != parent {
			t.Fatalf("setup: expected craft primary = %s, got %q", parent, c.Primary.ID)
		}
		return c
	}
	cases := []struct {
		name         string
		world        func(t *testing.T) *sim.World
		lo, hi, best float64
	}{
		{"Earth", func(t *testing.T) *sim.World { w, _ := spawnLandedOnEarthAt28p6(t); return w }, 5.17, 52.05, 5.17},
		{"Luna", func(t *testing.T) *sim.World {
			w, _ := spawnLandedOnMoon(t, sim.DefaultLaunchpadLatitude, sim.DefaultLaunchpadLongitudeEast)
			return w
		}, 23.46, 33.75, 23.46},
		{"Mars", func(t *testing.T) *sim.World {
			w, err := sim.NewWorld()
			if err != nil {
				t.Fatalf("NewWorld: %v", err)
			}
			spawn(t, w, spacecraft.LoadoutSaturnVID, "mars")
			return w
		}, 4.80, 52.42, 4.80},
		{"Glyph", func(t *testing.T) *sim.World {
			w, err := sim.NewWorld()
			if err != nil {
				t.Fatalf("NewWorld: %v", err)
			}
			for i := 0; i < len(w.Systems) && w.Systems[w.SystemIdx].Name != "Lumen"; i++ {
				w.CycleSystem()
			}
			if w.Systems[w.SystemIdx].Name != "Lumen" {
				t.Fatalf("could not browse to Lumen (stuck viewing %q)", w.Systems[w.SystemIdx].Name)
			}
			if c := spawn(t, w, spacecraft.LoadoutKernStackID, "glyph"); c.Primary.Atmosphere != nil {
				t.Fatalf("setup: expected Glyph to be airless; got %+v", c.Primary.Atmosphere)
			}
			return w
		}, 22.61, 34.61, 22.61},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			v := NewOrbitView(chipTestTheme())
			joined := strings.Join(v.buildNavigationBox(c.world(t)), "\n")
			value, best, hasBest := extractDepart(t, joined)
			if !hasBest {
				t.Fatalf("expected the %s pad's depart: row to carry a '(best N°)' suffix; got:\n%s", c.name, joined)
			}
			if value < c.lo-0.01 || value > c.hi+0.01 {
				t.Errorf("depart value %.2f° outside the ADR's %s swing %.2f..%.2f", value, c.name, c.lo, c.hi)
			}
			if best < c.best-0.01 || best > c.best+0.01 {
				t.Errorf("depart best = %.2f°, want %.2f° (the ADR's %s pad best)", best, c.best, c.name)
			}
		})
	}
}

// TestOrbitChipShowsBareDepartRow pins ADR 0050 decisions 1, 2, 4: the
// ORBIT chip's depart: row sits directly under incl: (before
// direction:) and, unlike the pad's, carries no `(best N°)` suffix: an
// orbital plane doesn't move under two-body coast, so the lowest value
// reachable by waiting is the value already on screen, and printing it
// twice would restate the row above it on every orbit in the game.
func TestOrbitChipShowsBareDepartRow(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	v.Resize(120, 40)
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	joined := strings.Join(v.buildNavigationBox(w), "\n")
	if !strings.Contains(joined, readout.LabelIncl) {
		t.Fatalf("setup: ORBIT chip missing incl: row:\n%s", joined)
	}
	inclIdx := strings.Index(joined, readout.LabelIncl)
	departIdx := strings.Index(joined, "depart:")
	if departIdx < 0 {
		t.Fatalf("expected ORBIT chip to show a depart: row; got:\n%s", joined)
	}
	if departIdx < inclIdx {
		t.Errorf("expected depart: to sit directly under incl:, got depart: before incl: in:\n%s", joined)
	}
	if strings.Contains(joined, "(best") {
		t.Errorf("expected ORBIT's depart: row to carry no '(best N°)' suffix; got:\n%s", joined)
	}
}

// TestOrbitChipDepartRowReadsPastNinetyForRetrogradeOrbit is
// TestLandedPadDepartRowReadsPastNinetyForRetrogradeHeading's ORBIT-chip
// sibling (review r1 F6): a retrograde LEO's depart: row must also stay
// unfolded past 90° rather than folding into Δincl's [0, 90] range. The
// reviewer measured `depart: 156.6°` for a retrograde 500 km LEO.
func TestOrbitChipDepartRowReadsPastNinetyForRetrogradeOrbit(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	v.Resize(120, 40)
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	if _, err := w.SpawnCraft(sim.SpawnSpec{AltitudeM: 500e3, Retrograde: true}); err != nil {
		t.Fatalf("SpawnCraft: %v", err)
	}
	joined := strings.Join(v.buildNavigationBox(w), "\n")
	value, _, hasBest := extractDepart(t, joined)
	if hasBest {
		t.Errorf("expected ORBIT's depart: row to carry no '(best N°)' suffix; got:\n%s", joined)
	}
	if value <= 90 {
		t.Errorf("depart value %.2f°, want > 90° for a retrograde orbit (folding to [0,90] would read this under 90°)", value)
	}
}

// TestUnfoldedPlaneAngleDegDoesNotFoldPastNinety pins ADR 0050 decision
// 1: depart: is an inclination like incl:, not a coplanarity test like
// Δincl:, so an obtuse angle between the two normals must read past
// 90° (up to 180°), unlike relativePlaneAngleDeg's fold to [0, 90].
// The ADR's own exoplanet pads read 61°..118°, a range
// relativePlaneAngleDeg could never produce.
func TestUnfoldedPlaneAngleDegDoesNotFoldPastNinety(t *testing.T) {
	a := orbital.Vec3{Z: 1}
	// 120 degrees from +Z, in the X-Z plane.
	b := orbital.Vec3{X: math.Sin(120 * math.Pi / 180), Z: math.Cos(120 * math.Pi / 180)}
	deg, ok := unfoldedPlaneAngleDeg(a, b)
	if !ok {
		t.Fatal("unfoldedPlaneAngleDeg: expected ok=true for two non-degenerate normals")
	}
	if deg < 119.99 || deg > 120.01 {
		t.Errorf("unfoldedPlaneAngleDeg = %.2f, want ~120 (relativePlaneAngleDeg would fold this to 60)", deg)
	}
	if folded, ok := relativePlaneAngleDeg(a, b); !ok || folded > 61 || folded < 59 {
		t.Fatalf("setup: relativePlaneAngleDeg = %.2f (ok=%v), want ~60 (confirms the two helpers genuinely differ)", folded, ok)
	}
}

// TestDepartRowNeverWarningColoured pins ADR 0050's Consequences: "depart:
// takes no Warning colour. It states a fact rather than a hazard." Uses
// plainThemeColored (distinguishable ANSI per style, unlike
// chipTestTheme's no-op styles) with the color profile forced to ANSI
// (go test's non-TTY stdout would otherwise make every Render a no-op
// regardless of style, per TestOrbitChipSubSurfacePeriapsisIsWarningColoured's
// own doc comment), on a pad whose depart value (~52°, the high end of
// the Earth swing) sits well past the 30° threshold that DOES colour
// the neighbouring Δincl row: proving the absence is deliberate, not
// an artifact of a low number that would never have triggered Warning
// anyway.
func TestDepartRowNeverWarningColoured(t *testing.T) {
	ambient := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI)
	t.Cleanup(func() { lipgloss.SetColorProfile(ambient) })

	w, c := spawnLandedOnEarthAt28p6(t)
	v := NewOrbitView(plainThemeColored())
	t0 := w.Clock.SimTime
	radius := c.Primary.RadiusMeters()
	place := func(at time.Time) {
		w.Clock.SimTime = at
		dir := render.BodyFixedToWorld(c.Primary, c.LaunchLatDeg, c.LaunchLonDeg, at)
		c.State.R = orbital.Vec3{X: radius * dir.X, Y: radius * dir.Y, Z: radius * dir.Z}
		omegaR := render.BodySpinOmegaWorld(c.Primary)
		omega := orbital.Vec3{X: omegaR.X, Y: omegaR.Y, Z: omegaR.Z}
		c.State.V = omega.Cross(c.State.R)
	}
	// A due-east launch normal precesses about the spin axis as the
	// primary rotates, so the depart value sweeps across a sidereal day
	// (the ADR's own 5.17..52.05 swing); scan a handful of hours rather
	// than assume any one offset lands near the high end.
	best := 0.0
	var bestAt time.Time
	for h := 0; h < 24; h++ {
		at := t0.Add(time.Duration(h) * time.Hour)
		place(at)
		val, _, _ := extractDepart(t, stripANSI(strings.Join(v.buildNavigationBox(w), "\n")))
		if val > best {
			best = val
			bestAt = at
		}
	}
	place(bestAt)

	rows := v.buildNavigationBox(w)
	var departLine string
	for _, l := range rows {
		if strings.Contains(stripANSI(l), "depart:") {
			departLine = l
			break
		}
	}
	if departLine == "" {
		t.Fatalf("expected a depart: row; got:\n%s", strings.Join(rows, "\n"))
	}
	value, _, hasBest := extractDepart(t, stripANSI(departLine))
	if !hasBest {
		t.Fatalf("setup: expected the depart: row to carry a best suffix; got: %q", departLine)
	}
	if value < 30 {
		t.Fatalf("setup: expected a depart value past the 30 degree Warning threshold this test means to probe, got %.2f", value)
	}
	if departLine != stripANSI(departLine) {
		t.Errorf("depart: row carries colour codes despite a value (%.2f°) past the Δincl Warning threshold; ADR says depart: never takes Warning: %q", value, departLine)
	}
}

// TestDepartSwingMatchesADRTable locks in departSwing's closed form
// against every body in the ADR 0050 table (decision 2's own numbers),
// independent of any chip rendering: low/high/best for i=28.6083°
// (every body's due-east pad inclination, latitude-independent of the
// primary) at each body's own eps (spin axis to reference normal).
// eps values are read from the catalog via unfoldedPlaneAngleDeg
// rather than hardcoded, so this test tracks the real bodies rather
// than a second copy of the numbers.
func TestDepartSwingMatchesADRTable(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	const padIncl = sim.DefaultLaunchpadLatitude
	find := func(id string) (bodyEps float64) {
		t.Helper()
		for i := 0; i < len(w.Systems); i++ {
			for _, b := range w.Systems[i].Bodies {
				if b.ID != id {
					continue
				}
				spinAxisR := render.BodyRotationAxisWorld(b)
				spinAxis := orbital.Vec3{X: spinAxisR.X, Y: spinAxisR.Y, Z: spinAxisR.Z}
				ref, ok := departReferenceNormal(b)
				if !ok {
					t.Fatalf("%s: no reference normal", id)
				}
				eps, _ := unfoldedPlaneAngleDeg(spinAxis, ref)
				return eps
			}
		}
		t.Fatalf("body %q not found in any loaded system", id)
		return 0
	}
	cases := []struct {
		id                          string
		wantLow, wantHigh, wantBest float64
	}{
		{"earth", 5.17, 52.05, 5.17},
		{"mars", 4.80, 52.42, 4.80},
		{"mercury", 21.63, 35.59, 21.63},
		{"moon", 23.46, 33.75, 23.46},
		{"glyph", 22.61, 34.61, 22.61},
	}
	for _, c := range cases {
		t.Run(c.id, func(t *testing.T) {
			eps := find(c.id)
			low, high, best := departSwing(padIncl, eps)
			const tol = 0.01
			if math.Abs(low-c.wantLow) > tol {
				t.Errorf("low = %.2f, want %.2f", low, c.wantLow)
			}
			if math.Abs(high-c.wantHigh) > tol {
				t.Errorf("high = %.2f, want %.2f", high, c.wantHigh)
			}
			if math.Abs(best-c.wantBest) > tol {
				t.Errorf("best = %.2f, want %.2f", best, c.wantBest)
			}
		})
	}
}

// TestLandedPadDepartRowReadsPastNinetyForRetrogradeHeading pins review
// r1 F6: the pad's depart: row must stay unfolded over [0, 180]
// (decision 1, unfoldedPlaneAngleDeg), never folded to [0, 90] the way
// Δincl's coplanarity test is. Every existing depart fixture launches
// within a few tens of degrees of due east, so a fold bug at this call
// site left every existing depart test green (the review's own
// sabotage ledger, case "i"). A KSC pad commanded to heading 270°
// (due west, HeadingTrim = +180°) reaches a retrograde orbit and must
// read a depart value past 90°: the reviewer measured `depart: 157.9°
// (best 128.0°)` at this exact heading.
func TestLandedPadDepartRowReadsPastNinetyForRetrogradeHeading(t *testing.T) {
	w, c := spawnLandedOnEarthAt28p6(t)
	c.HeadingTrim = math.Pi // command heading 270° (due west).
	v := NewOrbitView(chipTestTheme())
	joined := strings.Join(v.buildNavigationBox(w), "\n")
	value, _, hasBest := extractDepart(t, joined)
	if !hasBest {
		t.Fatalf("expected the pad's depart: row to carry a '(best N°)' suffix; got:\n%s", joined)
	}
	if value <= 90 {
		t.Errorf("depart value %.2f°, want > 90° for a retrograde (westward) launch heading (folding to [0,90] would read this under 90°)", value)
	}
}

// TestFrozenPadHasNoDepartRowOnKern pins ADR 0050 decision 3: Kern's
// own orbit around Lumen's star has zero inclination and Kern's spin
// axis carries zero AxialTilt (Lumen's faithful zero-obliquity
// mirror), so the reference normal sits exactly on Kern's spin axis
// (eps == 0) and a depart: row would read the pad's fixed incl: value
// at every heading and every hour: a row that can never move. Verified
// independently against the catalog (cmd/verifydepart, ADR progress
// log) before this test: eps=0.0000, closedForm=[28.61..28.61].
func TestFrozenPadHasNoDepartRowOnKern(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	// ADR 0015 / system_binding_test.go's own TestSpawnBindsToViewedSystem:
	// SpawnCraft's ParentBodyID lookup only searches the *viewed* System
	// (w.System(), keyed off w.SystemIdx), not every loaded System. NewWorld
	// starts viewing Sol, so without browsing to Lumen first, FindBody("kern")
	// misses and SpawnCraft silently falls back to the active craft's
	// existing primary (Earth) instead of erroring: caught here because the
	// first draft of this test asserted against Earth's own swing without
	// realizing it (Absent, or just unmatched? It was unmatched).
	for i := 0; i < len(w.Systems); i++ {
		if w.Systems[w.SystemIdx].Name == "Lumen" {
			break
		}
		w.CycleSystem()
	}
	if w.Systems[w.SystemIdx].Name != "Lumen" {
		t.Fatalf("could not browse to Lumen (stuck viewing %q)", w.Systems[w.SystemIdx].Name)
	}
	c, err := w.SpawnCraft(sim.SpawnSpec{
		LoadoutID:       spacecraft.LoadoutKernStackID,
		ParentBodyID:    "kern",
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
	if c.Primary.ID != "kern" {
		t.Fatalf("setup: expected craft primary = kern, got %q", c.Primary.ID)
	}
	v := NewOrbitView(chipTestTheme())
	rows := v.buildNavigationBox(w)
	joined := strings.Join(rows, "\n")
	// ADR 0051 correction C1: "hide the depart: row" becomes "the
	// depart: cell reads," (decision 2: every row always drawn).
	if regexp.MustCompile(`depart:\s+[0-9]`).MatchString(joined) {
		t.Errorf("expected a dash in the depart: cell on a frozen Kern pad (eps==0); got:\n%s", joined)
	}
}
