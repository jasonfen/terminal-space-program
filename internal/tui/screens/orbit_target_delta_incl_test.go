package screens

import (
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// orbitingVesselTargetWorld builds a world with the active craft in the
// default equatorial LEO seed orbit and a second craft in a 30°-
// inclined LEO orbit around the same primary, targeted from the
// active craft: a same-primary vessel target with a real, non-coplanar
// orbit (ADR 0050 decision 6).
func orbitingVesselTargetWorld(t *testing.T) *sim.World {
	t.Helper()
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	if _, err := w.SpawnCraft(sim.SpawnSpec{AltitudeM: 600e3, Inclination: 30}); err != nil {
		t.Fatalf("SpawnCraft: %v", err)
	}
	w.ActiveCraftIdx = 0
	w.SetTargetCraft(1)
	if w.Target.Kind != sim.TargetCraft {
		t.Fatalf("setup: expected TargetCraft, got %v", w.Target.Kind)
	}
	return w
}

// TestBuildTargetChipShowsDeltaInclForOrbitingVesselTarget pins ADR
// 0050 decision 6: the full TARGET chip's TargetCraft branch carries no
// Δincl row for any vessel today, landed or orbiting. Add it for an
// orbiting (has-a-real-orbit) target.
func TestBuildTargetChipShowsDeltaInclForOrbitingVesselTarget(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	w := orbitingVesselTargetWorld(t)
	lines := v.buildTargetBox(w)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "Δincl:") {
		t.Fatalf("expected a 'Δincl:' row on the TARGET chip for an orbiting vessel target, got none:\n%s", joined)
	}
}

// TestBuildTargetChipTagsDeltaInclDueEastForLandedVesselTarget pins
// ADR 0050 decision 7: a landed target's plane is r × v of its actual
// co-rotation state, which equals the due-east launch plane at every
// latitude, so the row says so rather than pretending the assumption
// isn't there. The active craft's own orbit is the seed equatorial LEO
// orbit (inclination ~0° in the body-equatorial frame), so the plane
// angle to a due-east launch plane from the KSC pad latitude
// (28.6083°) is exactly that latitude, independent of node alignment:
// this also doubles as a numeric check, not just a presence check.
func TestBuildTargetChipTagsDeltaInclDueEastForLandedVesselTarget(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
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
	w.ActiveCraftIdx = 0 // the seed equatorial LEO craft.
	w.SetTargetCraft(1)  // the landed KSC craft.
	if w.Target.Kind != sim.TargetCraft {
		t.Fatalf("setup: expected TargetCraft, got %v", w.Target.Kind)
	}

	lines := v.buildTargetBox(w)
	joined := strings.Join(lines, "\n")
	// ADR 0051's flattened 10-cell TARGET box has no slot for a separate
	// "landed at:" cell (the retired chip's own row for this branch);
	// Ap:/Pe:/incl: simply dash for a landed target instead. This test
	// is actually about the Δincl due-east tag, unaffected by that.
	diRe := regexp.MustCompile(`Δincl:\s+([0-9]+\.[0-9]+)°\s+\(due east\)`)
	m := diRe.FindStringSubmatch(joined)
	if m == nil {
		t.Fatalf("expected a 'Δincl: N° (due east)' row for the landed vessel target, got:\n%s", joined)
	}
	got, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		t.Fatalf("could not parse Δincl value %q: %v", m[1], err)
	}
	if diff := got - sim.DefaultLaunchpadLatitude; diff > 0.01 || diff < -0.01 {
		t.Errorf("Δincl (due east) = %.4f°, want ~%.4f° (KSC pad latitude, since the active craft's own orbit is equatorial)", got, sim.DefaultLaunchpadLatitude)
	}
}

// TestBuildTargetChipNoDeltaInclAcrossPrimaries pins ADR 0050 decision
// 6's amendment: a vessel target orbiting a DIFFERENT primary than the
// active craft gets no Δincl figure at all, matching the `I` key and
// the node markers (both already refuse via TargetSharesActivePrimary).
// Without this gate the figure mixes the target's own lap with its
// primary's motion round the active primary and reads a fast-looking,
// meaningless wobble.
func TestBuildTargetChipNoDeltaInclAcrossPrimaries(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	// Active craft (Crafts[0]) is the seed Earth LEO orbit; target a
	// second craft in lunar orbit, a different primary.
	if _, err := w.SpawnCraft(sim.SpawnSpec{ParentBodyID: "Moon", AltitudeM: 100e3}); err != nil {
		t.Fatalf("SpawnCraft: %v", err)
	}
	w.ActiveCraftIdx = 0
	w.SetTargetCraft(1)
	if w.Target.Kind != sim.TargetCraft {
		t.Fatalf("setup: expected TargetCraft, got %v", w.Target.Kind)
	}
	if w.TargetSharesActivePrimary() {
		t.Fatalf("setup: expected the lunar-orbit target and the Earth-orbit active craft to NOT share a primary")
	}

	lines := v.buildTargetBox(w)
	joined := strings.Join(lines, "\n")
	// ADR 0051 decision 2: the Δincl: LABEL is always present now (every
	// row always drawn); what must be absent is a real VALUE in its cell.
	if regexp.MustCompile(`Δincl:\s+[0-9]`).MatchString(joined) {
		t.Errorf("expected a dash in the Δincl cell for a vessel target orbiting a different primary, got:\n%s", joined)
	}
}

