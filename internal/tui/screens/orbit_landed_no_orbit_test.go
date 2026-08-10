package screens

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/jasonfen/terminal-space-program/internal/render"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// spawnLandedOnMoon spawns a Saturn V landed on the Moon — an airless
// body, so shouldShowLaunchHUD's Atmosphere==nil gate never fires and
// (pre-#375) the ORBIT chip / map ellipse took over instead of the
// LAUNCH chip, reading the co-rotation pseudo-orbit as a real one.
func spawnLandedOnMoon(t *testing.T, lat, lonOffset float64) (*sim.World, *spacecraft.Spacecraft) {
	t.Helper()
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c, err := w.SpawnCraft(sim.SpawnSpec{
		LoadoutID:       spacecraft.LoadoutSaturnVID,
		ParentBodyID:    "moon",
		Launchpad:       true,
		Latitude:        lat,
		LongitudeOffset: lonOffset,
	})
	if err != nil {
		t.Fatalf("SpawnCraft: %v", err)
	}
	if !c.Landed {
		t.Fatal("setup: launchpad spawn should set Landed=true")
	}
	if c.Primary.Atmosphere != nil {
		t.Fatalf("setup: Moon should have no atmosphere; got %+v", c.Primary.Atmosphere)
	}
	if shouldShowLaunchHUD(c) {
		t.Fatal("setup: want the LAUNCH chip's ascent gate OFF on an airless body, so the ORBIT chip owns this case")
	}
	return w, c
}

// TestLandedActiveVesselDrawsNoEllipseOrApsisMarkers (#375, site
// orbit.go:1164): a Landed active vessel's (R, ω×R) state resolves
// through ElementsFromState to a valid-looking ellipse (periapsis a
// few metres from the Moon's centre) — the map must draw no needle
// and no ▲/▼ apsis markers for it, while still keeping the vessel
// glyph on the canvas.
//
// "Nothing is drawn" is vacuous by default; this test was verified to
// actually fail when the craftHasOrbit guard at orbit.go's active-craft
// orbitVisible line was sabotaged back to the pre-fix condition (see
// PR description).
func TestLandedActiveVesselDrawsNoEllipseOrApsisMarkers(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	v.Resize(80, 24)
	w, _ := spawnLandedOnMoon(t, 10, 0)

	v.Render(w, 0, 80, 24)

	if got := v.canvas.CountColor(render.ColorCurrentOrbit); got != 0 {
		t.Errorf("landed active vessel drew %d cell(s) of the current-orbit ellipse color, want 0", got)
	}
	apoColor := render.MarkerColor(render.MarkerApoapsis, render.MarkerNominal, "")
	periColor := render.MarkerColor(render.MarkerPeriapsis, render.MarkerNominal, "")
	if got := v.canvas.CountOverlayColor(apoColor); got != 0 {
		t.Errorf("landed active vessel drew %d apoapsis marker(s), want 0", got)
	}
	if got := v.canvas.CountOverlayColor(periColor); got != 0 {
		t.Errorf("landed active vessel drew %d periapsis marker(s), want 0", got)
	}
}

