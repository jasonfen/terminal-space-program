package screens

import (
	"fmt"
	"strings"

	"github.com/jasonfen/terminal-space-program/internal/missions"
	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// Missions is the v0.7.4+ dedicated mission screen, rebuilt in v0.21
// (ADR 0025 Slice 5) into a gated ladder/program view: the active mission
// shows as a highlighted card on top (its name + live objective checklist
// with the current step's hint), and the rest of the ladder lists below —
// completed rungs checked, locked rungs shown-locked with a requirement
// hint, failed rungs marked. Reachable from the orbit-screen title bar's
// `[Missions]` button or the `M` keybinding (ADR 0025 Slice 5).
type Missions struct {
	theme Theme

	// backColStart / backColEnd track the [Back] click-target column
	// range, recomputed on every Render so terminal-resize doesn't
	// stale the hit-test. v0.7.4+.
	backColStart, backColEnd int
}

func NewMissions(th Theme) *Missions { return &Missions{theme: th} }

// ladderCategory is the render bucket each mission falls into on the ladder
// screen (ADR 0025 Slice 5). Drives the marker, styling, and whether the
// rung gets the active card.
type ladderCategory int

const (
	ladderCompleted ladderCategory = iota // Passed
	ladderActive                          // first unlocked InProgress — gets the card
	ladderAvailable                       // unlocked InProgress, but not the active one
	ladderLocked                          // InProgress with unmet requires
	ladderFailed                          // Failed
)

// ladderRow is one classified rung in the render-model. Objectives is
// populated only for the active row (the card needs the full checklist);
// Hint carries the "needs: …" requirement text for a locked rung.
type ladderRow struct {
	Name       string
	Category   ladderCategory
	Hint       string
	Objectives []missions.Objective
}

// classifyLadder is the pure render-model for the ladder screen: it buckets
// each mission and computes locked-rung hints, given the catalog's current
// per-mission Status. The active rung is the FIRST unlocked InProgress
// mission — every later unlocked InProgress one is merely available. A
// mission is locked when any mission ID in its Requires is not yet Passed;
// its hint names the unmet prerequisites. v0.21 Slice 5 (ADR 0025 §2/§"Locked
// rungs").
func classifyLadder(ms []missions.Mission) []ladderRow {
	passed := missions.PassedSet(ms)
	nameByID := make(map[string]string, len(ms))
	for i := range ms {
		nameByID[ms[i].ID] = ms[i].Name
	}
	activeAssigned := false
	rows := make([]ladderRow, 0, len(ms))
	for i := range ms {
		m := ms[i]
		row := ladderRow{Name: m.Name}
		switch {
		case m.Status == missions.Passed:
			row.Category = ladderCompleted
		case m.Status == missions.Failed:
			row.Category = ladderFailed
		case !m.RequirementsMet(passed):
			row.Category = ladderLocked
			row.Hint = lockedHint(m, nameByID, passed)
		case !activeAssigned:
			row.Category = ladderActive
			row.Objectives = m.Objectives
			activeAssigned = true
		default:
			row.Category = ladderAvailable
		}
		rows = append(rows, row)
	}
	return rows
}

// lockedHint builds the "needs: A, B" requirement text for a locked rung,
// naming the prerequisite missions that have not yet Passed (falling back to
// the raw ID when a requirement names an unknown mission).
func lockedHint(m missions.Mission, nameByID map[string]string, passed map[string]bool) string {
	var need []string
	for _, id := range m.Requires {
		if passed[id] {
			continue
		}
		n := nameByID[id]
		if n == "" {
			n = id
		}
		need = append(need, n)
	}
	if len(need) == 0 {
		return ""
	}
	return "needs: " + strings.Join(need, ", ")
}

// Render returns the ladder/program screen. width is the terminal width —
// used to right-align the [Back] button on row 0. Empty-catalog worlds show
// a placeholder so the player isn't faced with a blank screen.
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

	rows := classifyLadder(w.Missions)

	// Active card on top (ADR 0025 Slice 5 — Jason's "active card" layout):
	// the current mission, expanded to its objective checklist with the
	// current step's hint, framed in the HUD box so it reads as "what now".
	for _, r := range rows {
		if r.Category == ladderActive {
			b.WriteString(m.activeCard(r, width))
			b.WriteString("\n\n")
			break
		}
	}

	// The rest of the ladder lists below the card. The active rung is omitted
	// here — it already owns the card — so each remaining rung shows once.
	for _, r := range rows {
		if r.Category == ladderActive {
			continue
		}
		b.WriteString(m.ladderRowLine(r))
		b.WriteByte('\n')
	}

	b.WriteString("\n")
	b.WriteString(m.theme.Footer.Render("[esc] back to orbit"))
	return b.String()
}

// activeCard renders the highlighted active-mission card: an "ACTIVE: <name>"
// header, each objective with a ✓ (passed) / ▸ (current) / · (upcoming)
// marker, and the current objective's hint text indented beneath it (the
// hint that, by Jason's Slice-5 call, lives on the screen rather than the
// in-flight chip).
func (m *Missions) activeCard(r ladderRow, width int) string {
	// Clamp each content line to the terminal width less the box chrome
	// (rounded border = 2 cols + Padding(0,1) = 2 cols) so a long objective
	// hint can't push the box border off a narrow screen.
	max := width - 4
	if max < 8 {
		max = 8
	}
	lines := []string{clipLine(m.theme.Primary.Render("ACTIVE: "+r.Name), max)}
	currentSeen := false
	for _, o := range r.Objectives {
		marker, isCurrent := "  · ", false
		switch {
		case o.Status == missions.Passed:
			marker = "  ✓ "
		case o.Status == missions.Failed:
			marker = "  ✗ "
		case !currentSeen:
			marker, isCurrent = "  ▸ ", true
			currentSeen = true
		}
		lines = append(lines, clipLine(marker+o.Label(), max))
		// The current step's hint surfaces here (no hint in the chip).
		if isCurrent && o.Description != "" {
			lines = append(lines, clipLine(m.theme.Dim.Render("      "+o.Description), max))
		}
	}
	return m.theme.HUDBox.Render(strings.Join(lines, "\n"))
}

// ladderRowLine renders one non-active rung in the list below the card:
// completed rungs checked, available rungs as a pending dot, locked rungs
// dimmed with their requirement hint, failed rungs marked.
func (m *Missions) ladderRowLine(r ladderRow) string {
	switch r.Category {
	case ladderCompleted:
		return m.theme.Primary.Render("  ✓ " + r.Name)
	case ladderFailed:
		return m.theme.Alert.Render("  ✗ " + r.Name + "  (failed)")
	case ladderLocked:
		line := "  · " + r.Name
		if r.Hint != "" {
			line += "   " + r.Hint
		}
		return m.theme.Dim.Render(line)
	default: // ladderAvailable
		return "  · " + r.Name
	}
}

// HitBackButton reports whether a click at (col, row) lands on the
// title-row [Back] button. v0.7.4+.
func (m *Missions) HitBackButton(col, row int) bool {
	return row == 0 && col >= m.backColStart && col < m.backColEnd
}