// TestBuildTargetChipNoDeltaInclForVesselLandedAtPole pins ADR 0050
// decision 8 at the chip layer: a vessel target landed at the shipped
// North Pole preset has a pole-degenerate plane normal (|rT x vT| ~=
// 3e-7, not exactly zero), so the chip must withhold the Δincl row the
// same way PlanVesselPlaneMatch refuses and TargetPlaneNodePositions
// draws no markers there.
func TestBuildTargetChipNoDeltaInclForVesselLandedAtPole(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	if _, err := w.SpawnCraft(sim.SpawnSpec{Launchpad: true, Latitude: 90}); err != nil {
		t.Fatalf("SpawnCraft: %v", err)
	}
	w.ActiveCraftIdx = 0
	w.SetTargetCraft(1)
	if w.Target.Kind != sim.TargetCraft {
		t.Fatalf("setup: expected TargetCraft, got %v", w.Target.Kind)
	}

	lines := v.buildTargetBox(w)
	joined := strings.Join(lines, "\n")
	if regexp.MustCompile(`Δincl:\s+[0-9]`).MatchString(joined) {
		t.Errorf("expected a dash in the Δincl cell for a vessel target landed at the pole, got:\n%s", joined)
	}
}

// (The pad's own Δincl row, once shared by the retired SURFACE/DESCENT
// chips and tested here as TestLaunchChipTagsDeltaInclDueEastForLandedVesselTarget
// / TestLaunchChipShowsDeltaInclAgainstVesselTarget, is retired for
// good under ADR 0051 decision 9: "the pad block's second Δincl: ->
// TARGET's Δincl: cell only". The due-east tagging and the
// same-primary-vessel-target facts those two tests pinned are the exact
// same facts TestBuildTargetChipTagsDeltaInclDueEastForLandedVesselTarget
// and TestBuildTargetChipShowsDeltaInclForOrbitingVesselTarget above
// already prove against the live TARGET box, so nothing here needed a
// migrated twin.)

// TestDeltaInclRowWarningColouredPastThreshold: round 2 review R2-F3.
// deltaInclLabel (the shared helper behind TARGET's Δincl cell, the only
// place it lives now) colours the value Warning past 30 degrees, but
// every existing Δincl test uses chipTestTheme's no-op styles or a value
// that never crosses the threshold, so neutralising that colouring left
// every Δincl test green. Mirrors TestDepartRowNeverWarningColoured's
// idiom (plainThemeColored + the color profile forced to ANSI, since go
// test's non-TTY stdout otherwise makes every Render a no-op regardless
// of style) but asserts the opposite: unlike depart:, which the ADR
// says never takes Warning, Δincl: is meant to warn past 30 degrees.
// KSC-active / Baikonur-latitude-landed-target fixture, whose Δincl
// value (~70.6 degrees) sits well past the threshold.
func TestDeltaInclRowWarningColouredPastThreshold(t *testing.T) {
	ambient := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI)
	t.Cleanup(func() { lipgloss.SetColorProfile(ambient) })

	v := NewOrbitView(plainThemeColored())
	v.Resize(200, 80)
	w, ksc := spawnLandedOnEarthAt28p6(t)
	if _, err := w.SpawnCraft(sim.SpawnSpec{
		LoadoutID:       spacecraft.LoadoutSaturnVID,
		ParentBodyID:    "earth",
		Launchpad:       true,
		Latitude:        45.9645, // Baikonur's latitude.
		LongitudeOffset: 63.3052,
	}); err != nil {
		t.Fatalf("SpawnCraft: %v", err)
	}
	w.ActiveCraftIdx = 1 // back to the KSC pad craft.
	if w.ActiveCraft() != ksc {
		t.Fatalf("setup: expected the active craft to be the KSC pad craft")
	}
	w.SetTargetCraft(2) // the Baikonur-latitude landed target.
	if w.Target.Kind != sim.TargetCraft {
		t.Fatalf("setup: expected TargetCraft, got %v", w.Target.Kind)
	}

	lines := v.buildTargetBox(w)
	var diLine string
	for _, l := range lines {
		if strings.Contains(stripANSI(l), "Δincl:") {
			diLine = l
		}
	}
	if diLine == "" {
		t.Fatalf("expected a Δincl: row; got:\n%s", strings.Join(lines, "\n"))
	}
	diRe := regexp.MustCompile(`Δincl:\s+([0-9]+\.[0-9]+)°`)
	m := diRe.FindStringSubmatch(stripANSI(diLine))
	if m == nil {
		t.Fatalf("could not parse a Δincl value out of %q", diLine)
	}
	value, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		t.Fatalf("could not parse Δincl value %q: %v", m[1], err)
	}
	if value < 30 {
		t.Fatalf("setup: expected a Δincl value past the 30 degree Warning threshold this test means to probe, got %.2f", value)
	}
	if diLine == stripANSI(diLine) {
		t.Errorf("Δincl: row carries no colour codes despite a value (%.2f°) past the Warning threshold: %q", value, diLine)
	}
}
