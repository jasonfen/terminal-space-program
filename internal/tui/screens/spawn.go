package screens

import (
	"fmt"
	"strings"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// SpawnCraft is the modal form opened by `n` on the orbit screen.
// v0.8.2+: four fields — craft type, parent body, altitude, and
// direction (prograde / retrograde). v0.8.3+ added a POSITION
// toggle (orbit / alongside). v0.9.2+ extended POSITION with a
// third option (launchpad) and the ALTITUDE field doubles as a
// LATITUDE picker when launchpad is selected. Tab cycles field
// focus; ←/→ edit the focused field; Enter spawns; Esc cancels.
type SpawnCraft struct {
	theme    Theme
	fieldIdx int // 0=loadout, 1=position, 2=parent, 3=altitude/latitude, 4=direction

	loadoutIdx   int
	posMode      spawnPosMode // v0.9.2+: tri-state — orbit / alongside / launchpad
	parentBodies []bodies.CelestialBody // populated by Reset
	parentIdx    int
	altIdx       int
	latIdx       int // v0.9.2+: latitude preset cursor when posMode=launchpad
	retrograde   bool
}

// spawnPosMode enumerates the v0.8.3 / v0.9.2 spawn-position modes.
type spawnPosMode int

const (
	posOrbit     spawnPosMode = iota // pre-v0.8.3 default — circular orbit
	posAlongside                     // v0.8.3+ — within docking gate of active craft
	posLaunchpad                     // v0.9.2+ — surface co-rotating, altitude 0
)

// altitudePresets are the cycle values for the altitude field —
// km above the parent's mean radius. Hand-picked across orders of
// magnitude so the cycle covers LEO-style parking orbits, GEO-ish
// transfer alts, and high-Earth / interplanetary capture orbits.
var altitudePresets = []int{200, 500, 1000, 2000, 5000, 10000, 35786}

// latitudePresets are the cycle values for the launchpad latitude
// field (degrees north). Picked from real-world launch sites so the
// player can sanity-check the equatorial-spin boost: 0° (textbook
// best case), KSC (28.6°N — Saturn V default), Baikonur (45.6°N),
// Plesetsk (62.8°N — high-inclination polar launches), 90°N (north
// pole — zero spin assist, sanity check that the model bottoms out).
// v0.9.2+.
var latitudePresets = []float64{0.0, 28.6, 45.6, 62.8, 90.0}

// NewSpawnCraft constructs the screen.
func NewSpawnCraft(th Theme) *SpawnCraft { return &SpawnCraft{theme: th} }

// Reset prepares the form for a fresh open. systemBodies is the
// list of bodies in the active system; defaultParentID is the body
// the parent-field cursor lands on initially (typically the active
// craft's current primary). v0.8.2+: replaces the v0.8.2-pre
// no-arg Reset.
func (s *SpawnCraft) Reset(systemBodies []bodies.CelestialBody, defaultParentID string) {
	s.fieldIdx = 0
	s.loadoutIdx = 0
	s.posMode = posOrbit
	s.altIdx = 1 // 500 km — matches the v0.8.1 sister-spawn default
	s.latIdx = 1 // 28.6° KSC — matches the v0.9.2 launchpad default
	s.retrograde = false
	s.parentBodies = systemBodies
	s.parentIdx = 0
	for i, b := range systemBodies {
		if b.ID == defaultParentID {
			s.parentIdx = i
			break
		}
	}
}

// SpawnAction enumerates the form's outcomes.
type SpawnAction int

const (
	SpawnActionNone    SpawnAction = iota
	SpawnActionCancel              // esc
	SpawnActionConfirm             // enter — caller reads accessors
)

// SelectedLoadoutID returns the loadout ID for the current cursor.
func (s *SpawnCraft) SelectedLoadoutID() string {
	if s.loadoutIdx < 0 || s.loadoutIdx >= len(spacecraft.LoadoutOrder) {
		return spacecraft.LoadoutOrder[0]
	}
	return spacecraft.LoadoutOrder[s.loadoutIdx]
}

// SelectedParentID returns the body ID the cursor is on, or empty
// if the body list is unset (caller falls back to the active
// craft's primary).
func (s *SpawnCraft) SelectedParentID() string {
	if s.parentIdx < 0 || s.parentIdx >= len(s.parentBodies) {
		return ""
	}
	return s.parentBodies[s.parentIdx].ID
}

// SelectedAltitudeM returns the chosen altitude above the parent's
// mean radius (m).
func (s *SpawnCraft) SelectedAltitudeM() float64 {
	if s.altIdx < 0 || s.altIdx >= len(altitudePresets) {
		return 500e3
	}
	return float64(altitudePresets[s.altIdx]) * 1000
}

// SelectedRetrograde reports whether the player picked retrograde.
func (s *SpawnCraft) SelectedRetrograde() bool { return s.retrograde }

// SelectedAlongside reports whether the player picked the
// "alongside active craft" position. v0.8.3+.
func (s *SpawnCraft) SelectedAlongside() bool { return s.posMode == posAlongside }

// SelectedLaunchpad reports whether the player picked the
// surface-launchpad position. v0.9.2+.
func (s *SpawnCraft) SelectedLaunchpad() bool { return s.posMode == posLaunchpad }

// SelectedLatitudeDeg returns the chosen surface latitude (degrees
// north) when SelectedLaunchpad is true. Defaults to KSC (28.6°N)
// when the cursor is out of range. v0.9.2+.
func (s *SpawnCraft) SelectedLatitudeDeg() float64 {
	if s.latIdx < 0 || s.latIdx >= len(latitudePresets) {
		return 28.6
	}
	return latitudePresets[s.latIdx]
}

// HandleKey maps a raw key string to a SpawnAction. Tab cycles
// fields; ←/→ edit the focused field; Enter commits; Esc cancels.
func (s *SpawnCraft) HandleKey(key string) SpawnAction {
	const numFields = 5
	switch key {
	case "esc":
		return SpawnActionCancel
	case "enter":
		return SpawnActionConfirm
	case "tab", "down":
		s.fieldIdx = (s.fieldIdx + 1) % numFields
	case "shift+tab", "up":
		s.fieldIdx = (s.fieldIdx - 1 + numFields) % numFields
	case "left", "h":
		s.cycleField(-1)
	case "right", "l":
		s.cycleField(+1)
	}
	return SpawnActionNone
}

// cycleField nudges the focused field's value by step (typically
// ±1). Each field has its own wrap-around behaviour. v0.8.3+:
// added the position toggle (orbit / alongside). v0.9.2+: position
// is now a tri-state cycle (orbit / alongside / launchpad), and
// field 3 doubles as latitude when posMode=launchpad.
func (s *SpawnCraft) cycleField(step int) {
	switch s.fieldIdx {
	case 0:
		s.loadoutIdx = wrapIdx(s.loadoutIdx+step, len(spacecraft.LoadoutOrder))
	case 1:
		s.posMode = spawnPosMode(wrapIdx(int(s.posMode)+step, 3))
	case 2:
		if len(s.parentBodies) > 0 {
			s.parentIdx = wrapIdx(s.parentIdx+step, len(s.parentBodies))
		}
	case 3:
		if s.posMode == posLaunchpad {
			s.latIdx = wrapIdx(s.latIdx+step, len(latitudePresets))
		} else {
			s.altIdx = wrapIdx(s.altIdx+step, len(altitudePresets))
		}
	case 4:
		s.retrograde = !s.retrograde
	}
}

func wrapIdx(i, n int) int {
	if n <= 0 {
		return 0
	}
	for i < 0 {
		i += n
	}
	return i % n
}

// Render returns the modal form. Width is the terminal width.
func (s *SpawnCraft) Render(width int) string {
	var lines []string

	const titleText = "terminal-space-program — spawn craft"
	lines = append(lines, s.theme.Title.Render(titleText))
	lines = append(lines, "")

	// Field 0: craft type — list with the cursor row highlighted.
	lines = append(lines, s.fieldHeader(0, "CRAFT TYPE"))
	lines = append(lines, "")
	for i, id := range spacecraft.LoadoutOrder {
		l := spacecraft.Loadouts[id]
		row := fmt.Sprintf("%s %s  %s  — %s",
			l.Glyph, l.Name, l.Role, propulsionSummary(l))
		marker := "  "
		if i == s.loadoutIdx {
			marker = s.theme.Warning.Render("→ ")
			if s.fieldIdx == 0 {
				row = s.theme.Warning.Render(row)
			} else {
				row = s.theme.Primary.Render(row)
			}
		} else {
			row = s.theme.Dim.Render(row)
		}
		lines = append(lines, "  "+marker+row)
	}

	// Field 1: position mode — tri-state cycle. orbit (uses PARENT
	// + ALTITUDE + DIRECTION below); alongside (drops inside
	// docking gate, all three ignored); launchpad (surface, parent
	// + LATITUDE only — direction ignored).
	lines = append(lines, "")
	lines = append(lines, s.fieldHeader(1, "POSITION"))
	var posLabel string
	switch s.posMode {
	case posAlongside:
		posLabel = "alongside active (within docking gate)"
	case posLaunchpad:
		posLabel = "launchpad (surface, co-rotating)"
	default:
		posLabel = "circular orbit"
	}
	lines = append(lines, "  "+s.fieldValue(1, posLabel))

	// Field-3 + field-4 dim/disable masks vary by mode:
	// - orbit:     all three orbit-defining fields enabled
	// - alongside: parent + alt + direction all dimmed
	// - launchpad: parent + latitude (replaces alt) enabled,
	//              direction dimmed
	dimParent := s.posMode == posAlongside
	dimAlt := s.posMode != posOrbit
	dimDir := s.posMode != posOrbit

	// Field 2: parent body — single-line cycle.
	lines = append(lines, "")
	lines = append(lines, s.fieldHeader(2, "PARENT BODY"))
	parentLabel := "(none)"
	if pb := s.currentParent(); pb != nil {
		parentLabel = fmt.Sprintf("%s  (μ %.2e, R %.0f km)",
			pb.EnglishName, pb.GravitationalParameter(), pb.RadiusMeters()/1000)
	}
	lines = append(lines, "  "+s.fieldValueDimmed(2, parentLabel, dimParent))

	// Field 3: altitude (orbit) or latitude (launchpad) — preset cycle.
	lines = append(lines, "")
	if s.posMode == posLaunchpad {
		lines = append(lines, s.fieldHeader(3, "LATITUDE"))
		latLabel := fmt.Sprintf("%.1f° N", latitudePresets[s.latIdx])
		if latitudePresets[s.latIdx] < 0 {
			latLabel = fmt.Sprintf("%.1f° S", -latitudePresets[s.latIdx])
		}
		lines = append(lines, "  "+s.fieldValueDimmed(3, latLabel, false))
	} else {
		lines = append(lines, s.fieldHeader(3, "ALTITUDE"))
		altLabel := fmt.Sprintf("%d km", altitudePresets[s.altIdx])
		lines = append(lines, "  "+s.fieldValueDimmed(3, altLabel, dimAlt))
	}

	// Field 4: direction — toggle. Ignored in launchpad mode.
	lines = append(lines, "")
	lines = append(lines, s.fieldHeader(4, "DIRECTION"))
	dirLabel := "prograde"
	if s.retrograde {
		dirLabel = "retrograde"
	}
	lines = append(lines, "  "+s.fieldValueDimmed(4, dirLabel, dimDir))

	lines = append(lines, "")
	lines = append(lines, s.theme.Dim.Render(strings.Repeat("─", 60)))
	lines = append(lines, s.theme.Footer.Render(
		"[tab] field  [←/→] cycle  [enter] spawn  [esc] cancel"))

	return strings.Join(lines, "\n")
}

// fieldHeader returns the header label, highlighted when the field
// is focused.
func (s *SpawnCraft) fieldHeader(idx int, label string) string {
	if s.fieldIdx == idx {
		return s.theme.Warning.Render("▶ " + label)
	}
	return s.theme.Primary.Render("  " + label)
}

// fieldValue returns the rendered value, with cycle hints when the
// field is focused.
func (s *SpawnCraft) fieldValue(idx int, label string) string {
	if s.fieldIdx == idx {
		return s.theme.Warning.Render("◀  " + label + "  ▶")
	}
	return label
}

// fieldValueDimmed is fieldValue with an "inactive" state — used
// for orbit-defining fields when POSITION = alongside makes them
// irrelevant. v0.8.3+.
func (s *SpawnCraft) fieldValueDimmed(idx int, label string, dimmed bool) string {
	if dimmed {
		return s.theme.Dim.Render(label + "  (ignored)")
	}
	return s.fieldValue(idx, label)
}

// currentParent returns the body the cursor is on, or nil.
func (s *SpawnCraft) currentParent() *bodies.CelestialBody {
	if s.parentIdx < 0 || s.parentIdx >= len(s.parentBodies) {
		return nil
	}
	return &s.parentBodies[s.parentIdx]
}

// propulsionSummary one-lines a loadout's main-engine + RCS shape
// for the form preview. Pure-RCS craft (Thrust=0) call it out
// explicitly so the player knows `b` won't fire on that loadout.
//
// v0.9.1+ multi-stage loadouts list a stage count next to the dry
// mass so the player can see at a glance that the Saturn-V is a
// 3-stage chain instead of a single tank.
func propulsionSummary(l spacecraft.Loadout) string {
	dry := spacecraft.SumDryMass(l.Stages)
	fuel := spacecraft.SumFuelMass(l.Stages)
	bottomThrust := l.Thrust()
	bottomIsp := l.Isp()
	stageNote := ""
	if len(l.Stages) > 1 {
		stageNote = fmt.Sprintf(" (%d stages)", len(l.Stages))
	}
	if bottomThrust == 0 {
		return fmt.Sprintf("dry %.0fkg%s, RCS-only", dry, stageNote)
	}
	return fmt.Sprintf("dry %.0fkg, fuel %.0fkg%s, %.0fkN @ Isp %.0fs",
		dry, fuel, stageNote, bottomThrust/1000, bottomIsp)
}
