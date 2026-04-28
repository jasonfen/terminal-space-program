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

	// backColStart / backColEnd track the [Back] click-target column
	// range, recomputed on every Render so terminal-resize doesn't
	// stale the hit-test. v0.7.4+.
	backColStart, backColEnd int
}

func NewMissions(th Theme) *Missions { return &Missions{theme: th} }

// Render returns the mission list with status indicators and a
// progress summary. Empty-catalog worlds show a placeholder so the
// player isn't faced with a blank screen. width is the terminal
// width — used to right-align the [Back] button on row 0.
func (m *Missions) Render(w *sim.World, width int) string {
	const titleText = "missions"
	const backLabel = "[Back]"
	var b strings.Builder
	pad := width - len([]rune(titleText)) - len([]rune(backLabel))
	if pad < 1 {
		pad = 1
	}
	m.backColStart = len([]rune(titleText)) + pad
	m.backColEnd = m.backColStart + len([]rune(backLabel))
	b.WriteString(m.theme.Title.Render(titleText))
	b.WriteString(strings.Repeat(" ", pad))
	b.WriteString(m.theme.Primary.Render(backLabel))
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

// HitBackButton reports whether a click at (col, row) lands on the
// title-row [Back] button. v0.7.4+.
func (m *Missions) HitBackButton(col, row int) bool {
	return row == 0 && col >= m.backColStart && col < m.backColEnd
}
