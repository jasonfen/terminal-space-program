package screens

import (
	"math"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// Basic "render path doesn't panic and produces non-empty output" smoke test.
// Covers the critical integration that real tests (TTY-only) can't exercise:
// that Canvas.String()/Project()/HUD lipgloss panels compose into a real frame.
func TestOrbitViewRendersAllSystems(t *testing.T) {
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
	v.Resize(120, 40)

	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	for i := 0; i < len(w.Systems); i++ {
		out := v.Render(w, 0, 120, 40)
		if len(out) == 0 {
			t.Errorf("system %d (%s): empty render", i, w.System().Name)
		}
		if !strings.Contains(out, w.System().Name) {
			t.Errorf("system %d: expected system name %q in render", i, w.System().Name)
		}
		w.CycleSystem()
	}
}

// TestOrbitHUDRendersVesselAndPropellantSideBySide: at sufficiently
// wide terminals the VESSEL and PROPELLANT block headers share a row
// so the right-hand HUD doesn't get tall enough to push the layout
// off-screen. Below the threshold (v0.10.3+: half-column < 24 cols)
// the blocks fall back to stacked rendering. v0.7.5+ height-saving
// change; threshold bumped in v0.10.3+ to avoid content wrap.
func TestOrbitHUDRendersVesselAndPropellantSideBySide(t *testing.T) {
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
	v.Resize(240, 40)
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	out := v.Render(w, 0, 240, 40)
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "VESSEL") && strings.Contains(line, "PROPELLANT") {
			return
		}
	}
	t.Errorf("expected VESSEL and PROPELLANT on the same row at width 240; got render:\n%s", out)
}

// TestHudPhaseCollapsesVesselDuringAscent: in ascent phase
// (shouldShowLaunchHUD is true) the VESSEL block drops its
// altitude/apoapsis/periapsis/inclin. rows because the LAUNCH block
// already renders them with trend tags and a progress row, and the
// ATTITUDE block drops `hold` because LAUNCH's `sas` is the same
// value. The orbit-shape rows return once the craft circularises
// above the mission floor. v0.10.3+.
func TestHudPhaseCollapsesVesselDuringAscent(t *testing.T) {
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
	// NewWorld spawns in LEO; force a sub-orbital ascent arc
	// (periapsis below the 200 km mission floor) so
	// shouldShowLaunchHUD fires.
	c := w.ActiveCraft()
	c.Landed = false
	mu := c.Primary.GravitationalParameter()
	primaryR := c.Primary.RadiusMeters()
	rApo := primaryR + 250e3
	rPeri := primaryR - 100e3
	a := (rPeri + rApo) / 2
	vAtPeri := math.Sqrt(mu * (2/rPeri - 1/a))
	c.State.R.X, c.State.R.Y, c.State.R.Z = rPeri, 0, 0
	c.State.V.X, c.State.V.Y, c.State.V.Z = 0, vAtPeri, 0

	out := v.Render(w, 0, 200, 60)
	// VESSEL header still renders, but its rows are gated.
	if !strings.Contains(out, "VESSEL") {
		t.Fatal("expected VESSEL header to render during ascent")
	}
	// "altitude:" still appears (LAUNCH block uses the same prefix),
	// so check VESSEL's distinctive rows are gone instead.
	if strings.Contains(out, "  apoapsis:") || strings.Contains(out, "  periapsis:") {
		t.Errorf("expected VESSEL apoapsis/periapsis rows to be hidden during ascent; LAUNCH already shows ap/pe.\nrender:\n%s", out)
	}
	if strings.Contains(out, "  hold:") {
		t.Errorf("expected ATTITUDE `hold` row to be hidden during ascent; LAUNCH `sas` is the same value.\nrender:\n%s", out)
	}
	// LAUNCH's own rows must still be present.
	if !strings.Contains(out, "  ap:") || !strings.Contains(out, "  sas:") {
		t.Errorf("expected LAUNCH ap and sas rows during ascent.\nrender:\n%s", out)
	}

	// Circularise: drop the craft into a 300 km circular orbit, well
	// above the 200 km mission floor → flight phase.
	rCirc := primaryR + 300e3
	vCirc := math.Sqrt(mu / rCirc)
	c.State.R.X, c.State.R.Y, c.State.R.Z = rCirc, 0, 0
	c.State.V.X, c.State.V.Y, c.State.V.Z = 0, vCirc, 0

	out = v.Render(w, 0, 200, 60)
	if !strings.Contains(out, "  apoapsis:") || !strings.Contains(out, "  periapsis:") {
		t.Errorf("expected VESSEL apoapsis/periapsis rows to return once in stable orbit.\nrender:\n%s", out)
	}
	if !strings.Contains(out, "  hold:") {
		t.Errorf("expected ATTITUDE `hold` row to return in flight phase.\nrender:\n%s", out)
	}
	if strings.Contains(out, "LAUNCH") {
		t.Errorf("expected LAUNCH block to vanish once periapsis clears the mission floor.\nrender:\n%s", out)
	}
}