// TestLandedVesselKeepsGlyphAndInspectRegistration (#375 fix item 2):
// suppressing the ellipse/apsis markers must not take the vessel itself
// off the map — "Keep the glyph, the label, and inspect/click
// registration — the vessel is still there, it just has no track."
//
// The active-vessel test above uses lat 10°/lon 0°, which IsBehindBody
// occludes for the launch-anchored camera (confirmed by direct render:
// no glyph in the output at that lat/lon) — a happy accident for the
// ellipse-only test, but it means nothing exercises this half of the
// fix, and a glyph assertion added there would be vacuous by
// occlusion, not by a real guard. Lat 30°N is verified by direct render
// (every longitude at 30°N, and 60°N, all render the glyph — the
// anchor recenters on the vessel regardless of longitude) to be
// camera-facing, so the glyph assertion below is meaningful, and the
// ellipse/marker assertions repeated at the end of this test are the
// non-vacuous witness at THIS SAME lat/lon (see sabotage results in the
// PR description — reverting the craftHasOrbit guard at these
// coordinates still fails the ellipse/marker checks, and blanking the
// glyph draw still fails the glyph check).
func TestLandedVesselKeepsGlyphAndInspectRegistration(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	v.Resize(80, 24)
	w, c := spawnLandedOnMoon(t, 30, 0) // 30°N — camera-facing, verified above

	out := v.Render(w, 0, 80, 24)

	vesselGlyph := []rune(spacecraft.VesselGlyph)[0]
	if !strings.ContainsRune(out, vesselGlyph) {
		t.Errorf("landed vessel glyph missing from render at a camera-facing lat/lon:\n%s", out)
	}

	// "The label" + "inspect/click registration": addInspectable records
	// both the [j]-cycle stop (with the name its flare chip would show)
	// and the click-resolution entry (drawnOwners) in one call, so a
	// vessel that vanished from either would vanish from both.
	wantRef := InspectRef{Kind: InspectVessel, CraftID: c.ID}
	found := false
	for _, it := range v.inspectables {
		if it.ref == wantRef {
			found = true
			if it.name != c.Name {
				t.Errorf("inspect entry name = %q, want %q", it.name, c.Name)
			}
		}
	}
	if !found {
		t.Errorf("landed vessel not registered in the inspect/[j] cycle at a camera-facing lat/lon")
	}
	if _, ok := v.drawnOwners[wantRef.OwnerKey()]; !ok {
		t.Errorf("landed vessel not registered for click resolution (drawnOwners) at a camera-facing lat/lon")
	}

	// Same guard as TestLandedActiveVesselDrawsNoEllipseOrApsisMarkers,
	// repeated at THIS lat/lon: still no ellipse, still no apsis markers.
	if got := v.canvas.CountColor(render.ColorCurrentOrbit); got != 0 {
		t.Errorf("landed vessel at camera-facing lat/lon drew %d cell(s) of the current-orbit ellipse color, want 0", got)
	}
	apoColor := render.MarkerColor(render.MarkerApoapsis, render.MarkerNominal, "")
	periColor := render.MarkerColor(render.MarkerPeriapsis, render.MarkerNominal, "")
	if got := v.canvas.CountOverlayColor(apoColor); got != 0 {
		t.Errorf("landed vessel at camera-facing lat/lon drew %d apoapsis marker(s), want 0", got)
	}
	if got := v.canvas.CountOverlayColor(periColor); got != 0 {
		t.Errorf("landed vessel at camera-facing lat/lon drew %d periapsis marker(s), want 0", got)
	}
}

// TestLandedOtherVesselDrawsNoEllipse (#375, site orbit.go:1283): a
// second, non-active landed vessel on the same map must also draw no
// track — "every other landed vessel on the map draws its own needle"
// per the issue repro. The active vessel here has a real, visible
// orbit so the test also proves the guard is landed-specific, not a
// blanket "no ellipses" regression.
func TestLandedOtherVesselDrawsNoEllipse(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	v.Resize(80, 24)
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	active := w.ActiveCraft()
	if active == nil {
		t.Fatal("expected an active craft from NewWorld")
	}
	active.Landed = false // active vessel has a real orbit (sanity anchor)

	// Same primary as the active craft (Earth, NewWorld's default LEO
	// seed) so the pseudo-orbit's scale actually lands in the visible
	// map/apoapsis-pixel-threshold range — a landed vessel on a
	// different body/primary wouldn't draw here regardless of the
	// Landed guard, which would make this assertion vacuous.
	other, err := w.SpawnCraft(sim.SpawnSpec{
		LoadoutID:       spacecraft.LoadoutSaturnVID,
		ParentBodyID:    active.Primary.ID,
		Launchpad:       true,
		Latitude:        -20,
		LongitudeOffset: 15,
	})
	if err != nil {
		t.Fatalf("SpawnCraft(other): %v", err)
	}
	if !other.Landed {
		t.Fatal("setup: other vessel should be Landed")
	}
	// A distinctive color unused elsewhere on the canvas, so the ellipse
	// count below can't be confused with dim UI chrome / RCS trails that
	// also default to render.ColorDim.
	const otherColor = lipgloss.Color("#123456")
	other.Color = string(otherColor)
	// SpawnCraft made `other` active; restore the real-orbit craft as
	// active so this exercises the "every OTHER landed vessel" path.
	w.ActiveCraftIdx = 0

	v.Render(w, 0, 80, 24)
	if got := v.canvas.CountColor(otherColor); got != 0 {
		t.Errorf("other landed vessel drew %d cell(s) of its own orbit color, want 0", got)
	}
}

