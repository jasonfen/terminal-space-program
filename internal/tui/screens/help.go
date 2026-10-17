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
	// direct: opened by F1 from a form onto that form's own page (GK Q3).
	// esc on such a page leaves help for the form at once instead of
	// stepping to the index first, so "F1 or esc puts you back" is true.
	// A digit jump (1-9) drops it: esc then steps to the index (#629 L4).
	direct bool
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
		{"F1 / ?", "help, opened on the page for the screen you are on; F1 or esc puts you back exactly where you were"},
		{"F1 (in a box)", "inside a name or number box only F1 opens help (? is a typed character there); what you typed is kept"},
		{"enter (any screen)", "does the highlighted or typed thing (each page says what); on the map nothing, except while [j] inspects"},
		{"esc", "back to the screen that opened you: a screen opened from the pause menu returns to the menu, one opened by a key returns to the map (esc on the map opens the pause menu)"},
		{"F5 / F9", "quicksave / quickload (F9 asks first: a card, enter picks the highlighted row)"},
		{"ctrl+c", "quit: asks to save first, [esc] stays"},
		{"a card", "a question over the dimmed map: ↑ / ↓ move the ▸, enter picks that row, y picks the first row, n or esc is the way out; an action row asks once more on the same card before it acts, and the way out acts at once"},
	}},
	// `q` quits only inside the pause menu; in flight it is radial+
	// (Keymap.AttitudeRadialOut), so it gets a menu-scoped section of its
	// own rather than a line in GENERAL (#423).
	{"PAUSE MENU (esc from the map)", [][2]string{
		{"esc", "fly again: the menu stops the clock while it is open (the flight bar reads PAUSED) and closing it gives the clock back as it was; esc again from a screen the menu opened returns here"},
		{"↑ / ↓", "pick a row (it is highlighted); [enter] opens it"},
		{"s l b t k h", "shortcuts beside each row: save / load / build (VAB) / settings / keyboard layout / help"},
		{"q", "quit: asks to save first, same prompt as ctrl+c (menu only; in flight q is radial+)"},
		{"in Settings", "↑ / ↓ pick a row; space or enter flips or cycles the highlighted setting; esc back to the menu"},
		{"in Keyboard layout", "← / →, space or enter cycle the layout (QWERTY / QWERTZ moves letters only); esc back to the menu"},
	}},
	{"CAMERA & VIEW", [][2]string{
		{"f / F", "cycle camera focus forward / back (system → bodies → vessels; exits spectate)"},
		{"g", "snap a pan back onto what you're following (keeps focus and zoom)"},
		{"alt+g", "reset camera to the whole system"},
		{"+ / -", "zoom in / out"},
		{"v", "cycle view (Tilted / Top / Right / Bottom / Left / Orbit-flat), projections only; two rows above \"view:\" read which way north points (N ↑ up, N ⊙ toward you, N ⊗ away; a degree figure, N ↑ 42°, is how far north tilts toward or away from you, 0° flat on the screen) and how open your orbit ring looks (plane ◠ 67° open: 0° ─ is a flat line, 90° ○ a full circle)"},
		{"V", "launch / surface view: chase-cam on your active vessel (press again to return)"},
		{"o", "proximity view: side picture left, Centerline (out your port) right; port line, corridor, misalign / lateral rows"},
		{"shift+← / shift+→", "pan the view left / right (displaces the tracked center; [g] or any refocus clears it)"},
		{"shift+↑ / shift+↓", "pan the view up / down (plain arrows are the flight trims, see MANUAL FLIGHT)"},
		{"> / <", "tilt the 3D view up / down (tilted view only; moves the camera, never the vessel)"},
		{"{ / }", "yaw the 3D view left / right, wraps 360° (tilted view only)"},
		{"F2", "declutter: hide the instrument boxes + navball (ENGINE, PROPELLANT stay while an engine is lit)"},
	}},
	{"TIME & WARP", [][2]string{
		{".", "warp up (1× … 100000×; inert during a rendezvous coast; held to 10× within 50 m of another vessel)"},
		{",", "warp down (inert during a rendezvous coast; [/] cancels)"},
		{"G", "auto-warp to 30 s before the next burn (on the pad: the launch window, and sets the heading it wants; inside the last 30 s it only turns the heading), then 1× (inert during a rendezvous coast)"},
		{"y", "join a pending rendezvous warp: you fly copilot, they set the pair's warp"},
		{". / ,", "as rendezvous copilot: release toward following / brake the pair down"},
		{"/", "cancel warp: drop to 1× (also cancels auto-warp / rendezvous warp)"},
		{"warp caps", "10× while an engine burns or within 50 m of another vessel; 1× while the dock magnet pulls"},
		{"0", "pause / resume"},
	}},
	{"MANUAL FLIGHT", [][2]string{
		{"z / x", "throttle full / cut"},
		{"Z / X", "throttle +10% / -10%"},
		{"alt+z / alt+x", "throttle +1% / -1% (fine, like the alt+arrow nose trims; alt+Z / alt+X work too)"},
		{"w / s", "attitude prograde / retrograde (rcs: pulse-fire); PORT: up / down across the port"},
		{"a / d", "attitude normal+ / normal- (rcs: pulse-fire); PORT: left / right across the port"},
		{"q / e", "attitude radial+ / radial- (rcs: pulse-fire); PORT: in / out along the port (main engine: q holds Port+)"},
		{"W / S", "attitude surface prograde / retrograde (locks to ground)"},
		{"← / →", "pitch trim 5° back from / toward your heading (hold to ramp); in PORT 1° left / right across the port face"},
		{"↑ / ↓", "heading trim 5° toward north / south off due east (hold to ramp); in PORT 1° up / down across the port face"},
		{"alt+← / alt+→", "pitch trim 1° back from / toward your heading (fine: alt makes any trim fine); in PORT 0.25°"},
		{"alt+b / alt+f", "the same fine pitch trim: Terminal.app and Ghostty send Option+← / → as these"},
		{"alt+↑ / alt+↓", "heading trim 1° toward north / south (fine); name the pad window's exact heading; in PORT 0.25°"},
		{"alt", "the Alt key on Windows and Linux (WezTerm and most terminals send it as-is); Option on a Mac, see below"},
		{"alt on a Mac", "Ghostty works as-is; Terminal.app: Profiles > Keyboard > Use Option as Meta key; iTerm2: Left Option = Esc+"},
		{"|", "reset pitch trim and heading trim to 0 (in PORT, the port-face trims)"},
		{"b", "engage / cut the manual burn (main engine)"},
		{"r", "engine: main / rcs"},
		{"p", "rcs pulse step: 0.1 / 0.01 / 0.001 m/s (fine trim)"},
		{"k", "SAS model: slew / instant"},
		{";", "NavMode cycle: Surface → Orbit → Target → Port (skips Target with no vessel targeted, Port without its port)"},
		{"; (self-switch)", "Surface → Orbit climbing through the upper atmosphere (Earth 51 km); Orbit → Surface coming back down. ; overrides"},
		{"; (to a target)", "Target and Port are chosen by hand only; they never switch themselves"},
	}},
	{"NAVIGATION", [][2]string{
		{"l", "move the body cursor next (info only, not [t] target)"},
		{"h", "move the body cursor previous"},
		{"tab", "switch star system"},
		{"i", "body info screen (there [t] targets the shown body, then [H] / [P] plan to it)"},
		{"j", "inspect: step a name highlight through what's on the map"},
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
		{"c", "planner: refuses, clear all is ctrl+k"},
		{"H", "plant transfer to [t] target (plane-aware); refuses if a burn is planted (clear in [m]) or for another planet's moon"},
		{"I", "plant plane match ([t] target body / vessel / equatorial); also works inside the planner"},
		{"C", "plant circularize burn at next apoapsis; also works inside the planner"},
		{"K", "close on target vessel: plant nudge, or open the Rendezvous Planner if too far in phase; also works inside the planner"},
		{"R", "refine plan (re-Lambert the arrival); also works inside the planner"},
		{"P", "porkchop plot to your TARGET planet; also works inside the planner"},
		{"o", "porkchop: transfer options (nRev / direction / branch)"},
		{"n / r / b", "porkchop options: cycle nRev / retrograde / short-vs-long"},
		{"click cell", "porkchop: select a (dep, tof) cell, enter plants it"},
	}},
	{"RENDEZVOUS PLANNER (map chip, opens from K)", [][2]string{
		{"← / →", "walk the Rendezvous: their orbit / your orbit / the crossing"},
		{"↑ / ↓", "walk the Lap Ladder"},
		{"enter", "plant the highlighted row's burn"},
		{"K", "re-solve every row from now (once a row's burn time has passed)"},
		{"esc", "close without planting"},
	}},
	{"VESSEL", [][2]string{
		{"n", "open spawn form (loadout / position / inclination / parent / altitude / dir)"},
		{"f", "spawn form: toggle scale-class system filter (show all ↔ filter to this system)"},
		{"[ / ]", "cycle active vessel"},
		{"1-9", "jump to vessel N (no-op when the slot is empty)"},
		{"U", "undock the active composite: the pieces come back port to port 1.5 to 3 m apart, parting at 0.15 m/s"},
		{"U (cross-player)", "release your partner's vessel from a shared stack; theirs comes back even if they're offline"},
		{"c", "re-arm docking: clear the local re-arm latch for the [t] target (or every latch naming the active vessel) so a just-undocked pair can dock again"},
		{"Y", "deploy the top carried payload (keep flying the carrier); asks first"},
		{"D", "transpose: SM → firing core, LM → releasable nose payload; asks first"},
		{"t / T", "cycle / clear the target"},
		{"space", "decouple bottom stage, instantly, pad included (the STAGES box names it; bare chute capsule: arm the chute)"},
		{"E", "end flight: clear a crashed vessel from the slate (asks first)"},
	}},
	{"DOCKING (port to port)", [][2]string{
		{"ports", "on the nose of crewed modules, tugs, keepers, relays, stations, Probe Sat; not launchers, bare stages, comsats"},
		{"stack port", "a lone vessel's port is its top stage's; a docked pair or loaded carrier has none free until the partner leaves"},
		{"the gate", "ports within 0.5 m, under 0.1 m/s, misaligned under 4°, all at once; port line and Centerline ring go solid green"},
		{"centerline", "right half of proximity view: crosshair = your port, ring = theirs, cross = your lean; cross in ring = under 4°"},
		{"chip rows", "misalign: angle between the axes; lateral: their port across your face (↑ ↓ ◂ ▸); hold: the attitude you hold"},
		{"magnet", "a five-second pull closes the last half metre once the gate holds; warp 1×, keys ignored; leaving the gate ends it"},
		{"bump", "hulls touching outside the gate bounce apart; re-approach under 0.10 m/s; warp capped at 10× within 50 m of a hull"},
		{";", "cycle to PORT once your target has a port; nothing turns; speed: then reads (port), relative to the target"},
		{"q / e (rcs)", "in PORT with RCS armed: push in / out along the port's axis; w / s up / down, a / d left / right"},
		{"q (main engine)", "in PORT: hold your nose on the port axis, the docking attitude (hold: Port+ (PORT) on the chip); e holds it away"},
		{"arrows (PORT)", "in PORT ↑ / ↓ / ← / → tip your nose 1° up / down / left / right across the port face (alt 0.25°); | clears"},
		{"navball (PORT)", "pitch / yaw: how far your nose is above or below, right or left of, parallel to their port axis"},
		{"p", "rcs pulse step: 0.1 / 0.01 / 0.001 m/s; the last metres want 0.01 or finer"},
		{"U", "undock the active composite: the pieces come back port to port 1.5 to 3 m apart, parting at 0.15 m/s"},
		{"c", "re-arm docking: a just-undocked pair cannot re-dock until 100 m or ten minutes pass; [c] clears that now"},
	}},
	{"SPAWN FORM (n)", [][2]string{
		{"tab / shift+tab", "next / previous field"},
		{"← / →", "change the field; on ALTITUDE and INCLINATION step to the next stop"},
		{"enter", "spawn the vessel; on ALTITUDE or INCLINATION the first enter opens a box to type the number"},
		{"enter (box open)", "keep the typed number; the next enter spawns (the form says so beside the number)"},
		{"esc (box open)", "put the old number back; esc again leaves the form"},
		{"0-9 / backspace", "in the box: type / erase the number (INCLINATION also takes one decimal point)"},
		{"esc", "leave the form without spawning anything"},
		{"f", "show every system's vessels / only this system's"},
		{"a / x / d", "Custom stack: add the picked module / remove the top stage / cycle the dock seam (nose payload)"},
		{"F1", "help from anywhere on this form; esc or F1 brings you back with the field and any typed number as you left them"},
	}},
	{"VEHICLE ASSEMBLY (VAB)", [][2]string{
		{"esc → b", "open the VAB from the pause menu (Build)"},
		{"tab", "other column (palette ↔ vehicle), the only column switch"},
		{"← / →", "change part: swap the selected vehicle row's part within its kind (engine leads chemistry)"},
		{"↑ / ↓", "move cursor in the active column"},
		{"PgUp / PgDn", "jump section: next / previous tier or kind (palette) or stage (vehicle)"},
		{"/", "find: type part of a name, the palette narrows as you type; esc clears, enter keeps the filter"},
		{"a", "add a part or subassembly above the cursor's stage (a block keeps its marks); a component joins the stage"},
		{"n / x", "new empty stage on top / remove component group or stage under cursor"},
		{"+ / -", "count: raise / lower the count of the component group under the cursor"},
		{"[ / ]", "move the cursor's stage down / up in the stack"},
		{"y", "copy stage: duplicate the stage under the cursor, marks included"},
		{"enter", "open part into pieces: a whole catalog part becomes its editable parts (no undo key)"},
		{"d", "payload from here up: this stage and all above become a payload, released with [Y] (deploy one) or [U] (undock all)"},
		{"c", "drops with stage below: this stage drops together with the one below on one [space]"},
		{"t", "Δv goal: set a Σ Δv target (a tank row then hints the count to reach it)"},
		{"s / o", "save the design (name it) / open a design, a subassembly or a built-in vessel"},
		{"S", "save from here up: this stage and all above, named, as a subassembly for the palette (payload or stage)"},
		{"enter (name box)", "keep the typed name and save the design; esc cancels"},
		{"enter (Δv box)", "keep the typed Σ Δv goal; esc cancels"},
		{"enter (S card)", "keep the typed name and save the subassembly; ←/→ flips payload or stage; a taken name asks first"},
		{"enter (design list)", "load the highlighted design, subassembly or built-in vessel (a built-in opens unsaved); esc closes the list"},
		{"x (design list)", "delete a design or subassembly: a card asks twice, opening on keep it; built-in vessels can't be deleted"},
	}},
	{"MISSIONS (ladder screen, open with M)", [][2]string{
		{"1", "turn Flight School on / off (the row under its list says which)"},
		{"2", "turn the Challenge ladder on / off"},
	}},
	{"SAVES (menu → Save / Load Game)", [][2]string{
		{"↑ / ↓", "move the save cursor"},
		{"enter", "load-mode: load (a card asks first) · save-mode: new save / overwrite (a card asks first)"},
		{"enter (name box)", "keep the typed name and save; esc cancels"},
		{"d", "delete the highlighted save (a card asks first)"},
		{"a card", "opens on the way out: ↑ / ↓ + enter pick a row, y the first, n or esc the way out; an action row asks once more"},
		{"r", "rename a named save (quicksave / autosaves can't be renamed)"},
		{"esc", "back to the map"},
	}},
	{"MULTIPLAYER (session screen, open with O)", [][2]string{
		{"~", "chat (flight view): type + enter broadcasts; @handle DMs, tab completes; esc bails"},
		{"t", "target their ghost vessel; 2+ vessels opens a picker ([esc] backs out)"},
		{"v", "spectate: fit + camera-follow their ghost's orbit ([f] to return)"},
		{"s", "sync-warp forward to a player ahead of you (forward only)"},
		{"w", "rendezvous warp: agree to rendezvous with them; commits to an encounter when one's found, otherwise arms with no plan yet"},
		{"RANGE column", "live distance to them; warps lock inside 35 km at under 100 m/s closing"},
		{"h", "start / stop hosting: accept ssh guests (stop asks first and drops every guest cleanly)"},
		{"d", "host + admins: disconnect the selected player now; they reconnect at once with the same key (asks first)"},
		{"i / r / x", "host + admins: mint invite / revoke code / kick player (x asks first)"},
		{"enter (invite box)", "keep the typed handle and mint the invite; esc cancels"},
		{"x (what it does)", "kicked: dropped now, needs a new invite; vessels stay unless you pick kick and delete vessels (backed up first)"},
		{"asks first", "d disconnect, x kick, F4 restart server, h stop hosting: the roster shows them on their own amber row"},
		{"p", "host only: promote the selected player to admin / demote them"},
		{"F4", "host + admins: restart the server (warns everyone, drops them cleanly, they reconnect); asks first"},
		{"J", "hand a cross-player stack to the guest (refused if they aren't in the session); from the guest seat, take the stick back when the pilot has gone (map)"},
	}},
	{"MOUSE", [][2]string{
		{"click body", "focus that body"},
		{"click vessel", "focus that vessel"},
		{"click node", "open the planner for that node (canvas glyph or NODES row)"},
		{"click line", "inspect: name whose orbit that is (same highlight as [j])"},
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
		{"(ORBIT/SURF/TGT)", "the frame a held direction or nav: reads in: orbit, surface, target or PORT; one spelling everywhere it's named"},
		{"amber (SURF)", "GUIDANCE hold: amber frame word differs from nav:; it keeps the frame it was pressed in, on purpose"},
		{"[MAN] / [AUT]", "the navball's attitude tag: MAN turns the nose at a rate-limited slew; AUT (amber) snaps it instantly"},
		{"RCS", "navball toggle: amber when the RCS thrusters, not the main engine, are armed ([r] does the same)"},
		{"rcs", "monoprop's own Δv, from the RCS thrusters rather than the main engine"},
		{"◉ ● ○ ◇", "STAGES pips, bottom stage first: ◉ lit (burns next), ● fuelled and waiting, ○ spent, ◇ no tank (a pod or payload)"},
		{"speed", "in the navball frame, named (surface) / (orbit) / (target) / (port), alongside vert: and horiz:"},
		{"(max N)", "the same figure at full throttle, when it differs from the current one"},
		{"heading: / hdg", "GUIDANCE heading: is the bearing you have commanded; the navball's pitch and hdg are measured, where the nose points now (they agree on the pad, and differ while the nose slews). In ORBIT the ball's rungs count degrees out of the orbit plane, not above the horizon. trim: is how far ←/→ lean the nose down or up off the hold, so 15° down on a level prograde nose reads pitch -15°; → leans toward your heading, which lowers a prograde nose and raises a retrograde one"},
		{"● ORBIT READY [C]", "apoapsis has cleared this world's orbit floor: press [C] to plant the circularising burn"},
	}},
	{"MAP MARKERS", [][2]string{
		{"➤", "your vessel, in yellow (or your theme's craftmarker colour); other vessels and ghosts wear their own"},
		{"green", "the vessel you have targeted, and its orbit ([t] picks a target)"},
		{"Δ", "a planned burn, in the colour of the dashed orbit after it (blue, mint, amber, pink); click to edit"},
		{"▲", "apoapsis: the highest point of the orbit"},
		{"▼", "periapsis: the lowest point of the orbit"},
		{"◇", "ascending node: your orbit crosses your target's orbital plane going north (needs a target)"},
		{"◆", "descending node: the same crossing going south of the target's plane"},
		{"⊕", "perilune: the closest point of your pass by another body"},
		{"✕", "closest approach: one mark on your path, one on the target's, where you pass nearest"},
		{"▷", "entry: your path crosses into a body's sphere of influence"},
		{"◁", "exit: your path crosses back out of it"},
		{"⊗", "impact: where your trajectory meets the ground (surface view)"},
		{"dim / red", "a dimmed marker is on the path without your burn; a red one is below the surface"},
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
		{"b", "to fly the burn by hand instead, light the engine at the burn marker and cut it when the node row's Δv reads 0; the Flight School step clears once you are above 700 km"},
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
	"PAUSE MENU":         "you pressed esc on the map: save, load, quit, settings, keyboard layout",
	"CAMERA & VIEW":      "the map is too crowded, too far, or from the wrong angle",
	"TIME & WARP":        "you are waiting on a burn or an orbit",
	"MANUAL FLIGHT":      "you are flying by hand: throttle, engine, pointing the nose",
	"NAVIGATION":         "you want info on a body, a target, or your mission progress",
	"PLAN BURNS":         "you are planning a transfer, a circularisation or a node",
	"RENDEZVOUS PLANNER": "you are closing on another vessel and the planner opened",
	"VESSEL":             "you are spawning, switching, staging or undocking",
	"DOCKING":            "you are closing on a port: who has one, the gate, the magnet, the bump",
	"SPAWN FORM":         "you pressed n and are choosing a vessel, a place to start and an altitude",
	"VEHICLE ASSEMBLY":   "you are building your own rocket",
	"MISSIONS":           "you want Flight School or the Challenge ladder on or off",
	"SAVES":              "you are saving, loading, renaming or deleting a save",
	"MULTIPLAYER":        "you are flying with friends: chat, ghosts, sync, hosting",
	"MOUSE":              "you would rather click than type",
	"MAP MARKERS":        "a symbol on the map means nothing to you",
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
	sec := helpSections[h.page-1]
	if sec.header == helpIndexTitle(sec.header) {
		// The box title already says it (review #555 L4); a header with a
		// parenthetical keeps its row because it adds the when-clause.
		return h.renderRows(layout, sec.rows)
	}
	// The box title already names the page; the row keeps only the scope
	// note in parentheses, so "SPAWN FORM" is not followed by "SPAWN FORM (n)".
	note := sec.header[len(helpIndexTitle(sec.header))+1:]
	lines := []string{h.theme.Dim.Render(note)}
	return append(lines, h.renderRows(layout, sec.rows)...)
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
			mark = h.theme.Primary.Render("▸") + " "
		}
		num := strconv.Itoa(n + 1)
		numPad := strings.Repeat(" ", 3-len(num))
		lines = append(lines, mark+h.theme.Primary.Render(num)+numPad+title+pad+when)
	}
	return lines
}

