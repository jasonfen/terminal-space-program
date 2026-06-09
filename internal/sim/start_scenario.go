package sim

import (
	"fmt"
	"strings"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// StartScenario is a resolved request to configure a fresh world's
// starting craft from outside the TUI — built by the command-line flags
// in cmd/terminal-space-program and applied via ApplyStartScenario. It
// only ever shapes a fresh NewWorld (startup never auto-loads a save), so
// there's no persisted-state interaction. v0.17.
type StartScenario struct {
	// SystemName selects the star system by System.Name (case-insensitive).
	// Empty → Sol.
	SystemName string
	// BodyID is the parent body to spawn at (ID or English name, resolved
	// via System.FindBody). Empty → the system's home planet (Earth in Sol,
	// else the first planet).
	BodyID string
	// Loadout is the craft loadout ID (e.g. "Saturn-V"). Empty → S-IVB-1.
	Loadout string

	// Surface selects a launchpad spawn (LatDeg/LonDeg) over an orbital one
	// (AltitudeM/InclDeg/Retrograde).
	Surface bool

	// Orbital placement.
	AltitudeM  float64
	InclDeg    float64
	Retrograde bool

	// Surface placement (degrees north / degrees east of pseudo-Greenwich).
	LatDeg float64
	LonDeg float64
}

// ApplyStartScenario reshapes the world's starting craft per s, replacing
// the default LEO seed. It resolves the system + body, clears the default
// slate, spawns a single craft from the scenario via SpawnCraft (so
// launchpad co-rotation and ADR 0015 system binding come for free), and
// leaves that craft active with the camera following it. Returns a
// descriptive error (listing valid values) on an unknown system / body /
// loadout, leaving the world untouched.
func (w *World) ApplyStartScenario(s StartScenario) error {
	// 1. Resolve the system (default Sol / index 0).
	sysIdx := 0
	if s.SystemName != "" {
		idx := -1
		for i := range w.Systems {
			if strings.EqualFold(w.Systems[i].Name, s.SystemName) {
				idx = i
				break
			}
		}
		if idx < 0 {
			return fmt.Errorf("unknown system %q (have: %s)", s.SystemName, strings.Join(SystemNames(w.Systems), ", "))
		}
		sysIdx = idx
	}

	// 2. Resolve the body within that system before mutating any state.
	sys := w.Systems[sysIdx]
	bodyID := s.BodyID
	if bodyID == "" {
		bodyID = defaultStartBodyID(sys)
	}
	b := sys.FindBody(bodyID)
	if b == nil {
		return fmt.Errorf("unknown body %q in system %q (have: %s)", s.BodyID, sys.Name, strings.Join(bodyNames(sys), ", "))
	}

	// 3. Validate the loadout up front (SpawnCraft would silently fall back
	//    to S-IVB-1 on an unknown ID — surface it as an error instead).
	if s.Loadout != "" && !validLoadoutID(s.Loadout) {
		return fmt.Errorf("unknown loadout %q (have: %s)", s.Loadout, strings.Join(spacecraft.LoadoutOrder, ", "))
	}

	// 4. Browse to the target system so SpawnCraft binds the craft there
	//    (a craft binds to the viewed System at spawn time, ADR 0015), then
	//    replace the default slate.
	w.SystemIdx = sysIdx
	w.Calculator = orbital.ForSystem(w.System())
	w.Crafts = nil
	w.ActiveCraftIdx = -1

	spec := SpawnSpec{
		LoadoutID:    s.Loadout,
		ParentBodyID: b.ID,
	}
	if s.Surface {
		spec.Launchpad = true
		spec.Latitude = s.LatDeg
		spec.LongitudeOffset = s.LonDeg
	} else {
		spec.AltitudeM = s.AltitudeM
		spec.Inclination = s.InclDeg
		spec.Retrograde = s.Retrograde
	}
	if _, err := w.SpawnCraft(spec); err != nil {
		return err
	}
	// SpawnCraft already activated the craft, snapped the camera to its
	// system, and set Focus=FocusCraft.
	return nil
}

// SystemNames lists the loaded systems' display names, for discovery and
// error messages. v0.17.
func SystemNames(systems []bodies.System) []string {
	names := make([]string, len(systems))
	for i, s := range systems {
		names[i] = s.Name
	}
	return names
}

// bodyNames lists a system's body IDs (the canonical CLI tokens).
func bodyNames(sys bodies.System) []string {
	ids := make([]string, len(sys.Bodies))
	for i, b := range sys.Bodies {
		ids[i] = b.ID
	}
	return ids
}

// defaultStartBodyID picks the body to spawn at when none is given: Earth
// when the system has it (keeps the familiar Sol default), else the first
// planet (top-level, non-star body).
func defaultStartBodyID(sys bodies.System) string {
	if b := sys.FindBody("earth"); b != nil {
		return b.ID
	}
	for i := 1; i < len(sys.Bodies); i++ {
		if sys.Bodies[i].ParentID == "" {
			return sys.Bodies[i].ID
		}
	}
	if len(sys.Bodies) > 0 {
		return sys.Bodies[0].ID
	}
	return ""
}

// validLoadoutID reports whether id is a known catalog loadout.
func validLoadoutID(id string) bool {
	for _, l := range spacecraft.LoadoutOrder {
		if l == id {
			return true
		}
	}
	return false
}
