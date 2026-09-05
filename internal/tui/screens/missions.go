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

// classifyLadder is the pure render-model for one program's rung list: it
// buckets each mission and computes locked-rung hints, given the catalog's
// current per-mission Status. activeID names the mission the caller has
// already decided owns the top-of-screen active card (typically
// World.ActiveMission().ID, computed once across BOTH programs — #426 item
// F split the single interleaved ladder into a FLIGHT SCHOOL list and a
// CHALLENGES list, so "the first unlocked InProgress mission in THIS
// program" is no longer the right notion of active; the caller decides once,
// globally, and this just marks whichever row matches). A mission is locked
// when any mission ID in its Requires is not yet Passed; its hint names the
// unmet prerequisites. v0.21 Slice 5 (ADR 0025 §2/§"Locked rungs"); split by
// program #426.
func classifyLadder(ms []missions.Mission, activeID string) []ladderRow {
	passed := missions.PassedSet(ms)
	nameByID := make(map[string]string, len(ms))
	for i := range ms {
		nameByID[ms[i].ID] = ms[i].Name
	}
	rows := make([]ladderRow, 0, len(ms))
	for i := range ms {
		m := ms[i]
		row := ladderRow{Name: m.Name}
		switch {
		case m.Status == missions.Passed:
			row.Category = ladderCompleted
		case m.Status == missions.Failed:
			row.Category = ladderFailed
		case activeID != "" && m.ID == activeID:
			row.Category = ladderActive
			row.Objectives = m.Objectives
		case !m.RequirementsMet(passed):
			row.Category = ladderLocked
			row.Hint = lockedHint(m, nameByID, passed)
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
//
// #426 item F reshaped the body: the active mission (if any) still owns a
// card on top, but the rest of the ladder is now two headed lists — FLIGHT
// SCHOOL and CHALLENGES — each with its own N/M complete count, instead of
// one interleaved list gated on both programs being on. A program that's
// switched off shows one dim offer row under its header (its one-key
// toggle) instead of its mission list; that offer row is also what a
// player sees for BOTH programs at once, replacing the old flat "missions
// off — enable … in Settings" placeholder.
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

	activeMission := w.ActiveMission()
	var activeID string
	if activeMission != nil {
		activeID = activeMission.ID
	}

	// Active card on top (ADR 0025 Slice 5 — Jason's "active card" layout):
	// the current mission, expanded to its objective checklist with the
	// current step's hint, framed in the HUD box so it reads as "what now".
	// With nothing active, a completed Program's Sendoff takes the same
	// slot instead of leaving it blank (#426 item F, decision 9).
	switch {
	case activeMission != nil:
		row := ladderRow{Name: activeMission.Name, Category: ladderActive, Objectives: activeMission.Objectives}
		b.WriteString(m.activeCard(row, width))
		b.WriteString("\n\n")
	default:
		if text, offer, ok := w.LadderSendoff(); ok {
			b.WriteString(m.sendoffCard(text, offer, width))
			b.WriteString("\n\n")
		}
	}

	b.WriteString(m.programSection("FLIGHT SCHOOL", "Flight School", "1",
		programMissions(w.Missions, missions.ProgramTutorial),
		w.MissionProgramEnabled(missions.ProgramTutorial), activeID))
	b.WriteByte('\n')
	b.WriteString(m.programSection("CHALLENGES", "the Challenge ladder", "2",
		programMissions(w.Missions, missions.ProgramChallenge),
		w.MissionProgramEnabled(missions.ProgramChallenge), activeID))

	b.WriteString("\n")
	b.WriteString(m.theme.Footer.Render("[esc] back to orbit"))
	return b.String()
}

// programMissions filters ms to the ones tagged with the given Program, in
// catalog order. An untagged mission (Program == "") never appears in
// either headed list — the shipped catalog tags every mission, so this is a
// documented simplification for a hypothetical modder catalog entry with no
// Program tag, not a real-world gap.
func programMissions(ms []missions.Mission, program string) []missions.Mission {
	var out []missions.Mission
	for _, mi := range ms {
		if mi.Program == program {
			out = append(out, mi)
		}
	}
	return out
}

// programSection renders one headed sub-list: the header line with its own
// "N/M complete" count (computed regardless of whether the program is
// currently enabled, so a player who switched it off can still see how far
// they got), then either the classified rung list (the active rung, if any
// of these missions owns it, is skipped — the card above already shows it)
// or, when the program is off, one dim offer row naming its one-key toggle
// (#426 item F, decision 5). When the program is on, the same key is
// offered as "[N] turn off <label>" under its list, so a player who wants
// to play unguided can opt out of Flight School in three presses (M, 1,
// esc) instead of a trip through Settings (Jason, 2026-09-04). heading is
// the section's all-caps label ("FLIGHT SCHOOL"); label is the lower-case
// name used in the offer row's "[N] turn on/off <label>" text; key is that
// digit ("1" or "2").
func (m *Missions) programSection(heading, label, key string, ms []missions.Mission, enabled bool, activeID string) string {
	var b strings.Builder
	passed := 0
	for i := range ms {
		if ms[i].Status == missions.Passed {
			passed++
		}
	}
	b.WriteString(m.theme.Primary.Render(fmt.Sprintf("%s  %d/%d complete", heading, passed, len(ms))))
	b.WriteByte('\n')
	if !enabled {
		b.WriteString(m.theme.Dim.Render(fmt.Sprintf("  [%s] turn on %s", key, label)))
		b.WriteByte('\n')
		return b.String()
	}
	for _, r := range classifyLadder(ms, activeID) {
		if r.Category == ladderActive {
			continue // owns the card above
		}
		b.WriteString(m.ladderRowLine(r))
		b.WriteByte('\n')
	}
	b.WriteString(m.theme.Dim.Render(fmt.Sprintf("  [%s] turn off %s", key, label)))
	b.WriteByte('\n')
	return b.String()
}

// sendoffCard renders the whole-Program-complete state in the same box
// style as activeCard, so it reads as "what now" the same way the active
// card did (#426 item F, decision 9).
func (m *Missions) sendoffCard(text string, offerChallenges bool, width int) string {
	max := width - 4
	if max < 8 {
		max = 8
	}
	lines := []string{clipLine(m.theme.Primary.Render(text), max)}
	if offerChallenges {
		lines = append(lines, clipLine(m.theme.Dim.Render("  [2] turn on the Challenge ladder"), max))
	}
	return m.theme.HUDBox.Render(strings.Join(lines, "\n"))
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

// lockGlyph marks a locked rung — a single-width dingbat (U+26BF SQUARED
// KEY) chosen alongside the game's existing single-width HUD glyph set
// (✓ ✗ ▸ ● ◇ ⚠ ·) rather than an emoji lock (🔒/🔓/⛔ all measure
// double-width via lipgloss.Width, which would throw off column math
// elsewhere the way none of the existing glyphs do). #426 item F.
const lockGlyph = "⚿"

// ladderRowLine renders one non-active rung in the list below the card:
// completed rungs checked, available rungs bright with ▸, locked rungs
// dimmed with a lock glyph and their requirement hint, failed rungs marked.
func (m *Missions) ladderRowLine(r ladderRow) string {
	switch r.Category {
	case ladderCompleted:
		return m.theme.Primary.Render("  ✓ " + r.Name)
	case ladderFailed:
		return m.theme.Alert.Render("  ✗ " + r.Name + "  (failed)")
	case ladderLocked:
		line := "  " + lockGlyph + " " + r.Name
		if r.Hint != "" {
			line += "   " + r.Hint
		}
		return m.theme.Dim.Render(line)
	default: // ladderAvailable
		return "  ▸ " + r.Name
	}
}

// HitBackButton reports whether a click at (col, row) lands on the
// title-row [Back] button. v0.7.4+.
func (m *Missions) HitBackButton(col, row int) bool {
	return row == 0 && col >= m.backColStart && col < m.backColEnd
}