// TestTitleBarShowsClockAndWarp: v0.10.3+ moved the CLOCK block from
// the HUD into the title bar. The title row must show T+date and the
// current warp rate; the HUD must no longer carry a `CLOCK` header.
func TestTitleBarShowsClockAndWarp(t *testing.T) {
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
	v.Resize(200, 40)
	out := v.Render(w, 0, 200, 40)
	titleRow := strings.Split(out, "\n")[0]
	if !strings.Contains(titleRow, "T+") {
		t.Errorf("expected T+date on title row; got %q", titleRow)
	}
	if !strings.Contains(titleRow, "warp ") {
		t.Errorf("expected `warp Nx` on title row; got %q", titleRow)
	}
	if strings.Contains(out, "CLOCK") {
		t.Errorf("expected no CLOCK header in HUD after move to title bar; render:\n%s", out)
	}
}

// TestFocusLabelOverlaidOnCanvas: v0.10.3+ moved the FOCUS block from
// the HUD into the canvas's top-left corner via SetCellLabel. The
// rendered output must contain `focus: <name>` and no longer a FOCUS
// section header in the HUD.
func TestFocusLabelOverlaidOnCanvas(t *testing.T) {
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
	v.Resize(160, 40)
	out := v.Render(w, 0, 160, 40)
	if !strings.Contains(out, "focus: ") {
		t.Errorf("expected `focus:` overlay on canvas; render:\n%s", out)
	}
	// Ensure FOCUS HUD header is gone (the old "FOCUS" was an exact
	// section header line; check the trailing newline pattern that
	// distinguishes it from any other use of the word).
	if strings.Contains(out, "\nFOCUS\n") {
		t.Errorf("expected FOCUS HUD section to be gone after move to canvas; render:\n%s", out)
	}
}

// TestOrbitTitleBarButtonHits: after rendering, the title-bar
// hit-test ranges line up with the right-aligned [Menu] /
// [Missions] buttons. Pre-render hits return false (col ranges are
// zero); a wide-terminal render places both buttons at the right
// edge so HitMenuButton / HitMissionsButton resolve there.
func TestOrbitTitleBarButtonHits(t *testing.T) {
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
	v.Resize(180, 40)
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	_ = v.Render(w, 0, 180, 40)

	// Buttons live on row 0; the right-aligned group ends at the
	// rightmost column. Hit somewhere inside each range.
	if !v.HitMenuButton((v.menuColStart+v.menuColEnd)/2, 0) {
		t.Errorf("expected center of [Menu] range to register a hit")
	}
	if v.HitMenuButton(0, 0) {
		t.Errorf("expected col 0 (left-aligned title text) to miss [Menu]")
	}
	if !v.HitMissionsButton((v.missionsColStart+v.missionsColEnd)/2, 0) {
		t.Errorf("expected center of [Missions] range to register a hit")
	}
	if v.HitMissionsButton((v.menuColStart+v.menuColEnd)/2, 0) {
		t.Errorf("expected [Menu]'s range to miss [Missions]")
	}
	// Row 1 should miss both — buttons are row-0 only.
	if v.HitMenuButton((v.menuColStart+v.menuColEnd)/2, 1) {
		t.Errorf("expected row 1 to miss [Menu]")
	}
}

// TestBodyPixelRadiusMonotonic: perceived-size bucketing is monotonic
// in physical radius. Tier 1 (small) < tier 2 (terrestrial) < tier 4
// (gas giant) < tier 6 (star). System-primary flag promotes to star
// even for small primaries (e.g. a dwarf star that would otherwise
// bucket lower).
func TestBodyPixelRadiusMonotonic(t *testing.T) {
	cases := []struct {
		name   string
		radius float64
		want   int
	}{
		{"tiny moon 500 km", 5e5, 1},
		{"terrestrial Earth 6378 km", 6.378e6, 2},
		{"gas giant Jupiter 69911 km", 6.9911e7, 4},
		{"star Sun 696000 km", 6.96e8, 6},
	}
	prev := 0
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b := bodies.CelestialBody{MeanRadius: c.radius / 1000} // Radius field is in km
			got := BodyPixelRadius(b, false, 0, 0)                 // scale=0 → tier path; cap=0 → default
			if got != c.want {
				t.Errorf("got pxRadius=%d, want %d (radius %.0f km)",
					got, c.want, c.radius/1000)
			}
			if got < prev {
				t.Errorf("non-monotonic: %s got %d after previous %d",
					c.name, got, prev)
			}
			prev = got
		})
	}
}

