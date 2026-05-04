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
			{"esc", "back / close (or open save/load/quit menu on home)"},
			{"ctrl+c", "quit (immediate)"},
			{"?", "toggle this help"},
		}},
		{"NAVIGATION", [][2]string{
			{"→ / l", "next body"},
			{"← / h", "previous body"},
			{"tab", "next system"},
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
		{"FLIGHT (planted)", [][2]string{
			{"m", "open maneuver planner (burn now)"},
			{"enter", "commit burn"},
			{"esc (in planner)", "cancel burn"},
			{"ctrl+d (in planner)", "delete the node being edited"},
			{"ctrl+k (in planner)", "clear ALL planned nodes for active craft"},
			{"H", "plant Hohmann transfer to selected body"},
			{"I", "plant inclination match (selected body / equatorial)"},
			{"P", "porkchop plot for selected body"},
			{"R", "refine plan (re-Lambert arrival)"},
		}},
		{"MANUAL FLIGHT", [][2]string{
			{"z / x", "throttle full / cut"},
			{"Z / X", "throttle +10% / -10%"},
			{"w / s", "attitude prograde / retrograde (orient only in main; pulse-fire in rcs)"},
			{"a / d", "attitude normal+ / normal- (orient only in main; pulse-fire in rcs)"},
			{"q / e", "attitude radial+ / radial- (orient only in main; pulse-fire in rcs)"},
			{"b", "engage / cut manual burn (main engine only)"},
			{"r", "engine: main / rcs (RCS = monoprop pulse-fire on attitude keys)"},
		}},
		{"MULTI-CRAFT (v0.8.1+)", [][2]string{
			{"n", "open spawn form (loadout / position / parent body / altitude / direction)"},
			{"[ / ]", "cycle active craft (no-op when only one craft)"},
			{"U", "undock active composite (v0.8.3+)"},
		}},
		{"PERSISTENCE", [][2]string{
			{"F5", "quicksave (XDG_STATE_HOME)"},
			{"F9", "quickload"},
			{"q", "quit (confirm + autosave)"},
		}},
		{"MOUSE (orbit canvas)", [][2]string{
			{"click body", "focus body (same as ←/→ to land on it)"},
			{"click vessel", "focus craft"},
			{"click node", "open planner pre-loaded for that node (edit-replace, canvas glyph or HUD NODES row)"},
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
