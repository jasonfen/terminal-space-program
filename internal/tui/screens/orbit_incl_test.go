package screens

import (
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

	diRe := regexp.MustCompile(`Δi:\s+([0-9]+\.[0-9]+)°`)
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
		if !strings.Contains(out, "Δi:") {
			t.Fatalf("h=%d: Δi row missing from HUD output:\n%s", hours, out)
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

// TestLandedInclinLabelRelabeledToLaunchLat — when a craft is Landed
// the LAUNCH HUD swaps the "incl.:" row for "launch lat: 28.6°
// (locked)". The value isn't wrong (it equals the latitude); the
// rename makes that explicit and adds the lock tag so the player
// doesn't expect the number to change while pad-warping. v0.11.4+.
func TestLandedInclinLabelRelabeledToLaunchLat(t *testing.T) {
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
	out := v.Render(w, 0, 200, 80)
	if !strings.Contains(out, "launch lat:") {
		t.Errorf("expected Landed HUD to show 'launch lat:' row; got:\n%s", out)
	}
	if !strings.Contains(out, "(locked)") {
		t.Errorf("expected Landed HUD to tag launch-lat row with '(locked)'; got:\n%s", out)
	}
	if strings.Contains(out, "incl.:") {
		t.Errorf("expected Landed HUD to drop the 'incl.:' label; got:\n%s", out)
	}
}
