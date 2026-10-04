package screens

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/render"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// padWindowFixture: KSC pad (active, Landed) with a 51.6 deg, 400 km
// station as the vessel target.
func padWindowFixture(t *testing.T) (*sim.World, *spacecraft.Spacecraft, *OrbitView) {
	t.Helper()
	w, pad := spawnLandedOnEarthAt28p6(t)
	padIdx := w.ActiveCraftIdx
	if _, err := w.SpawnCraft(sim.SpawnSpec{AltitudeM: 400e3, Inclination: 51.6}); err != nil {
		t.Fatalf("station spawn: %v", err)
	}
	w.ActiveCraftIdx = padIdx
	w.SetTargetCraft(len(w.Crafts) - 1)
	v := NewOrbitView(launchThemeForTest())
	v.Resize(DesignWidth, DesignHeight)
	return w, pad, v
}

func padSetTime(w *sim.World, c *spacecraft.Spacecraft, at time.Time) {
	w.Clock.SimTime = at
	lat, lon := c.SurfaceLatLon()
	d := render.BodyFixedToWorld(c.Primary, lat, lon, at)
	r := c.Primary.RadiusMeters()
	c.State.R = orbital.Vec3{X: r * d.X, Y: r * d.Y, Z: r * d.Z}
	om := render.BodySpinOmegaWorld(c.Primary)
	c.State.V = orbital.Vec3{X: om.X, Y: om.Y, Z: om.Z}.Cross(c.State.R)
}

func planRowOf(v *OrbitView, w *sim.World) string {
	lines := v.buildNavigationBox(w)
	return lines[len(lines)-1]
}

var padWindowRe = regexp.MustCompile(`plan:\s+window T-(\d+h\d+m|\d+d\d+h|\d+m\d+s) at (\d{3})°, lead [+-]\d+°$`)

// The plan: row on the pad names the window, its heading to 1 degree
// and the lead at the pass; trimming the heading moves nothing in it.
func TestNavigationPlanRowShowsPadWindow(t *testing.T) {
	w, pad, v := padWindowFixture(t)
	row := planRowOf(v, w)
	m := padWindowRe.FindStringSubmatch(row)
	if m == nil {
		t.Fatalf("plan row %q is not `window T-... at NNN°, lead +N°`", row)
	}
	if m[2] != "045" && m[2] != "135" {
		t.Errorf("heading %s, want 045 or 135 from KSC to 51.6 deg", m[2])
	}
	pad.HeadingTrim += 20 * 3.141592653589793 / 180
	if got := planRowOf(v, w); got != row {
		t.Errorf("trim moved the window row:\n%q\n%q", row, got)
	}
	// Whole frame too (the seam the player sees).
	if out := v.Render(w, 0, DesignWidth, DesignHeight); !strings.Contains(out, "window T-") {
		t.Errorf("rendered 140x40 frame has no window reading")
	}
}

// Past the pass the row rolls to the other pass (mirror heading).
func TestNavigationPlanRowRollsToOtherPass(t *testing.T) {
	w, pad, v := padWindowFixture(t)
	first := padWindowRe.FindStringSubmatch(planRowOf(v, w))
	lw, _ := w.LaunchWindow()
	padSetTime(w, pad, lw.PassAt.Add(time.Minute))
	second := padWindowRe.FindStringSubmatch(planRowOf(v, w))
	if first == nil || second == nil {
		t.Fatalf("rows did not parse: %v %v", first, second)
	}
	if first[2] == second[2] {
		t.Errorf("heading did not change across the pass: %s", first[2])
	}
}

// No pass (KSC to the Moon): a best-angle phrase, never "none".
func TestNavigationPlanRowNoWindowReadsBest(t *testing.T) {
	w, _ := spawnLandedOnEarthAt28p6(t)
	for i, b := range w.System().Bodies {
		if b.ID == "moon" {
			w.SetTargetBody(i)
		}
	}
	v := NewOrbitView(launchThemeForTest())
	v.Resize(DesignWidth, DesignHeight)
	row := planRowOf(v, w)
	if !regexp.MustCompile(`plan:\s+window best 9\.\d\d° T-\d`).MatchString(row) {
		t.Errorf("plan row %q, want `window best 9.xx° T-...`", row)
	}
	if strings.Contains(row, "none") {
		t.Errorf("plan row says none: %q", row)
	}
}

// No target: plan: keeps its permanent dash on the pad.
func TestNavigationPlanRowPadWithoutTargetIsDash(t *testing.T) {
	w, _ := spawnLandedOnEarthAt28p6(t)
	v := NewOrbitView(launchThemeForTest())
	v.Resize(DesignWidth, DesignHeight)
	if row := planRowOf(v, w); !regexp.MustCompile(`plan:\s+—$`).MatchString(row) {
		t.Errorf("plan row %q, want a dash", row)
	}
}

// TARGET's lead: reads live on the pad (rule C lifted for lead only);
// close:/rel:/TCA stay dashed.
func TestTargetLeadLiveOnPad(t *testing.T) {
	w, _, v := padWindowFixture(t)
	box := strings.Join(v.buildTargetBox(w), "\n")
	if !regexp.MustCompile(`lead:\s+[+-]\d+° \((ahead|behind)\)`).MatchString(box) {
		t.Errorf("lead: not live on the pad:\n%s", box)
	}
	if !regexp.MustCompile(`close:\s+—`).MatchString(box) || !regexp.MustCompile(`TCA:\s+—`).MatchString(box) {
		t.Errorf("close:/TCA: must stay dashed on the pad:\n%s", box)
	}
	w.ActiveCraft().Crashed = true
	box = strings.Join(v.buildTargetBox(w), "\n")
	if !regexp.MustCompile(`lead:\s+—`).MatchString(box) {
		t.Errorf("a crashed vessel's lead: must dash:\n%s", box)
	}
}