// Render windows the body to the terminal height inside the shared form
// frame (B11 / G9 Q4): one box titled with the page, the position line and
// controls on the frame's bottom edge. Truncates each row to the box.
// Clamps + caches the scroll geometry so HandleKey paging stays in range.
// width x height is the whole framed block (the App's Title Row sits above).
func (h *Help) Render(width, height int, layout keylayout.Layout) string {
	body := h.bodyLines(layout)
	boxW := width - 2*frameInset

	// frame (2) + box top edge, title and bottom edge (3).
	viewH := height - 5
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

	lines := make([]string, 0, viewH)
	for _, ln := range window {
		lines = append(lines, clipLine(ln, boxW-2))
	}
	// Pad so the box keeps its height even when the content is shorter than
	// the viewport (short terminals, last page).
	for len(lines) < viewH {
		lines = append(lines, "")
	}
	title := "KEYBINDINGS"
	if h.page != helpIndexPage {
		title = pageTitle(h.page)
	}
	return formFrame(h.theme, formBox(h.theme, title, lines, boxW), width, height, h.footer())
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
	// Digits reach pages 1-9 only; the rest are reached by cursor.
	keys := "[1-9] jump   [↑/↓ enter] pages 10-" + strconv.Itoa(helpPageCount()) + "   [F1/esc] close"
	if h.page != helpIndexPage {
		keys = "[↑/↓ PgUp/PgDn] scroll  [esc] index  [F1] close"
		if h.direct {
			keys = "[↑/↓ PgUp/PgDn] scroll  [esc / F1] back to where you were"
		}
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

// OpenFor opens the page titled title (as pageTitle spells it) in direct
// mode, so esc and F1 both leave help. An unknown title opens the index.
func (h *Help) OpenFor(title string) {
	h.ResetScroll()
	for n := 0; n < helpPageCount(); n++ {
		if pageTitle(n) == title {
			h.OpenPage(n)
			h.direct = true
			return
		}
	}
}

// Back returns from a page to the index and reports true; on the index it
// reports false so the caller closes the overlay.
func (h *Help) Back() bool {
	if h.page == helpIndexPage || h.direct {
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
		// Review #629 L4: a jump leaves the form's own page, so esc no
		// longer means "back to the form"; it steps to the index.
		h.direct = false
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
func (h *Help) ResetScroll() {
	h.scroll, h.page, h.cursor, h.direct = 0, helpIndexPage, 0, false
}

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
