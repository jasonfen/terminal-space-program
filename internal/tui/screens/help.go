package screens

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// Help is the keybinding reference overlay. Invoked via F1 from any
// screen; F1 or esc returns. The content is taller than most terminals,
// so the body scrolls between a sticky title and a sticky footer
// (↑/↓ PgUp/PgDn Home/End); Render windows the body to the terminal
// height and ANSI-truncates each row to width so one entry stays one row.
type Help struct {
	theme  Theme
	scroll int
	// viewH / maxScroll are cached from the last Render so HandleKey can
	// page and clamp without re-deriving the layout. Zero until first
	// Render (Render runs every frame, so this self-corrects immediately).
	viewH     int
	maxScroll int
}

func NewHelp(th Theme) *Help { return &Help{theme: th} }

type helpSection struct {
	header string
	rows   [][2]string
}

// helpSections groups every binding into logical sections (player-facing,
// so no internal version tags). Keep each description short enough to read
// at ~80 columns; longer rows are ANSI-truncated with an ellipsis.
var helpSections = []helpSection{
	{"GENERAL", [][2]string{
		{"F1", "toggle this help"},
		{"esc", "back / close (or save/load/settings/quit menu on home)"},
		{"F5 / F9", "quicksave / quickload"},
		{"q", "quit (confirm + autosave)"},
		{"ctrl+c", "quit immediately"},
	}},
	{"CAMERA & VIEW", [][2]string{
		{"f / F", "cycle camera focus forward / back (system → bodies → craft)"},
		{"g", "reset camera to the whole system"},
		{"+ / -", "zoom in / out"},
		{"v", "cycle view (tilted / top / right / bottom / left / flat / launch)"},
		{"shift+↑ / ↓", "tilt the 3D view up / down (tilted view only)"},
		{"{ / }", "yaw the 3D view left / right, wraps 360° (tilted view only)"},
		{"F2", "declutter — hide chips + navball (core column stays)"},
	}},
	{"NAVIGATION", [][2]string{
		{"→ / l", "move the body cursor next (info / porkchop — not [t] target)"},
		{"← / h", "move the body cursor previous"},
		{"tab", "switch star system"},
		{"i", "body info screen"},
	}},
	{"TIME & WARP", [][2]string{
		{".", "warp up (1× … 100000×)"},
		{",", "warp down"},
		{"G", "auto-warp to 30 s before the next burn, then 1×"},
		{"/", "cancel warp — drop to 1× (also cancels auto-warp)"},
		{"0", "pause / resume"},
	}},
	{"PLAN BURNS", [][2]string{
		{"m", "open the maneuver planner"},
		{"enter", "commit the burn (in planner)"},
		{"ctrl+d", "delete the node being edited (in planner)"},
		{"c / C", "clear ALL planned nodes for the active craft (in planner)"},
		{"H", "plant transfer to [t] target body (plane-aware)"},
		{"I", "plant inclination match ([t] target / equatorial)"},
		{"C", "plant circularize burn at next apoapsis"},
		{"K", "plant rendezvous nudge to the target craft"},
		{"R", "refine plan (re-Lambert the arrival)"},
		{"P", "porkchop plot for the body under the cursor"},
		{"o", "porkchop: transfer options (nRev / direction / branch)"},
		{"n / r / b", "porkchop options: cycle nRev / retrograde / short-vs-long"},
		{"click cell", "porkchop: select a (dep, tof) cell — enter plants it"},
	}},
	{"MANUAL FLIGHT", [][2]string{
		{"z / x", "throttle full / cut"},
		{"Z / X", "throttle +10% / -10%"},
		{"w / s", "attitude prograde / retrograde (rcs: pulse-fire)"},
		{"a / d", "attitude normal+ / normal- (rcs: pulse-fire)"},
		{"q / e", "attitude radial+ / radial- (rcs: pulse-fire)"},
		{"W / S", "attitude surface prograde / retrograde (locks to ground)"},
		{"< / >", "pitch trim ±5° east off the active mode"},
		{"?", "reset pitch trim to 0"},
		{"b", "engage / cut the manual burn (main engine)"},
		{"r", "engine: main / rcs"},
		{"k", "SAS model: slew / instant"},
		{";", "NavMode cycle: Orbit → Surface → Target"},
	}},
	{"CRAFT", [][2]string{
		{"n", "open spawn form (loadout / position / parent / altitude / dir)"},
		{"[ / ]", "cycle active craft"},
		{"1-9", "jump to craft N (no-op when the slot is empty)"},
		{"U", "undock the active composite"},
		{"D", "transpose: SM → firing core, LM → releasable nose payload"},
		{"t / T", "cycle / clear the target"},
		{"space", "decouple bottom stage (bare chute capsule: arm the chute)"},
	}},
	{"MOUSE", [][2]string{
		{"click body", "focus that body"},
		{"click vessel", "focus that craft"},
		{"click node", "open the planner for that node (canvas glyph or NODES row)"},
		{"click empty", "open the planner at the projected orbit point"},
		{"click HUD", "open body info"},
		{"[»Burn]", "toggle auto-warp to the next burn (same as G); [■Burn] while running, dimmed when none planned"},
	}},
}

