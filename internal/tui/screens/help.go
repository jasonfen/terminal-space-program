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
			{"v", "cycle view (tilted / top / right / bottom / left / orbit-flat)"},
			{"shift+↑ / shift+↓", "tilt +5° / -5° (ViewTilted only, v0.10.6+)"},
		}},
		{"TIME", [][2]string{
			{".", "warp up (1× … 100000×)"},
			{",", "warp down"},
			{"0", "pause / resume"},
		}},
		{"FLIGHT (planted)", [][2]string{
			{"m", "open maneuver planner (burn now)"},
			{"enter", "commit burn"},
			{"esc (in planner)", "cancel burn"},
			{"ctrl+d (in planner)", "delete the node being edited"},
			{"c / C (in planner)", "clear ALL planned nodes for active craft (ctrl+k still works)"},
			{"H", "plant transfer to selected body (plane-aware: combined/split)"},
			{"I", "plant inclination match (selected body / equatorial)"},
			{"C", "plant circularize burn at next apoapsis"},
			{"K", "plant rendezvous nudge to target craft (recommended single-burn)"},
			{"P", "porkchop plot for selected body"},
			{"R", "refine plan (re-Lambert arrival)"},
		}},
		{"MANUAL FLIGHT", [][2]string{
			{"z / x", "throttle full / cut"},
			{"Z / X", "throttle +10% / -10%"},
			{"w / s", "attitude prograde / retrograde (orient only in main; pulse-fire in rcs)"},
			{"a / d", "attitude normal+ / normal- (orient only in main; pulse-fire in rcs)"},
			{"q / e", "attitude radial+ / radial- (orient only in main; pulse-fire in rcs)"},
			{"W / S", "attitude surface prograde / retrograde — locks to v - ω×r (v0.9.2+)"},
			{"< / >", "pitch trim ±10° east — held thrust direction tilts off the active mode (v0.9.2+)"},
			{"\\", "reset pitch trim to 0 (v0.9.2+)"},
			{"b", "engage / cut manual burn (main engine only)"},
			{"r", "engine: main / rcs (RCS = monoprop pulse-fire on attitude keys)"},
			{"k", "SAS model: slew / instant (navball [MAN]/[AUT]) (v0.10.0+)"},
			{";", "NavMode cycle: Orbit → Surface → Target (skips Target when none set) (v0.9.3+)"},
		}},
		{"MULTI-CRAFT (v0.8.1+)", [][2]string{
			{"n", "open spawn form (loadout / position / parent / altitude / dir; Custom = stack builder, v0.10.1+)"},
			{"[ / ]", "cycle active craft (no-op when only one craft)"},
			{"1-9", "jump to craft N (no-op when slot empty) (v0.12.0+)"},
			{"U", "undock active composite (v0.8.3+)"},
			{"t / T", "cycle / clear target (v0.9.0+)"},
			{"space", "decouple bottom stage (v0.9.1+); on a bare chute capsule, arms the parachute (v0.12+)"},
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
		{"PORKCHOP", [][2]string{
			{"o", "open transfer-options sub-menu (nRev / direction / branch)"},
			{"n / r / b", "(sub-menu) cycle nRev / toggle retrograde / toggle short-vs-long branch"},
			{"enter / esc / o", "(sub-menu) close — re-solves grid with the new options"},
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
