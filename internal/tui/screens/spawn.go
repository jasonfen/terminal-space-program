package screens

import (
	"fmt"
	"strings"

	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// SpawnCraft is the modal form opened by `n` on the orbit screen.
// v0.8.2 ships a minimal one-field form — the player picks a craft
// type from spacecraft.LoadoutOrder and confirms with Enter. Future
// patches add altitude / parent-body / direction fields per the
// v0.8 plan §v0.8.1 spawn-form section.
type SpawnCraft struct {
	theme      Theme
	loadoutIdx int
}

// NewSpawnCraft constructs the screen.
func NewSpawnCraft(th Theme) *SpawnCraft { return &SpawnCraft{theme: th} }

// Reset returns the form to its default state — the v0.8 plan's
// open scoping question on persistence (#21) keeps this default-
// fresh for now; if playtest finds the player wants the last pick
// remembered, drop the reset call from app.Update.
func (s *SpawnCraft) Reset() { s.loadoutIdx = 0 }

// SpawnAction enumerates the form's outcomes.
type SpawnAction int

const (
	SpawnActionNone    SpawnAction = iota
	SpawnActionCancel              // esc
	SpawnActionConfirm             // enter — caller reads SelectedLoadoutID
)

// SelectedLoadoutID returns the loadout ID corresponding to the
// current cursor position. Caller passes this into
// sim.World.SpawnCraft via SpawnSpec.LoadoutID.
func (s *SpawnCraft) SelectedLoadoutID() string {
	if s.loadoutIdx < 0 || s.loadoutIdx >= len(spacecraft.LoadoutOrder) {
		return spacecraft.LoadoutOrder[0]
	}
	return spacecraft.LoadoutOrder[s.loadoutIdx]
}

// HandleKey maps a raw key string to a SpawnAction. Left/right
// arrows cycle the loadout field; Enter commits; Esc cancels.
// Number keys 1..N jump directly to a loadout (1-indexed).
func (s *SpawnCraft) HandleKey(key string) SpawnAction {
	switch key {
	case "esc":
		return SpawnActionCancel
	case "enter":
		return SpawnActionConfirm
	case "left", "h":
		s.loadoutIdx--
		if s.loadoutIdx < 0 {
			s.loadoutIdx = len(spacecraft.LoadoutOrder) - 1
		}
	case "right", "l":
		s.loadoutIdx++
		if s.loadoutIdx >= len(spacecraft.LoadoutOrder) {
			s.loadoutIdx = 0
		}
	default:
		// Digit keys 1..N pick the Nth loadout directly.
		if len(key) == 1 && key[0] >= '1' && key[0] <= '9' {
			idx := int(key[0]-'1')
			if idx < len(spacecraft.LoadoutOrder) {
				s.loadoutIdx = idx
			}
		}
	}
	return SpawnActionNone
}

// Render returns the modal form. Width is the terminal width.
func (s *SpawnCraft) Render(width int) string {
	var lines []string

	// Title bar.
	const titleText = "terminal-space-program — spawn craft"
	lines = append(lines, s.theme.Title.Render(titleText))
	lines = append(lines, "")

	// Field label + selector.
	lines = append(lines, s.theme.Primary.Render("CRAFT TYPE"))
	lines = append(lines, "")

	for i, id := range spacecraft.LoadoutOrder {
		l := spacecraft.Loadouts[id]
		marker := "  "
		row := fmt.Sprintf("%s %s  %s  — %s",
			l.Glyph, l.Name, l.Role, propulsionSummary(l))
		if i == s.loadoutIdx {
			marker = s.theme.Warning.Render("→ ")
			row = s.theme.Warning.Render(row)
		} else {
			row = s.theme.Dim.Render(row)
		}
		lines = append(lines, "  "+marker+row)
	}

	lines = append(lines, "")
	lines = append(lines, s.theme.Dim.Render(strings.Repeat("─", 60)))
	lines = append(lines, s.theme.Footer.Render(
		"[←/→] cycle  [1-4] jump  [enter] spawn  [esc] cancel"))

	return strings.Join(lines, "\n")
}

// propulsionSummary one-lines a loadout's main-engine + RCS shape
// for the form preview. Pure-RCS craft (Thrust=0) call it out
// explicitly so the player knows `b` won't fire on that loadout.
func propulsionSummary(l spacecraft.Loadout) string {
	if l.Thrust == 0 {
		return fmt.Sprintf("dry %.0fkg, RCS-only", l.DryMass)
	}
	return fmt.Sprintf("dry %.0fkg, fuel %.0fkg, %.0fkN @ Isp %.0fs",
		l.DryMass, l.Fuel, l.Thrust/1000, l.Isp)
}
