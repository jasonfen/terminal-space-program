package screens

import (
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/jasonfen/terminal-space-program/internal/keylayout"
)

// Help is the keybinding reference overlay. Invoked via F1 from any
// screen; F1 or esc returns. The content is taller than most terminals,
// so the body scrolls between a sticky title and a sticky footer
// (↑/↓ PgUp/PgDn Home/End); Render windows the body to the terminal
// height and ANSI-truncates each row to width so one entry stays one row.
type Help struct {
	theme  Theme
	scroll int
	// page is -1 on the index (what F1 opens on), 0 on "Your first
	// flight", and n (1-based) on helpSections[n-1]. cursor is the index
	// row (0-based) the up/down + enter path acts on. Grill G1 Q1, #494.
	page   int
	cursor int
	// viewH / maxScroll are cached from the last Render so HandleKey can
	// page and clamp without re-deriving the layout. Zero until first
	// Render (Render runs every frame, so this self-corrects immediately).
	viewH     int
	maxScroll int
}

func NewHelp(th Theme) *Help { return &Help{theme: th, page: helpIndexPage} }

// helpIndexPage is the page value of the index itself.
const helpIndexPage = -1

type helpSection struct {
	header string
	rows   [][2]string
}

// helpSections groups every binding into logical sections (player-facing,
// so no internal version tags). Keep each description short enough to read
// at ~80 columns; longer rows are ANSI-truncated with an ellipsis.
//
// Section ORDER (grilled 2026-09-04, #425): GENERAL, PAUSE MENU,
// CAMERA & VIEW, TIME & WARP, MANUAL FLIGHT, NAVIGATION, PLAN BURNS,
// RENDEZVOUS PLANNER, VESSEL, VEHICLE ASSEMBLY (VAB), SAVES, MULTIPLAYER,
// MOUSE, READOUT GLOSSARY: puts "how do I fly" (camera, warp, manual
// flight) right after GENERAL / PAUSE MENU instead of six PgDn presses
// down behind SAVES and the 13-row MULTIPLAYER section (issue #425
// evidence). READOUT GLOSSARY sits last: a lookup appendix for the codes
// on the HUD, not a "how do I fly" section (ADR 0049 stage A3a).
var helpSections = []helpSection{
	{"GENERAL", [][2]string{
		{"F1 / ?", "toggle this help"},
		{"esc", "back / close (or save/load/build/settings/keyboard layout/help/quit menu on home)"},
		{"F5 / F9", "quicksave / quickload"},
		{"ctrl+c", "quit — asks to save first, [esc] stays"},
	}},
	// `q` quits only inside the pause menu — in flight it is radial+
	// (Keymap.AttitudeRadialOut), so it gets a menu-scoped section of its
	// own rather than a line in GENERAL (#423).
	{"PAUSE MENU (esc from the map)", [][2]string{
		{"q", "quit — asks to save first, same prompt as ctrl+c — menu only; in flight q is radial+"},
	}},
	{"CAMERA & VIEW", [][2]string{
		{"f / F", "cycle camera focus forward / back (system → bodies → vessels; exits spectate)"},
		{"g", "reset camera to the whole system"},
		{"+ / -", "zoom in / out"},
		{"v", "cycle view (Tilted / Top / Right / Bottom / Left / Orbit-flat), projections only"},
		{"V", "launch / surface view — chase-cam on your active vessel (press again to return)"},
		{"o", "proximity view — close-range picture of your target vessel (press again to return)"},
		{"↑ ↓ ← →", "pan the view — displaces the tracked center; [g] or any refocus clears it"},
		{"shift+↑ / shift+↓", "tilt the 3D view up / down (tilted view only)"},
		{"shift+← / shift+→", "yaw the 3D view left / right, wraps 360° (tilted view only)"},
		{"F2", "declutter — hide the instrument boxes + navball (ENGINE, PROPELLANT stay while an engine is lit)"},
	}},
	{"TIME & WARP", [][2]string{
		{".", "warp up (1× … 100000×; inert during a rendezvous coast)"},
		{",", "warp down (inert during a rendezvous coast — [/] cancels)"},
		{"G", "auto-warp to 30 s before the next burn, then 1× (inert during a rendezvous coast)"},
		{"y", "join a pending rendezvous warp — you fly copilot, they set the pair's warp"},
		{". / ,", "as rendezvous copilot: release toward following / brake the pair down"},
		{"/", "cancel warp — drop to 1× (also cancels auto-warp / rendezvous warp)"},
		{"0", "pause / resume"},
	}},
	{"MANUAL FLIGHT", [][2]string{
		{"z / x", "throttle full / cut"},
		{"Z / X", "throttle +10% / -10%"},
		{"w / s", "attitude prograde / retrograde (rcs: pulse-fire)"},
		{"a / d", "attitude normal+ / normal- (rcs: pulse-fire)"},
		{"q / e", "attitude radial+ / radial- (rcs: pulse-fire)"},
		{"W / S", "attitude surface prograde / retrograde (locks to ground)"},
		{"< / >", "pitch trim 5° west / east off the active mode"},
		{"{ / }", "heading trim 5° toward north / south off due east, on the pad or mid-ascent"},
		{"|", "reset pitch trim and heading trim to 0"},
		{"b", "engage / cut the manual burn (main engine)"},
		{"r", "engine: main / rcs"},
		{"p", "rcs pulse step: 0.1 / 0.01 / 0.001 m/s (fine trim)"},
		{"k", "SAS model: slew / instant"},
		{";", "NavMode cycle: Orbit → Surface → Target (skips Target when none is set)"},
	}},
	{"NAVIGATION", [][2]string{
		{"l", "move the body cursor next (info only, not [t] target)"},
		{"h", "move the body cursor previous"},
		{"tab", "switch star system"},
		{"i", "body info screen"},
		{"j", "inspect — step a name highlight through what's on the map"},
		{"enter", "inspect: make the highlighted thing your target (esc exits)"},
		{"M", "missions ladder (program / objective progress)"},
		{"O", "session roster (multiplayer: players, Δt, invites, sync-to)"},
	}},
	{"PLAN BURNS", [][2]string{
		{"m", "open the maneuver planner"},
		{"↑ / ↓", "planner: move the Plan Cursor through PLANNED NODES"},
		{"enter", "planner: cursor on an unloaded node loads it for editing; otherwise commits the form"},
		{"ctrl+d", "planner: delete the node under the Plan Cursor"},
		{"ctrl+k", "planner: clear ALL planned nodes for the active vessel"},
		{"c", "planner: refuses — clear all is ctrl+k"},
		{"H", "plant transfer to [t] target body (plane-aware); also works inside the planner (QUICK PLANS)"},
		{"I", "plant plane match ([t] target body / vessel / equatorial); also works inside the planner"},
		{"C", "plant circularize burn at next apoapsis; also works inside the planner"},
		{"K", "close on target vessel: plant nudge, or open the Rendezvous Planner if too far in phase; also works inside the planner"},
		{"R", "refine plan (re-Lambert the arrival); also works inside the planner"},
		{"P", "porkchop plot to your TARGET planet; also works inside the planner"},
		{"o", "porkchop: transfer options (nRev / direction / branch)"},
		{"n / r / b", "porkchop options: cycle nRev / retrograde / short-vs-long"},
		{"click cell", "porkchop: select a (dep, tof) cell — enter plants it"},
	}},
	{"RENDEZVOUS PLANNER (map chip, opens from K)", [][2]string{
		{"← / →", "walk the Rendezvous: their orbit / your orbit / the crossing"},
		{"↑ / ↓", "walk the Lap Ladder"},
		{"enter", "plant the highlighted row's burn"},
		{"esc", "close without planting"},
	}},
	{"VESSEL", [][2]string{
		{"n", "open spawn form (loadout / position / parent / altitude / dir)"},
		{"f", "spawn form: toggle scale-class system filter (show all ↔ filter to this system)"},
		{"[ / ]", "cycle active vessel"},
		{"1-9", "jump to vessel N (no-op when the slot is empty)"},
		{"U", "undock the active composite (cross-player stack: release your partner's vessel — theirs comes back even if they're offline)"},
		{"c", "re-arm docking — clear the local re-arm latch for the [t] target (or every latch naming the active vessel) so a just-undocked pair can dock again"},
		{"Y", "deploy the top carried payload (keep flying the carrier)"},
		{"D", "transpose: SM → firing core, LM → releasable nose payload"},
		{"t / T", "cycle / clear the target"},
		{"space", "decouple bottom stage (bare chute capsule: arm the chute)"},
		{"E", "end flight — clear a crashed vessel from the slate (y/n confirm)"},
	}},
	{"VEHICLE ASSEMBLY (VAB)", [][2]string{
		{"esc → b", "open the VAB from the pause menu (Build)"},
		{"tab", "switch active column (palette ↔ vehicle) — the only column switch"},
		{"← / →", "swap the selected vehicle row's part within its kind (engine leads chemistry)"},
		{"↑ / ↓", "move cursor in the active column"},
		{"PgUp / PgDn", "jump to next/prev kind section (palette) or stage (vehicle)"},
		{"a", "add the selected component to the current stage (or part as a new stage)"},
		{"n / x", "new empty stage on top / remove component group or stage under cursor"},
		{"+ / -", "increase / decrease count of the component group under the cursor"},
		{"[ / ]", "move the cursor's stage down / up in the stack (reorder)"},
		{"y", "duplicate the stage under the cursor"},
		{"enter", "crack an atomic catalog part into its editable seed components"},
		{"d", "toggle dock seam below the stage (nose payload, [U]ndock-released)"},
		{"c", "toggle fused decouple (stage drops with the group below)"},
		{"t", "set a Σ Δv target (a tank row then hints the count to reach it)"},
		{"s / o", "save the design (name it) / open a saved design"},
	}},
	{"MISSIONS (ladder screen — open with M)", [][2]string{
		{"1", "turn Flight School on / off (the row under its list says which)"},
		{"2", "turn the Challenge ladder on / off"},
	}},
	{"SAVES (menu → Save / Load Game)", [][2]string{
		{"↑ / ↓", "move the save cursor"},
		{"enter", "load-mode: load (confirms) · save-mode: new save / overwrite"},
		{"d", "delete the highlighted save (confirms)"},
		{"r", "rename a named save (quicksave / autosaves can't be renamed)"},
		{"esc", "back to the map"},
	}},
	{"MULTIPLAYER (session screen — open with O)", [][2]string{
		{"~", "chat (flight view): type + enter broadcasts; @handle DMs, tab completes; esc bails"},
		{"t", "target their ghost vessel — 2+ vessels opens a picker ([esc] backs out)"},
		{"v", "spectate — fit + camera-follow their ghost's orbit ([f] to return)"},
		{"s", "sync-warp forward to a player ahead of you (forward only)"},
		{"w", "rendezvous warp — agree to rendezvous with them; commits to an encounter when one's found, otherwise arms with no plan yet"},
		{"RANGE column", "live distance to them; warps lock inside 35 km at under 100 m/s closing"},
		{"h", "start / stop hosting — accept ssh guests (stop confirms, drops guests)"},
		{"i / r / x", "host + admins: mint invite / revoke code / remove player"},
		{"p", "host only: promote the selected player to admin / demote them"},
		{"F4", "host + admins: restart the server (drains guests, they reconnect)"},
		{"J", "hand a cross-player stack to the guest (refused if they aren't in the session); from the guest seat, take the stick back when the pilot has gone (map)"},
	}},
	{"MOUSE", [][2]string{
		{"click body", "focus that body"},
		{"click vessel", "focus that vessel"},
		{"click node", "open the planner for that node (canvas glyph or NODES row)"},
		{"click line", "inspect — name whose orbit that is (same highlight as [j])"},
		{"click empty", "open the planner at the projected orbit point"},
		{"click HUD", "open body info"},
		{"[»Burn]", "toggle auto-warp to the next burn (same as G, inert during a rendezvous coast); [■Burn] while running, dimmed when none planned"},
	}},
	// ADR 0049 (Readout Contract) decision 6: labels are one short word,
	// but five codes survive on the HUD (CA:, e:, τ, the AN/DN angle,
	// rcs; corrected by the ADR 0050 audit from an earlier "six" here),
	// so each gets a line here rather than being spelled out on every
	// chip. Order matches the ADR and CONTEXT.md's Readout Contract
	// entry.
	//
	// ADR 0051 slice 2b adds eight more entries, one per new symbol or
	// word the eight instrument boxes introduced: the Ap trend arrow
	// and the → "becomes" rule share one line (re-grill Q3), the node
	// row's ⚠ (re-grill Q4), plan/dir/speed (decisions 15/14/13d), the
	// (max N) full-throttle wording (decision 13b/C5), and ORBIT
	// READY's cue (decision 10, re-grill Q7: no floor number, by
	// rule).
	//
	// #478 adds two more and replaces one: `dir` retires (NAVIGATION's
	// dir: cell is gone, A3) in favour of `pro / retro`, the tag that
	// replaced it on incl:; `(ORBIT) / (SURF) / (TGT)` documents the one
	// spelling every frame uses everywhere it's named (hold:'s tag,
	// nav:, the navball [MODE] button); and `rcs` finally gets the line
	// the decision-6 comment above always meant it to have (it survived
	// the ADR 0049 rename table but was never actually added here).
	{"READOUT GLOSSARY", [][2]string{
		{"fpa", "flight path angle: velocity above / below the local horizontal"},
		{"Q", "dynamic pressure: aerodynamic stress on the airframe, in kPa"},
		{"TCA", "time of closest approach, in a rendezvous or flyby"},
		{"TWR", "thrust to weight ratio: engines' thrust over vessel weight"},
		{"Ap / Pe", "apoapsis / periapsis: height above the surface, like every altitude in the game"},
		{"Δv / Δincl", "delta-v / relative inclination to a target's orbital plane"},
		{"T- / T+", "countdown convention: T- counts down to an event, T+ counts up since it"},
		{"incl (min N°)", "on the pad: the inclination your commanded heading yields, and the Inclination Floor (|launch latitude|) it can't go below"},
		{"depart", "the angle between the orbit you'd reach and the plane the world beneath you travels in: the plane you leave along"},
		{"Ap ↑ / ↓", "apoapsis trend: climbing / falling; no glyph while it's steady"},
		{"→", "becomes: between a current value and a planned or resulting one, wherever it appears"},
		{"⚠ (node)", "on a planned burn: it exceeds the stage's Δv budget"},
		{"plan", "the world the planned burn's numbers are measured from, plus its node angles"},
		{"pro / retro", "prograde or retrograde: which way the orbit runs, tagged on incl:"},
		{"(ORBIT/SURF/TGT)", "the frame a held direction or nav: reads in: orbit, surface, or target-relative — one spelling everywhere it's named"},
		{"rcs", "monoprop's own Δv, from the RCS thrusters rather than the main engine"},
		{"speed", "inertial speed, alongside vert: and horiz:"},
		{"(max N)", "the same figure at full throttle, when it differs from the current one"},
		{"● ORBIT READY [C]", "apoapsis has cleared this world's orbit floor: press [C] to plant the circularising burn"},
	}},
}

