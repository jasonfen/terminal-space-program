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

// missionsLegend is the key legend on the frame's bottom edge.
const missionsLegend = "[1] Flight School on/off · [2] Challenge ladder on/off · [esc] back"

// Render returns the ladder/program screen inside the shared form frame
// (B11 / G9 Q4). width x height is the whole framed block. Empty-catalog
// worlds show a placeholder so the player isn't faced with a blank screen.
//
// #426 item F reshaped the body: the active mission (if any) still owns a
// box on top, but the rest of the ladder is two headed lists, FLIGHT
// SCHOOL and CHALLENGES, side by side, each with its own N/M complete
// count. A program that's switched off shows one dim offer row under its
// header (its one-key toggle) instead of its mission list; that offer row
// is also what a player sees for BOTH programs at once, replacing the old
// flat "missions off, enable … in Settings" placeholder.
func (m *Missions) Render(w *sim.World, width, height int) string {
	legend := m.theme.Footer.Render(missionsLegend)
	inner := width - 2*frameInset
	if len(w.Missions) == 0 {
		box := formBox(m.theme, "MISSIONS", []string{m.theme.Dim.Render("  (no missions loaded)")}, clampI(inner, 20, 60))
		return formFrame(m.theme, box, width, height, m.theme.Footer.Render("[esc] back"))
	}

	activeMission := w.ActiveMission()
	var activeID string
	if activeMission != nil {
		activeID = activeMission.ID
	}

	var body []string
	// Active box on top (ADR 0025 Slice 5, Jason's "active card" layout):
	// the current mission, expanded to its objective checklist with the
	// current step's hint, so it reads as "what now". With nothing active,
	// a completed Program's Sendoff takes the same slot (#426 item F,
	// decision 9).
	switch {
	case activeMission != nil:
		row := ladderRow{Name: activeMission.Name, Category: ladderActive, Objectives: activeMission.Objectives}
		body = append(body, formBox(m.theme, "ACTIVE: "+row.Name, m.activeLines(row, inner-2), inner)...)
	default:
		if text, offer, ok := w.LadderSendoff(); ok {
			body = append(body, formBox(m.theme, "SENDOFF", m.sendoffLines(text, offer, inner-2), inner)...)
		}
	}

	lw := (inner - 1) / 2
	rw := inner - lw - 1
	ft, fl := m.programLines("FLIGHT SCHOOL", "Flight School", "1",
		programMissions(w.Missions, missions.ProgramTutorial),
		w.MissionProgramEnabled(missions.ProgramTutorial), activeID, lw-2)
	ct, cl := m.programLines("CHALLENGES", "the Challenge ladder", "2",
		programMissions(w.Missions, missions.ProgramChallenge),
		w.MissionProgramEnabled(missions.ProgramChallenge), activeID, rw-2)
	body = append(body, joinBoxes(formBox(m.theme, ft, fl, lw), formBox(m.theme, ct, cl, rw), lw, 1)...)
	return formFrame(m.theme, body, width, height, legend)
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
func (m *Missions) programLines(heading, label, key string, ms []missions.Mission, enabled bool, activeID string, max int) (title string, lines []string) {
	passed := 0
	for i := range ms {
		if ms[i].Status == missions.Passed {
			passed++
		}
	}
	title = fmt.Sprintf("%s  %d/%d complete", heading, passed, len(ms))
	if !enabled {
		return title, []string{m.theme.Dim.Render(fmt.Sprintf("  [%s] turn on %s", key, label))}
	}
	for _, r := range classifyLadder(ms, activeID) {
		if r.Category == ladderActive {
			continue // owns the box above
		}
		lines = append(lines, clipLine(m.ladderRowLine(r), max))
	}
	lines = append(lines, m.theme.Dim.Render(fmt.Sprintf("  [%s] turn off %s", key, label)))
	return title, lines
}

// sendoffLines is the whole-Program-complete state in the same box as the
// active mission, so it reads as "what now" the same way (#426 item F,
// decision 9).
func (m *Missions) sendoffLines(text string, offerChallenges bool, max int) []string {
	if max < 8 {
		max = 8
	}
	lines := []string{clipLine(m.theme.Primary.Render(text), max)}
	if offerChallenges {
		lines = append(lines, clipLine(m.theme.Dim.Render("  [2] turn on the Challenge ladder"), max))
	}
	return lines
}

// activeLines renders the active-mission box body: each objective with a ✓ (passed) / ▸ (current) / · (upcoming)
// marker, and the current objective's hint text indented beneath it (the
// hint that, by Jason's Slice-5 call, lives on the screen rather than the
// in-flight chip).
func (m *Missions) activeLines(r ladderRow, max int) []string {
	// Clamp each content line to the box so a long objective hint can't
	// push the border off a narrow screen.
	if max < 8 {
		max = 8
	}
	var lines []string
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
	return lines
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
