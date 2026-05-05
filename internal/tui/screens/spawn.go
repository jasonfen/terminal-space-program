package screens

import (
	"fmt"
	"strings"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// SpawnCraft is the modal form opened by `n` on the orbit screen.
// v0.8.2+: four fields — craft type, parent body, altitude, and
// direction (prograde / retrograde). Tab cycles field focus; ←/→
// edit the focused field; Enter spawns; Esc cancels.
type SpawnCraft struct {
	theme    Theme
	fieldIdx int // 0=loadout, 1=position, 2=parent, 3=altitude, 4=direction

	loadoutIdx   int
	alongside    bool // v0.8.3+: spawn next to active craft for docking testing
	parentBodies []bodies.CelestialBody // populated by Reset
	parentIdx    int
	altIdx       int
	retrograde   bool
}

// altitudePresets are the cycle values for the altitude field —
// km above the parent's mean radius. Hand-picked across orders of
// magnitude so the cycle covers LEO-style parking orbits, GEO-ish
// transfer alts, and high-Earth / interplanetary capture orbits.
var altitudePresets = []int{200, 500, 1000, 2000, 5000, 10000, 35786}

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
	s.alongside = false
	s.altIdx = 1 // 500 km — matches the v0.8.1 sister-spawn default
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
func (s *SpawnCraft) SelectedAlongside() bool { return s.alongside }

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
// adds the position toggle (orbit / alongside).
func (s *SpawnCraft) cycleField(step int) {
	switch s.fieldIdx {
	case 0:
		s.loadoutIdx = wrapIdx(s.loadoutIdx+step, len(spacecraft.LoadoutOrder))
	case 1:
		s.alongside = !s.alongside
	case 2:
		if len(s.parentBodies) > 0 {
			s.parentIdx = wrapIdx(s.parentIdx+step, len(s.parentBodies))
		}
	case 3:
		s.altIdx = wrapIdx(s.altIdx+step, len(altitudePresets))
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

	// Field 1: position mode — toggle between "orbit" (uses
	// PARENT BODY + ALTITUDE + DIRECTION below) and "alongside"
	// (drops the new craft inside the docking gate of the active
	// craft for testing).
	lines = append(lines, "")
	lines = append(lines, s.fieldHeader(1, "POSITION"))
	posLabel := "circular orbit"
	if s.alongside {
		posLabel = "alongside active (within docking gate)"
	}
	lines = append(lines, "  "+s.fieldValue(1, posLabel))

	// When alongside is picked, the orbit-defining fields below
	// don't apply — render them dimmed so the player sees they're
	// inactive.
	dimWhen := s.alongside

	// Field 2: parent body — single-line cycle.
	lines = append(lines, "")
	lines = append(lines, s.fieldHeader(2, "PARENT BODY"))
	parentLabel := "(none)"
	if pb := s.currentParent(); pb != nil {
		parentLabel = fmt.Sprintf("%s  (μ %.2e, R %.0f km)",
			pb.EnglishName, pb.GravitationalParameter(), pb.RadiusMeters()/1000)
	}
	lines = append(lines, "  "+s.fieldValueDimmed(2, parentLabel, dimWhen))

	// Field 3: altitude — single-line preset cycle.
	lines = append(lines, "")
	lines = append(lines, s.fieldHeader(3, "ALTITUDE"))
	altLabel := fmt.Sprintf("%d km", altitudePresets[s.altIdx])
	lines = append(lines, "  "+s.fieldValueDimmed(3, altLabel, dimWhen))

	// Field 4: direction — toggle.
	lines = append(lines, "")
	lines = append(lines, s.fieldHeader(4, "DIRECTION"))
	dirLabel := "prograde"
	if s.retrograde {
		dirLabel = "retrograde"
	}
	lines = append(lines, "  "+s.fieldValueDimmed(4, dirLabel, dimWhen))

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