// firstFlight is the "Your first flight" page (grill G1 Q2, #494): the
// flight the game starts you in (the orbit start) first, then the pad, in
// Flight School's own order, each line naming the moment it applies. The
// left column is a key so it is Display-translated to the active layout
// like every other row. Authored outside helpSections on purpose: it
// restates keys already covered there, so the keymap coverage test has
// nothing to learn from it.
var firstFlight = []helpSection{
	{"ORBIT START (you begin in a 500 km orbit around Earth)", [][2]string{
		{"v", "to change your view of the map, cycle the camera"},
		{"t", "tap until TARGET reads Moon ([T] clears it)"},
		{"H", "with the Moon targeted, plant a transfer: two burn markers appear"},
		{"G", "once a burn is planted, warp to 30 s before it; the burn fires itself"},
		{"b", "to fly the burn by hand instead, light the engine until you pass 700 km"},
	}},
	{"THE PAD (a Saturn V on the launchpad)", [][2]string{
		{"n", "open the spawn form: pick Saturn V, position launchpad"},
		{"z", "on the pad, throttle to full"},
		{"b", "light the engine and lift off ([space] only drops stages)"},
		{"W", "above 10 km, point along your motion over the ground and hold it through the turn"},
		{"space", "when the first stage runs dry, drop it and keep flying"},
		{"C", "at ● ORBIT READY, plant the circularising burn; it fires itself"},
	}},
}