// TestBodyPixelRadiusAdaptive: when scale × radius produces ≥ 4 px,
// the function switches to true-size rendering (so a periapsis marker
// inside the rendered Earth disk reads as a surface collision instead
// of being hidden by tier bucketing).
func TestBodyPixelRadiusAdaptive(t *testing.T) {
	earth := bodies.CelestialBody{MeanRadius: 6378} // km

	// Sol-wide zoom: 6378 km × ~1e-12 px/m → way below threshold,
	// stays at terrestrial tier (2 px).
	if got := BodyPixelRadius(earth, false, 1e-12, 0); got != 2 {
		t.Errorf("system zoom: got %d px, want 2 (tier)", got)
	}
	// FocusCraft-style zoom: scale ~2e-6 px/m → 6.378e6 m × 2e-6 ≈
	// 13 px, well past the 4 px threshold. Should render true.
	if got := BodyPixelRadius(earth, false, 2e-6, 0); got < 8 {
		t.Errorf("close zoom: got %d px, want true-size ≥ 8", got)
	}
	// Extreme zoom-in with default cap (maxPx=0 → 512).
	if got := BodyPixelRadius(earth, false, 1, 0); got != 512 {
		t.Errorf("absurd zoom default cap: got %d px, want 512", got)
	}
	// Render-side cap (passed in by callers) overrides the default.
	// v0.8.4: render call sites thread canvas reach so the body
	// disk grows to the canvas edge — fixes the visual lie where an
	// altitude-0 craft floated outside a clamped-small disk.
	if got := BodyPixelRadius(earth, false, 1, 1024); got != 1024 {
		t.Errorf("absurd zoom canvas cap: got %d px, want 1024", got)
	}
}

// TestBodyPixelRadiusPrimaryFlag: even a sub-star-sized body rendered
// as system primary gets the star tier so the rendering distinguishes
// it from planets.
func TestBodyPixelRadiusPrimaryFlag(t *testing.T) {
	small := bodies.CelestialBody{MeanRadius: 1000} // 1000 km = terrestrial
	nonPrim := BodyPixelRadius(small, false, 0, 0)
	prim := BodyPixelRadius(small, true, 0, 0)
	if prim <= nonPrim {
		t.Errorf("primary flag should promote size: non-primary=%d primary=%d",
			nonPrim, prim)
	}
}

func TestOrbitViewZoom(t *testing.T) {
	v := NewOrbitView(Theme{HUDBox: lipgloss.NewStyle()})
	v.Resize(80, 24)
	w, _ := sim.NewWorld()
	v.Render(w, 0, 80, 24) // triggers autoFit
	before := v.canvas.Scale()
	v.ZoomIn()
	if v.canvas.Scale() <= before {
		t.Errorf("ZoomIn did not increase scale (before=%.3e after=%.3e)",
			before, v.canvas.Scale())
	}
}

// TestOrbitRendersNavballPanel: the framed KSP-style navball panel
// composites into the frame for a fresh LEO craft — no "NAVBALL"
// label, the RCS toggle, the prograde marker glyph, the target-
// minus glyph from the left column, and recorded hit boxes.
// v0.9.6-polish moved the navball into a bottom-right canvas panel.
func TestOrbitRendersNavballPanel(t *testing.T) {
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
	v.Resize(160, 48)
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	out := v.Render(w, 0, 160, 48)
	if strings.Contains(out, "NAVBALL") {
		t.Errorf("NAVBALL label should be gone from render output")
	}
	if !strings.Contains(out, "RCS") {
		t.Errorf("expected RCS toggle in navball panel")
	}
	if !strings.ContainsRune(out, '⊕') {
		t.Errorf("expected prograde glyph ⊕ in navball panel")
	}
	if !strings.ContainsRune(out, '◌') {
		t.Errorf("expected anti-target glyph ◌ in the left SAS column")
	}
	if len(v.navballControls) == 0 {
		t.Errorf("expected navball control hit boxes to be recorded")
	}
}
