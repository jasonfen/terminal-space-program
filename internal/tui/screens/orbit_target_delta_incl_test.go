package screens

import (
	"regexp"
	"strconv"
	"strings"
	"testing"

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
	lines := v.buildTargetChip(w)
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

	lines := v.buildTargetChip(w)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "landed at:") {
		t.Fatalf("setup: expected a 'landed at:' row for the landed target:\n%s", joined)
	}
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

	lines := v.buildTargetChip(w)
	joined := strings.Join(lines, "\n")
	if strings.Contains(joined, "Δincl:") {
		t.Errorf("expected no Δincl row for a vessel target orbiting a different primary, got:\n%s", joined)
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

	lines := v.buildTargetChip(w)
	joined := strings.Join(lines, "\n")
	if strings.Contains(joined, "Δincl:") {
		t.Errorf("expected no Δincl row for a vessel target landed at the pole, got:\n%s", joined)
	}
}

// TestLaunchChipShowsDeltaInclAgainstVesselTarget pins ADR 0050
// decision 6: today Δincl: on the pad is gated on sim.TargetBody only,
// so targeting a vessel prints nothing even though the vessel has a
// perfectly good fixed plane to match. Matching one from the pad is
// the canonical launch-window problem PlanVesselPlaneMatch (the `I`
// key) already solves. Ungating landedInclHeadingRows for
// TargetCraft/TargetGhost is the fix.
func TestLaunchChipShowsDeltaInclAgainstVesselTarget(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	v.Resize(200, 80)
	w, _ := spawnLandedOnEarthAt28p6(t)
	// Crafts[0] is the seed LEO craft NewWorld() spawns before the pad
	// craft above; targeting it gives a same-primary vessel with a real
	// orbit, no launch assumed.
	w.SetTargetCraft(0)
	if w.Target.Kind != sim.TargetCraft {
		t.Fatalf("setup: expected TargetCraft, got %v", w.Target.Kind)
	}

	lines := v.buildLaunchChip(w)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "Δincl:") {
		t.Fatalf("expected a 'Δincl:' row on the pad chip when targeting a same-primary vessel, got none:\n%s", joined)
	}
}