// TestOrbitChipShowsSurfaceFactsWhenLanded (#375, site
// orbit_chip_builders.go:1300 / buildOrbitMetricsChip): the ORBIT chip
// for a Landed vessel on an airless body must not vanish (a chip that
// disappears reads as broken) and must not show Ap/Pe/period computed
// from the co-rotation pseudo-orbit — it shows the facts that ARE true
// on the ground instead.
func TestOrbitChipShowsSurfaceFactsWhenLanded(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	v.Resize(80, 24)
	w, _ := spawnLandedOnMoon(t, 10, 0)

	lines := v.buildOrbitMetricsChip(w)
	if lines == nil {
		t.Fatal("ORBIT chip vanished for a landed vessel, want surface facts instead")
	}
	joined := strings.Join(lines, "\n")
	for _, want := range []string{"body:", "Moon", "landed at:", "altitude:", "co-rotation:"} {
		if !strings.Contains(joined, want) {
			t.Errorf("landed ORBIT chip missing %q:\n%s", want, joined)
		}
	}
	for _, unwanted := range []string{"Ap:", "Pe:", "period:", "t→Ap:", "t→Pe:", "PERIAPSIS BELOW SURFACE"} {
		if strings.Contains(joined, unwanted) {
			t.Errorf("landed ORBIT chip still shows orbital readout %q, want it suppressed:\n%s", unwanted, joined)
		}
	}
}

// TestTargetChipLandedTargetShowsNoApPe (#375, site
// orbit_chip_builders.go:1533): a landed TARGET keeps range / closing /
// |v_rel| (still meaningful relative-state math) but swaps its Ap/Pe/
// inclin. for the landing site, since those numbers came from the same
// co-rotation pseudo-orbit as the ORBIT chip's.
func TestTargetChipLandedTargetShowsNoApPe(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	v.Resize(80, 24)
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	active := w.ActiveCraft()
	if active == nil {
		t.Fatal("expected an active craft from NewWorld")
	}
	active.Landed = false

	targetCraft, err := w.SpawnCraft(sim.SpawnSpec{
		LoadoutID:       spacecraft.LoadoutSaturnVID,
		ParentBodyID:    active.Primary.ID,
		Launchpad:       true,
		Latitude:        5,
		LongitudeOffset: 5,
	})
	if err != nil {
		t.Fatalf("SpawnCraft(target): %v", err)
	}
	if !targetCraft.Landed {
		t.Fatal("setup: target vessel should be Landed")
	}
	w.ActiveCraftIdx = 0 // restore the orbiting craft as active
	w.SetTargetCraft(1)  // target the landed vessel

	lines := v.buildTargetChip(w)
	if lines == nil {
		t.Fatal("TARGET chip returned nil for a landed target")
	}
	joined := strings.Join(lines, "\n")
	for _, unwanted := range []string{"Ap:", "Pe:", "inclin.:"} {
		if strings.Contains(joined, unwanted) {
			t.Errorf("landed target's TARGET chip still shows %q, want it swapped for landing site:\n%s", unwanted, joined)
		}
	}
	for _, want := range []string{"landed at:", "range:", "|v_rel|:", "closing:"} {
		if !strings.Contains(joined, want) {
			t.Errorf("landed target's TARGET chip missing %q:\n%s", want, joined)
		}
	}
}

// TestLandedVesselNeverPrintsNegativeZero (#375 acceptance): the
// original bug's Ap/Pe/altitude sign-flipped between "0.0" and "-0.0"
// every tick because ω×R sits the craft exactly at the pseudo-orbit's
// apoapsis. A single-frame check can pass by luck (the flip is
// per-tick), so this drives 200 physics ticks and checks the ORBIT
// chip on every one of them.
func TestLandedVesselNeverPrintsNegativeZero(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	v.Resize(80, 24)
	w, _ := spawnLandedOnMoon(t, 10, 0)

	for i := 0; i < 200; i++ {
		w.Tick()
		lines := v.buildOrbitMetricsChip(w)
		for _, l := range lines {
			if strings.Contains(l, "-0.0") || strings.Contains(l, "-0 ") {
				t.Fatalf("tick %d: landed ORBIT chip printed a negative zero: %q\nfull chip:\n%s",
					i, l, strings.Join(lines, "\n"))
			}
		}
	}
}