const firstFlightTitle = "Your first flight"
const firstFlightWhen = "you are new and want one flight, start to finish"
const firstFlightEnd = "F1 → MANUAL FLIGHT for the rest"

// helpWhen is each section's "when you would open this" clause, shown on
// the index. Keyed by helpSections header; a test pins that every section
// has one.
var helpWhen = map[string]string{
	"GENERAL":            "you want to close this, quit, or quicksave",
	"PAUSE MENU":         "you pressed esc on the map and want to save, load or quit",
	"CAMERA & VIEW":      "the map is too crowded, too far, or from the wrong angle",
	"TIME & WARP":        "you are waiting on a burn or an orbit",
	"MANUAL FLIGHT":      "you are flying by hand: throttle, engine, pointing the nose",
	"NAVIGATION":         "you want info on a body, a target, or your mission progress",
	"PLAN BURNS":         "you are planning a transfer, a circularisation or a node",
	"RENDEZVOUS PLANNER": "you are closing on another vessel and the planner opened",
	"VESSEL":             "you are spawning, switching, staging, docking or undocking",
	"VEHICLE ASSEMBLY":   "you are building your own rocket",
	"MISSIONS":           "you want Flight School or the Challenge ladder on or off",
	"SAVES":              "you are saving, loading, renaming or deleting a save",
	"MULTIPLAYER":        "you are flying with friends: chat, ghosts, sync, hosting",
	"MOUSE":              "you would rather click than type",
	"READOUT GLOSSARY":   "a code on the HUD (fpa, TCA, e:, τ ...) means nothing to you",
}

