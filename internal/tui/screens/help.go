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
			{"q / ctrl+c", "quit"},
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