// TestLandedTargetHasNoClosestApproachPrediction (#375 batched-review
// follow-up): craftHasOrbit correctly gates the TARGET chip's Ap/Pe and
// the map's ellipse/apsis markers, but closestApproachRows (the TCA/CA
// chip rows) and drawClosestApproachMarker (the map's ✕ marker) both
// feed the target's raw (R, V) straight into
// planner.NextClosestApproach(Positions) — bypassing craftHasOrbit
// entirely. For a landed target that Kepler-propagates the same
// co-rotation pseudo-orbit the rest of #375 suppresses and reports a
// TCA/CA pair (plus a ✕ on the map) for a phantom trajectory diving
// through the body.
//
// Range/closing/|v_rel| stay meaningful for a landed target (plain
// relative-state math, not propagation) so only the predicted-encounter
// rows and marker are gated — the chip must not blank those either.
//
// Same primary as the active craft (Earth) and the same camera-facing
// lat/lon pattern as TestLandedVesselKeepsGlyphAndInspectRegistration
// (30°N) — a different-primary or occluded target would make the
// marker assertion pass vacuously regardless of the guard, the same
// trap TestLandedOtherVesselDrawsNoEllipse hit on the first pass.
func TestLandedTargetHasNoClosestApproachPrediction(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	v.Resize(80, 24)
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	active := w.ActiveCraft()
	if active == nil {
		t.Fatal("expected an active craft from NewWorld")
	}
	active.Landed = false

	target, err := w.SpawnCraft(sim.SpawnSpec{
		LoadoutID:       spacecraft.LoadoutSaturnVID,
		ParentBodyID:    active.Primary.ID, // same primary as the active craft
		Launchpad:       true,
		Latitude:        30, // camera-facing, per TestLandedVesselKeepsGlyphAndInspectRegistration
		LongitudeOffset: 0,
	})
	if err != nil {
		t.Fatalf("SpawnCraft(target): %v", err)
	}
	if !target.Landed {
		t.Fatal("setup: target vessel should be Landed")
	}
	w.ActiveCraftIdx = 0 // restore the orbiting craft as active
	w.SetTargetCraft(1)  // target the landed vessel

	lines := v.buildTargetChip(w)
	if lines == nil {
		t.Fatal("TARGET chip returned nil for a landed target")
	}
	joined := strings.Join(lines, "\n")
	for _, unwanted := range []string{"TCA:", "CA:"} {
		if strings.Contains(joined, unwanted) {
			t.Errorf("landed target's TARGET chip still shows %q (a propagated closest-approach prediction), want it suppressed:\n%s", unwanted, joined)
		}
	}
	for _, want := range []string{"range:", "|v_rel|:", "closing:"} {
		if !strings.Contains(joined, want) {
			t.Errorf("landed target's TARGET chip lost %q — only the propagated CA/TCA prediction should be gated, not the whole row group:\n%s", want, joined)
		}
	}

	v.Render(w, 0, 80, 24)
	caColor := render.MarkerColor(render.MarkerClosestApproach, render.MarkerNominal, "")
	if got := v.canvas.CountOverlayColor(caColor); got != 0 {
		t.Errorf("landed target drew %d closest-approach ✕ marker(s), want 0 — the marker plots the same propagated pseudo-orbit as the chip rows", got)
	}
}

// TestFormatChipKmSnapsNegativeZero locks the #375 item-6 fix: the km
// formatter shared by the ORBIT/TARGET chips' altitude/Ap/Pe rows must
// never print a "-0.0" for a magnitude that rounds to zero at its
// display precision, even where the underlying number is legitimately
// noise-level rather than exactly zero — nzero is already proven for
// v_vert; formatChipKm is the same treatment for km readouts.
func TestFormatChipKmSnapsNegativeZero(t *testing.T) {
	cases := []struct {
		m    float64
		want string
	}{
		{-3, "0.0 km"},              // noise-level negative -> snapped
		{3, "0.0 km"},               // noise-level positive -> unaffected either way
		{-1_737_393.6, "-1737.4 km"}, // real negative value stays negative
		{1500, "1.5 km"},
	}
	for _, c := range cases {
		if got := formatChipKm(c.m); got != c.want {
			t.Errorf("formatChipKm(%g) = %q, want %q", c.m, got, c.want)
		}
	}
}