// helpIndexTitle is a section header without its parenthetical scope note,
// which the when-clause carries instead.
func helpIndexTitle(header string) string {
	if i := strings.Index(header, " ("); i >= 0 {
		return header[:i]
	}
	return header
}

func helpWhenFor(header string) string { return helpWhen[helpIndexTitle(header)] }

// helpPageCount is the index length: the first-flight page plus every
// section.
func helpPageCount() int { return len(helpSections) + 1 }

// pageTitle names page n (0 = first flight, k = helpSections[k-1]).
func pageTitle(n int) string {
	if n == 0 {
		return firstFlightTitle
	}
	return helpIndexTitle(helpSections[n-1].header)
}

func (h *Help) renderRows(layout keylayout.Layout, rows [][2]string) []string {
	var lines []string
	for _, r := range rows {
		token := keylayout.DisplayToken(layout, r[0])
		pad := strings.Repeat(" ", maxInt(0, 20-len([]rune(token))))
		lines = append(lines, "  "+h.theme.Primary.Render(token)+pad+r[1])
	}
	return lines
}

func (h *Help) sectionLines(layout keylayout.Layout, s helpSection) []string {
	lines := []string{h.theme.Primary.Render(s.header)}
	return append(lines, h.renderRows(layout, s.rows)...)
}