// bodyLines builds the scrollable section content (everything between the
// sticky title and footer), one terminal row per slice element.
func (h *Help) bodyLines() []string {
	var lines []string
	for si, s := range helpSections {
		if si > 0 {
			lines = append(lines, "") // blank gap between sections
		}
		lines = append(lines, h.theme.Primary.Render(s.header))
		for _, r := range s.rows {
			pad := strings.Repeat(" ", maxInt(0, 20-len([]rune(r[0]))))
			lines = append(lines, "  "+h.theme.Primary.Render(r[0])+pad+r[1])
		}
	}
	return lines
}

// Render windows the body to the terminal height between a sticky title
// and footer, and truncates each row to width. Clamps + caches the scroll
// geometry so HandleKey paging stays in range.
func (h *Help) Render(width, height int) string {
	title := h.theme.Title.Render("terminal-space-program — keybindings")
	body := h.bodyLines()

	const topChrome = 2 // title + blank line
	const botChrome = 1 // footer
	viewH := height - topChrome - botChrome
	if viewH < 1 {
		viewH = 1
	}
	maxScroll := len(body) - viewH
	if maxScroll < 0 {
		maxScroll = 0
	}
	h.viewH, h.maxScroll = viewH, maxScroll
	h.clamp()

	end := h.scroll + viewH
	if end > len(body) {
		end = len(body)
	}
	window := body[h.scroll:end]

	var b strings.Builder
	b.WriteString(clipLine(title, width))
	b.WriteString("\n\n")
	for _, ln := range window {
		b.WriteString(clipLine(ln, width))
		b.WriteByte('\n')
	}
	// Pad so the footer sits on the bottom row even when the content is
	// shorter than the viewport (short terminals, last page).
	for i := len(window); i < viewH; i++ {
		b.WriteByte('\n')
	}
	b.WriteString(clipLine(h.footer(), width))
	return b.String()
}

// footer is the sticky bottom row: ▲/▼ markers when more content sits
// above / below, plus the scroll + close controls.
func (h *Help) footer() string {
	marker := "   "
	switch {
	case h.scroll > 0 && h.scroll < h.maxScroll:
		marker = "▲▼ "
	case h.scroll > 0:
		marker = "▲  "
	case h.scroll < h.maxScroll:
		marker = "▼  "
	}
	return h.theme.Footer.Render(marker + "[↑/↓ PgUp/PgDn] scroll   [F1/esc] close")
}

// HandleKey scrolls the body. Called by the app while the help screen is
// active; F1/esc closing is handled by the app, not here.
func (h *Help) HandleKey(msg tea.KeyMsg) {
	switch msg.String() {
	case "up", "k":
		h.ScrollBy(-1)
	case "down", "j":
		h.ScrollBy(1)
	case "pgup", "b":
		h.Page(-1)
	case "pgdown", " ":
		h.Page(1)
	case "home", "g":
		h.scroll = 0
	case "end", "G":
		h.scroll = h.maxScroll
	}
	h.clamp()
}

// ScrollBy moves the window by n rows (clamped). ResetScroll returns to
// the top, called when the overlay opens.
func (h *Help) ScrollBy(n int) { h.scroll += n; h.clamp() }
func (h *Help) ResetScroll()   { h.scroll = 0 }

// Page moves a near-full viewport in dir (±1), overlapping one row.
func (h *Help) Page(dir int) { h.ScrollBy(dir * maxInt(1, h.viewH-1)) }

func (h *Help) clamp() {
	if h.scroll > h.maxScroll {
		h.scroll = h.maxScroll
	}
	if h.scroll < 0 {
		h.scroll = 0
	}
}

// clipLine truncates a (possibly ANSI-styled) line to width display
// cells, appending an ellipsis only when it actually cuts.
func clipLine(s string, width int) string {
	if width <= 0 {
		return s
	}
	return ansi.Truncate(s, width, "…")
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
