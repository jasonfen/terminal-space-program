package screens

import (
	"fmt"
	"strings"

	"github.com/jasonfen/terminal-space-program/internal/missions"
	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// Missions is the v0.7.4+ dedicated mission-list screen. Reachable
// from the orbit-screen title bar's `[Missions]` button (or the
// keyboard via app.go's MissionsKey). Lists every loaded mission with
// its current status; the orbit HUD's MISSION block was removed in
// v0.7.4 to give the right-hand pane back to flight state.
type Missions struct {
	theme Theme
}

func NewMissions(th Theme) *Missions { return &Missions{theme: th} }

// Render returns the mission list with status indicators and a
// progress summary. Empty-catalog worlds show a placeholder so the
// player isn't faced with a blank screen.
func (m *Missions) Render(w *sim.World) string {
	var b strings.Builder
	b.WriteString(m.theme.Title.Render("missions"))
	b.WriteString("\n")
	b.WriteString(m.theme.Dim.Render(strings.Repeat("─", 40)))
	b.WriteString("\n\n")

	if len(w.Missions) == 0 {
		b.WriteString(m.theme.Dim.Render("  (no missions loaded)"))
		b.WriteString("\n\n")
		b.WriteString(m.theme.Footer.Render("[esc] back to orbit"))
		return b.String()
	}

	passed, failed := 0, 0
	for _, ms := range w.Missions {
		switch ms.Status {
		case missions.Passed:
			passed++
		case missions.Failed:
			failed++
		}
	}
	summary := fmt.Sprintf("%d/%d complete", passed, len(w.Missions))
	if failed > 0 {
		summary += fmt.Sprintf("  (%d failed)", failed)
	}
	b.WriteString("  " + m.theme.Primary.Render(summary))
	b.WriteString("\n\n")

	for i, ms := range w.Missions {
		marker := m.theme.Dim.Render("  · ")
		switch ms.Status {
		case missions.Passed:
			marker = m.theme.Primary.Render("  ✓ ")
		case missions.Failed:
			marker = m.theme.Alert.Render("  ✗ ")
		}
		b.WriteString(marker)
		b.WriteString(ms.Name)
		b.WriteByte('\n')
		statusLine := m.theme.Dim.Render("      " + ms.Status.String())
		b.WriteString(statusLine)
		b.WriteByte('\n')
		if ms.Description != "" {
			b.WriteString(m.theme.Dim.Render("      " + ms.Description))
			b.WriteByte('\n')
		}
		if i < len(w.Missions)-1 {
			b.WriteByte('\n')
		}
	}

	b.WriteString("\n")
	b.WriteString(m.theme.Footer.Render("[esc] back to orbit"))
	return b.String()
}