// bodyLines builds the scrollable content (everything between the sticky
// title and footer), one terminal row per slice element: the index, or the
// current page. Key tokens (the left column) are Display-translated to the
// active layout so a QWERTZ player's keycaps match the overlay (ADR 0022);
// descriptions are left untouched so prose like "zoom in" keeps its letters.
func (h *Help) bodyLines(layout keylayout.Layout) []string {
	if h.page == helpIndexPage {
		return h.indexLines()
	}
	if h.page == 0 {
		var lines []string
		for i, s := range firstFlight {
			if i > 0 {
				lines = append(lines, "")
			}
			lines = append(lines, h.sectionLines(layout, s)...)
		}
		return append(lines, "", "  "+h.theme.Footer.Render(firstFlightEnd))
	}
	return h.sectionLines(layout, helpSections[h.page-1])
}

// indexLines is the index: a numbered row per page with its when-clause.
// Digits 1-9 jump straight to a page; every row is also reachable with
// up/down + enter (there are more pages than digit keys).
func (h *Help) indexLines() []string {
	lines := []string{h.theme.Primary.Render("WHERE TO START"), ""}
	for n := 0; n < helpPageCount(); n++ {
		when := firstFlightWhen
		if n > 0 {
			when = helpWhenFor(helpSections[n-1].header)
		}
		title := pageTitle(n)
		pad := strings.Repeat(" ", maxInt(1, 22-lipgloss.Width(title)))
		mark := "  "
		if n == h.cursor {
			mark = "> "
		}
		num := strconv.Itoa(n + 1)
		numPad := strings.Repeat(" ", 3-len(num))
		lines = append(lines, mark+h.theme.Primary.Render(num)+numPad+title+pad+when)
	}
	return lines
}

