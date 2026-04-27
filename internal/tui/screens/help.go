package screens

import (
	"strings"
)

// Help is the keybinding reference overlay. Invoked via `?` from any
// screen; `?` or `esc` returns.
type Help struct {
	theme Theme
}

func NewHelp(th Theme) *Help { return &Help{theme: th} }

func (h *Help) Render() string {
	title := h.theme.Title.Render("terminal-space-program — keybindings")

	sections := []struct {
		header string
		rows   [][2]string
	}{
		{"GLOBAL", [][2]string{
			{"q", "quit (confirm prompt)"},
			{"ctrl+c", "quit (immediate)"},
			{"?", "toggle this help"},
			{"esc", "back / close"},
		}},
		{"NAVIGATION", [][2]string{
			{"→ / l", "next body"},
			{"← / h", "previous body"},
			{"s", "next system"},
			{"i", "body info"},
			{"+ / -", "zoom in / out"},
			{"f", "next focus target"},
			{"F", "previous focus target"},
			{"g", "reset to system-wide view"},
			{"v", "cycle view (top / right / bottom / left / orbit-flat)"},
		}},
		{"TIME", [][2]string{
			{".", "warp up (1× … 100000×)"},
			{",", "warp down"},
			{"0 / space", "pause / resume"},
		}},
		{"FLIGHT", [][2]string{
			{"m", "open maneuver planner (burn now)"},
			{"enter", "commit burn"},
			{"esc (in planner)", "cancel burn"},
			{"n", "plan a node (T+5m prograde 50m/s)"},
			{"N", "clear all planned nodes"},
		}},
		{"PERSISTENCE", [][2]string{
			{"S", "save game (XDG_STATE_HOME)"},
			{"L", "load game"},
			{"q", "quit (confirm + autosave)"},
		}},
		{"MOUSE (orbit canvas)", [][2]string{
			{"click body", "focus body (same as ←/→ to land on it)"},
			{"click vessel", "focus craft"},
			{"click node", "open planner pre-loaded for that node (edit-replace)"},
			{"click empty", "open planner staged at projected orbit point"},
			{"click HUD", "open body info"},
		}},
		{"MOUSE (porkchop)", [][2]string{
			{"click cell", "select that (dep, tof) — press [enter] to plant"},
		}},
	}

	var b strings.Builder
	b.WriteString(title)
	b.WriteString("\n\n")
	for _, s := range sections {
		b.WriteString(h.theme.Primary.Render(s.header))
		b.WriteByte('\n')
		for _, r := range s.rows {
			b.WriteString("  ")
			b.WriteString(h.theme.Primary.Render(r[0]))
			b.WriteString(strings.Repeat(" ", maxInt(0, 20-len(r[0]))))
			b.WriteString(r[1])
			b.WriteByte('\n')
		}
		b.WriteByte('\n')
	}
	b.WriteString(h.theme.Footer.Render("[?] or [esc] to close"))
	return b.String()
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
