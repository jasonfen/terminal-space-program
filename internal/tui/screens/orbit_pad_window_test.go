package screens

import (
	"github.com/charmbracelet/lipgloss"
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

func markerCounts(v *OrbitView) (an, dn, ca int) {
	an = v.canvas.CountOverlayColor(render.MarkerColor(render.MarkerAscendingNode, render.MarkerNominal, ""))
	dn = v.canvas.CountOverlayColor(render.MarkerColor(render.MarkerDescendingNode, render.MarkerNominal, ""))
	ca = v.canvas.CountOverlayColor(render.MarkerColor(render.MarkerClosestApproach, render.MarkerNominal, ""))
	return
}

// TestTargetMarkersAbsentWhileLanded (#460, G6 Q5): on the pad with a
// vessel target the map used to plant ◇ ◆ and the ✕ pair on the ground
// from the co-rotation pseudo-orbit. Nothing target-shaped draws.
func TestTargetMarkersAbsentWhileLanded(t *testing.T) {
	w, pad, v := padWindowFixture(t)
	v.Resize(200, 60)
	v.Render(w, 0, 200, 60)
	if an, dn, ca := markerCounts(v); an+dn+ca != 0 {
		t.Errorf("Landed pad drew target markers: AN=%d DN=%d CA=%d", an, dn, ca)
	}
	// Positive control: the instrument returns a positive once airborne
	// in a real orbit (same world, pad vessel lifted off into orbit).
	pad.Landed = false
	pad.State.R, pad.State.V = orbital.Vec3{X: 6.771e6}, orbital.Vec3{Y: 7670}
	v.Render(w, 0, 200, 60)
	if an, dn, ca := markerCounts(v); an+dn+ca == 0 {
		t.Error("control failed: no target markers even in orbit, the counter cannot see them")
	}
}

// TestTargetPlaneNodesRenderForBodyTarget (#460, G6 Q5): a body target
// gets ◇/◆ in orbit like a vessel target does.
func TestTargetPlaneNodesRenderForBodyTarget(t *testing.T) {
	v := NewOrbitView(Theme{HUDBox: lipgloss.NewStyle()})
	v.Resize(200, 60)
	w := inclinedCircularEarthOrbitCraft(t, 45, 500e3)
	for i, b := range w.System().Bodies {
		if b.ID == "moon" {
			w.SetTargetBody(i)
		}
	}
	v.Render(w, 0, 200, 60)
	if an, dn, _ := markerCounts(v); an == 0 || dn == 0 {
		t.Errorf("body target in orbit: want both node markers, got AN=%d DN=%d", an, dn)
	}
}

// #548 review LOW 86 (Jason 2026-10-04, "dash stays"): lead: is a
// vessel/ghost reading. A body target on the pad keeps lead: dashed; the
// plan: row's window and heading time a launch toward a body.
func TestTargetLeadDashedForBodyTargetOnPad(t *testing.T) {
	w, _ := spawnLandedOnEarthAt28p6(t)
	for i, b := range w.System().Bodies {
		if b.ID == "moon" {
			w.SetTargetBody(i)
		}
	}
	v := NewOrbitView(launchThemeForTest())
	v.Resize(DesignWidth, DesignHeight)
	box := strings.Join(v.buildTargetBox(w), "\n")
	if !regexp.MustCompile(`lead:\s+—`).MatchString(box) {
		t.Errorf("a body target's lead: must stay dashed on the pad:\n%s", box)
	}
}

// #548 review LOW 115 (Jason 2026-10-04, "say it as a lean"): GUIDANCE's
// trim: names the way ←/→ lean the nose, not a signed pitch, because an
// east lean lowers an eastbound nose (navball pitch -15° beside trim +15°).
func TestGuidanceTrimReadsAsLean(t *testing.T) {
	w, pad := spawnLandedOnEarthAt28p6(t)
	v := NewOrbitView(launchThemeForTest())
	v.Resize(DesignWidth, DesignHeight)
	for _, c := range []struct {
		deg  float64
		want string
	}{{15, `trim:\s+15° E`}, {-5, `trim:\s+5° W`}, {0, `trim:\s+0°`}} {
		pad.PitchTrim = c.deg * 3.141592653589793 / 180
		box := strings.Join(v.buildGuidanceBox(w), "\n")
		if !regexp.MustCompile(c.want).MatchString(box) {
			t.Errorf("trim %+.0f: GUIDANCE lacks %q:\n%s", c.deg, c.want, box)
		}
		if regexp.MustCompile(`trim:\s+[+-]\d`).MatchString(box) {
			t.Errorf("trim %+.0f: GUIDANCE still prints a signed trim:\n%s", c.deg, box)
		}
	}
}