// Render windows the body to the terminal height between a sticky title
// and footer, and truncates each row to width. Clamps + caches the scroll
// geometry so HandleKey paging stays in range.
func (h *Help) Render(width, height int, layout keylayout.Layout) string {
	title := h.theme.Title.Render("terminal-space-program — keybindings")
	body := h.bodyLines(layout)

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

// PositionLine is the footer's "where am I" text: the page name and its
// place among the pages (grill G1 Q1: replaces the bare ▼ cue).
func (h *Help) PositionLine() string {
	if h.page == helpIndexPage {
		return "INDEX · " + strconv.Itoa(helpPageCount()) + " pages"
	}
	return pageTitle(h.page) + " · " + strconv.Itoa(h.page+1) + " of " + strconv.Itoa(helpPageCount())
}

// footer is the sticky bottom row: the position line, ▲/▼ when a long page
// has rows above / below, plus the controls for where you are.
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
	keys := "[1-9] jump   [↑/↓ enter] pick   [F1/esc] close"
	if h.page != helpIndexPage {
		keys = "[↑/↓ PgUp/PgDn] scroll  [esc] index  [F1] close"
	}
	// #498: the position line and cue are the only signs of where you are,
	// so they get Primary rather than the dim Footer gray.
	return h.theme.Primary.Render(marker) + h.theme.Primary.Render(h.PositionLine()) + "   " + h.theme.Footer.Render(keys)
}

// OpenPage jumps to page n (0 = first flight, k = helpSections[k-1]); out
// of range is ignored.
func (h *Help) OpenPage(n int) {
	if n < 0 || n >= helpPageCount() {
		return
	}
	h.page, h.cursor, h.scroll = n, n, 0
}

// Back returns from a page to the index and reports true; on the index it
// reports false so the caller closes the overlay.
func (h *Help) Back() bool {
	if h.page == helpIndexPage {
		return false
	}
	h.page, h.scroll = helpIndexPage, 0
	return true
}

// HandleKey scrolls the body, jumps pages and moves the index cursor.
// Called by the app while the help screen is active; F1/esc closing is
// handled by the app (via Back), not here.
func (h *Help) HandleKey(msg tea.KeyMsg) {
	k := msg.String()
	if len(k) == 1 && k[0] >= '1' && k[0] <= '9' {
		h.OpenPage(int(k[0] - '1'))
		return
	}
	if h.page == helpIndexPage {
		switch k {
		case "up", "k":
			h.cursor = maxInt(0, h.cursor-1)
		case "down", "j":
			h.cursor = minInt(helpPageCount()-1, h.cursor+1)
		case "home", "g":
			h.cursor = 0
		case "end", "G":
			h.cursor = helpPageCount() - 1
		case "enter":
			h.OpenPage(h.cursor)
		}
		return
	}
	switch k {
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
// the index, called when the overlay opens.
func (h *Help) ScrollBy(n int) { h.scroll += n; h.clamp() }
func (h *Help) ResetScroll()   { h.scroll, h.page, h.cursor = 0, helpIndexPage, 0 }

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

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// helpTokenConnectors are the words-between-keys used in a row's left
// column ("z / x", "esc → b"). HelpKeyTokens splits on these rather than
// on bare whitespace so a connector is never mistaken for a key, while the
// pan row ("↑ ↓ ← →") still yields four separate arrow tokens.
var helpTokenConnectors = []string{" / ", " → "}

// HelpKeyTokens returns every key token the overlay names in its left
// column, split out of the compound rows ("z / x" → "z", "x"; "↑ ↓ ← →" →
// four arrows). The keymap-coverage test in package tui reads it to prove
// the overlay still names every binding in DefaultKeymap, so helpSections
// itself can stay unexported. Tokens are the QWERTY spellings authored in
// helpSections, before any keylayout.DisplayToken translation.
func HelpKeyTokens() map[string]bool {
	tokens := map[string]bool{}
	for _, s := range helpSections {
		for _, r := range s.rows {
			for _, tok := range splitHelpToken(r[0]) {
				tokens[tok] = true
			}
		}
	}
	return tokens
}

// splitHelpToken breaks one left-column cell into the individual keys it
// names. A cell that is itself a key ("/") is returned whole.
func splitHelpToken(cell string) []string {
	cell = strings.TrimSpace(cell)
	if cell == "" {
		return nil
	}
	pieces := []string{cell}
	for _, sep := range helpTokenConnectors {
		var next []string
		for _, p := range pieces {
			next = append(next, strings.Split(p, sep)...)
		}
		pieces = next
	}
	var out []string
	for _, p := range pieces {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		// A space-separated run of short glyphs is a key list ("↑ ↓ ← →");
		// anything with a word in it is one label ("click body").
		if fields := strings.Fields(p); len(fields) > 1 && allShort(fields) {
			out = append(out, fields...)
			continue
		}
		out = append(out, p)
	}
	return out
}

func allShort(fields []string) bool {
	for _, f := range fields {
		if len([]rune(f)) > 3 {
			return false
		}
	}
	return true
}
