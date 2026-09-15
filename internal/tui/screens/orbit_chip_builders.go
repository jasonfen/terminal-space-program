package screens

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/missions"
	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
	"github.com/jasonfen/terminal-space-program/internal/planner"
	"github.com/jasonfen/terminal-space-program/internal/render"
	"github.com/jasonfen/terminal-space-program/internal/settings"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
	"github.com/jasonfen/terminal-space-program/internal/tui/readout"
)

// This file holds the chip builders transplanted from renderHUD's
// per-block code (ADR 0010 / v0.13 slice 2). Each returns the chip's
// styled lines (a bare colored header + rows) or nil when the block isn't
// contextually relevant — the "relevant" half of the render rule. The old
// section() divider is dropped: a chip's header doubles as its label.
// Arithmetic and labels mirror the originals so the readouts are
// unchanged; only the placement (canvas corner vs. tall column) differs.

// Declared maximum content-line height (title included, borders not) for
// each of the eight ADR 0051 instrument boxes, decision 16 / build open
// item 3: a box Settings switches off leaves this many blank rows in its
// slot rather than dropping, so nothing below it in the column ever
// moves, even on the one box (MISSION) whose live height varies. The
// other seven are already fixed-height by decision 2 ("every row always
// present"), so their declared max is simply their one true height,
// pinned here rather than left to be discovered by a future shrink.
// MISSION alone varies at render time (3 to 7 rows, sized to its Flight
// School step); 7 is its measured widest rung (the ADR's own budget
// table).
const (
	engineBoxMaxLines     = 4
	propellantBoxMaxLines = 4
	guidanceBoxMaxLines   = 4
	navigationBoxMaxLines = 8
	commsBoxMaxLines      = 2
	targetBoxMaxLines     = 5
	stagesBoxMaxLines     = 1
	missionBoxMaxLines    = 7
)

// blankInstrumentBoxLines renders a Settings-hidden box's slot: its bare
// title (no live badge: there's nothing live to badge) followed by
// blank rows out to maxLines, so the box's declared maximum height is
// what the layout ever reserves for it, whether it's showing content or
// not (decision 16).
func blankInstrumentBoxLines(theme Theme, name string, maxLines int) []string {
	lines := make([]string, maxLines)
	lines[0] = theme.Primary.Render(name)
	return lines
}

// engineLit reports whether c currently has a live burn under way: the
// ADR 0010 condition decision 16 carries forward: fuel and a live burn
// are never hidden by F2 Declutter, so ENGINE and PROPELLANT are the two
// boxes exempted from the group hide while this is true. Scoped to the
// ACTIVE craft (not the whole slate, unlike AnyCraftThrusting/the 10x
// burn-warp cap): ENGINE/PROPELLANT show the active craft's own
// throttle and fuel, so the exemption should track whether THAT craft's
// numbers are live, not whether some other craft off-screen is burning.
func engineLit(c *spacecraft.Spacecraft) bool {
	return c != nil && (c.ActiveBurn != nil || c.ManualBurn != nil)
}

// navigationBoxesInOrder appends the eight ADR 0051 instrument boxes at
// Core priority (never dropped by layoutChipsBySide's shrink/drop until
// every Normal chip on their side has already gone), in the ruled left
// order (ENGINE, PROPELLANT, GUIDANCE, COMMS, STAGES, MISSION) and right
// order (NAVIGATION, TARGET).
//
// Two independent gates, per decision 16:
//   - F2 Declutter hides all eight together (a momentary "clean map"
//     gesture: the chip is dropped outright, not blanked, so the
//     column genuinely shortens while it's on), except ENGINE and
//     PROPELLANT stay through it while engineLit is true.
//   - Settings can switch any box off individually (a standing
//     preference): rather than dropping, that box's slot renders
//     blankInstrumentBoxLines at its declared max height, so the boxes
//     below it in the column never move. A box can be both: Declutter
//     hides it outright even if Settings would otherwise blank it, and
//     the two exempt boxes still respect their OWN Settings choice
//     (blank vs. live) while surviving Declutter.
func (v *OrbitView) navigationBoxesInOrder(w *sim.World, chips []builtChip) []builtChip {
	// The Proximity View (ADR 0043) is its own close-range instrument
	// panel (buildProximityChip), not one of the two views ADR 0051's
	// "one layout, both views" decision 3 covers (the orbit map and the
	// LAUNCH/surface view), it never coexisted with the pre-ADR-0051
	// VESSEL/MISSIONS core chips at small canvases either. Suppressing
	// the eight boxes here keeps the Proximity View's own budget intact
	// instead of the much larger new box set evicting it via the
	// stacker at a narrow terminal.
	if w.ViewMode == sim.ViewProximity {
		return chips
	}
	lit := engineLit(w.ActiveCraft())
	type boxDef struct {
		id       settings.Chip
		build    func(*sim.World) []string
		name     string
		maxLines int
		exempt   bool // stays through F2 Declutter while lit
	}
	left := []boxDef{
		{settings.ChipEngine, v.buildEngineBox, "ENGINE", engineBoxMaxLines, true},
		{settings.ChipPropellant, v.buildPropellantBox, "PROPELLANT", propellantBoxMaxLines, true},
		{settings.ChipGuidance, v.buildGuidanceBox, "GUIDANCE", guidanceBoxMaxLines, false},
		{settings.ChipComms, v.buildCommsBox, "COMMS", commsBoxMaxLines, false},
		{settings.ChipStages, v.buildStagesBox, "STAGES", stagesBoxMaxLines, false},
		{settings.ChipMissions, v.buildMissionBox, "MISSION", missionBoxMaxLines, false},
	}
	for i, b := range left {
		if v.declutter && !(b.exempt && lit) {
			continue
		}
		var lines []string
		if v.settings.ChipEnabled(b.id) {
			lines = b.build(w)
		} else {
			lines = blankInstrumentBoxLines(v.theme, b.name, b.maxLines)
		}
		c := builtChip{id: b.id, corner: cornerTopLeft, lines: lines, priority: chipPriorityCore}
		if i == 0 {
			// ENGINE folded in the retired NODES chip's node row
			// (decision 1); keep its click routing alive by reusing
			// ChipNodes' id purely for HitChip resolution (app.go opens
			// the maneuver screen on a click matching this id): this
			// overrides the b.id set above, which is only ChipEngine's
			// own Settings-visibility lookup done explicitly two lines
			// up, not a value HitChip ever needs to see.
			c.id = settings.ChipNodes
		}
		chips = append(chips, c)
	}
	rightBoxes := []boxDef{
		{settings.ChipNavigation, v.buildNavigationBox, "NAVIGATION", navigationBoxMaxLines, false},
		{settings.ChipTarget, v.buildTargetBox, "TARGET", targetBoxMaxLines, false},
	}
	for _, b := range rightBoxes {
		if v.declutter {
			continue
		}
		var lines []string
		if v.settings.ChipEnabled(b.id) {
			lines = b.build(w)
		} else {
			lines = blankInstrumentBoxLines(v.theme, b.name, b.maxLines)
		}
		chips = append(chips, builtChip{id: b.id, corner: cornerTopRight, lines: lines, priority: chipPriorityCore})
	}
	return chips
}

func (v *OrbitView) assembleChips(w *sim.World) []builtChip {
	var chips []builtChip
	// The eight fixed instrument boxes (ADR 0051), first in the left and
	// right stacks respectively, decision 2's "boxes never move" reads
	// most simply as a fixed prefix of each column, with notices (below)
	// layering after them until slice 3 moves every notice into its own
	// bay.
	chips = v.navigationBoxesInOrder(w, chips)
	// PROXIMITY (ADR 0043) is the close-range view's own instrument panel,
	// not a notice: it replaces the map's box stack while the Proximity
	// View is up, so it stays in cornerTopLeft under the ordinary
	// Graceful Shrink budget (ADR 0046), with its own Compact Form. Nil
	// in every other ViewMode.
	if lines := v.buildProximityChip(w); lines != nil && v.chipEnabled("") {
		chips = append(chips, builtChip{corner: cornerTopLeft, lines: lines, compact: v.buildProximityChipCompact(w)})
	}

	// Every pop-up notice lives in the bay now (ADR 0051 slice 3 item 2,
	// re-grill Q5): cornerBay is exempt from the side budgets
	// layoutChipsBySide enforces, and composeChips clamps its own height
	// and width against whatever the eight boxes and the navball are
	// using this frame (ruling 1), so notices can never move a box, and
	// a box can never move because a notice appeared or left.
	//
	// Bay append order is oldest-first (composeChips stacks the LAST
	// entry at the bottom and folds from index 0 upward when the bay
	// overflows (see layoutBayFold). Ordered here least-critical-first,
	// most-critical-last: transient, self-refreshing readouts (the live
	// path's next few moments) fold before session/coordination lines,
	// which fold before the two things a player must never lose access
	// to mid-interaction (DOCKED's only route to [J]/[U], and the
	// keyboard-focus-holding RENDEZVOUS PLAN picker): those two go last
	// so a genuine overflow always empties the transient end of the bay
	// first.
	add := func(id settings.Chip, lines []string) {
		if lines == nil || !v.chipEnabled(id) {
			return
		}
		chips = append(chips, builtChip{id: id, corner: cornerBay, lines: lines})
	}
	// FRAME TRANSITION / CAPTURE PREVIEW / SOI PASS: the live path's next
	// SOI crossing and (if the last planted node changes primary) the
	// arrival preview at it. All three Target-independent (SOI PASS
	// de-dupes with TARGET inside its own builder when they name the same
	// body).
	add(settings.ChipFrameTransition, v.buildFrameTransitionChip(w))
	add(settings.ChipCapture, v.buildCaptureChip(w))
	add(settings.ChipSOIPass, v.buildSOIPassChip(w))
	add(settings.ChipChute, v.buildChuteChip(w))
	// CLOSE RANGE (ADR 0043): a one-line pointer at the Proximity View
	// jump key, offered when an approach crosses inside the range at
	// which the game already treats two vessels as flying together.
	// Self-limiting rather than standing: sim's crossing state machine
	// retires it the moment the player acts, and it never renders inside
	// the view it advertises.
	add("", v.buildProximityHintChip(w))
	// SESSION moments (v0.27 S6 / ADR 0034): join/leave/sync events as a
	// transient notice. Always-on when events are fresh (empty id:
	// moments are too short-lived to warrant a Settings toggle);
	// declutter still clears it via the empty-id path.
	add("", v.buildSessionEventsChip(w))
	// CHAT: a coordination line must not be togglable into silence
	// (ADR 0035 §2).
	add("", v.buildChatChip(w))
	// RENDEZVOUS (v0.29 S2): the persistent Rendezvous Warp surface —
	// join prompt / armed-waiting / coasting readout. Always-on while the
	// state machine is live (empty id); F2 declutter still clears it.
	add("", v.buildRendezvousChip(w))
	// TIME LOCK (ADR 0037 §3): the minimal standing line for a plain
	// proximity lock — no agreement, so nothing else on screen would say
	// the player's warp is being held. Nil inside an agreement, where the
	// chip above says it better.
	add("", v.buildTimeLockChip(w))
	// VESSEL DESTROYED (#427 / ADR 0048): the game's first Standing
	// Alert: persists for as long as the active craft's Crashed state
	// holds, not a transient Event Flash. Bypasses chipEnabled entirely
	// (no Settings id, nothing to toggle) and survives F2 declutter: a
	// destroyed vessel with no visible way out is exactly the "safety/
	// continuity fact the player has no other way to see" CONTEXT.md's
	// Standing Alert rule exists for.
	if lines := v.buildVesselDestroyedChip(w); lines != nil {
		chips = append(chips, builtChip{corner: cornerBay, lines: lines})
	}
	// DOCKED (ADR 0038 S4): the rider-view standing block — unconditional
	// while one of this player's craft rides in another player's stack
	// (names the ride + the exits), with #253's owner-away line folded in
	// as an extra row. Always-on (empty id); F2 declutter still clears
	// it. #328: this is the rider's only surviving route to [J] request
	// control / [U] undock once absorbed into another player's stack, so
	// it goes near the end of the bay's fold order.
	add("", v.buildDockGuestChip(w))
	// RENDEZVOUS PLAN (ADR 0045 S6, #399; renamed from MEETING PLAN,
	// slice 3 ruling 2): the picker holds keyboard focus while open
	// (app.go's key intercept claims ←/→/↑/↓/Enter/Esc before they can
	// reach camera pan or anything else), so unlike every other chip it
	// bypasses chipEnabled: a modal the player just summoned with K must
	// not silently vanish under F2 declutter while it's still eating
	// their keystrokes. Last in append order: the bay's fold order
	// (oldest-first) means it is the very last thing ever folded away.
	if lines := v.buildMeetingPickerChip(); lines != nil {
		chips = append(chips, builtChip{corner: cornerBay, lines: lines})
	}
	return chips
}

// sessionEventChipTTL is how long a join/leave/sync moment stays on
// the canvas — wall clock, so warp can't stretch or blink it.
//
// sessionEventChipDepth caps how many rows can be on screen at once
// (#280, ADR 0037 §4). The chip used to render every moment inside its
// TTL with no bound, so a burst — 852 coupled/released moments in one
// live session — grew a block tall enough to occlude the flight view.
// ADR 0037 removes the structural source of that burst by never chipping
// the couple state inside an agreement, but the cap is defense in depth:
// the same CHAT depth-cap pattern (oldest dropped first), so any
// legitimate burst stays bounded no matter where it comes from. Same
// depth as CHAT — the ADR asks for that chip's pattern, and two capped
// stacks in the same view have no reason to disagree about "a few rows".
// Playtest-tunable.
const (
	sessionEventChipTTL   = 6 * time.Second
	sessionEventChipDepth = chatChipDepth
)

// buildSessionEventsChip renders recent multiplayer session moments
// (v0.27 S6). Nil outside a session or when every event has aged out.
func (v *OrbitView) buildSessionEventsChip(w *sim.World) []string {
	if len(w.SessionEvents) == 0 {
		return nil
	}
	now := time.Now()
	type row struct {
		text  string
		alert bool // rendered in the Alert style instead of Dim
	}
	var rows []row
	plain := func(text string) { rows = append(rows, row{text: text}) }
	for _, e := range w.SessionEvents {
		if now.Sub(e.At) > sessionEventChipTTL {
			continue
		}
		switch e.Kind {
		case sim.SessionEventJoin:
			plain("◇ " + e.Handle + " joined")
		case sim.SessionEventLeave:
			plain("◇ " + e.Handle + " left")
		case sim.SessionEventSync:
			plain("◇ " + e.Handle + " synced to you")
		case sim.SessionEventSyncedTo:
			plain("◇ synced to " + e.Handle)
		case sim.SessionEventCoWarpCoupled:
			plain("◇ warp coupled with " + e.Handle)
		case sim.SessionEventCoWarpReleased:
			plain("◇ warp released with " + e.Handle)
		case sim.SessionEventRendezvousArmed:
			plain("◇ " + e.Handle + " wants to rendezvous — [y] join")
		case sim.SessionEventRendezvousArrived:
			plain("◇ rendezvous: encounter reached — " + e.Handle + " alongside")
		case sim.SessionEventRendezvousCancelled:
			plain("◇ rendezvous with " + e.Handle + " cancelled")
		case sim.SessionEventRendezvousWaypoint:
			// #252: the standing intent passed an encounter outside couple
			// range and re-aimed — the RENDEZVOUS chip carries the new τ/CA,
			// this moment just says the advance was deliberate.
			plain("◇ rendezvous: waypoint passed — coasting on with " + e.Handle)
		case sim.SessionEventDocked:
			plain("◇ docked with " + e.Handle)
		case sim.SessionEventUndocked:
			plain("◇ undocked from " + e.Handle)
		case sim.SessionEventTransfer:
			plain("◇ control handed to " + e.Handle)
		case sim.SessionEventUndockRefused:
			// #307: two rows because the second one is the way out — the
			// refusal alone would leave the player stuck with no next move.
			rows = append(rows,
				row{text: "⚠ undock refused — your vessel is not on top of the stack", alert: true},
				row{text: "  have " + e.Handle + " hand control back, then release"})
		case sim.SessionEventTransferRefused:
			// ADR 0040 §2: the reason travels with the moment, so the chip
			// says what stopped the handover rather than that one didn't
			// happen. Detail is already a sentence from the ledger.
			reason := e.Detail
			if reason == "" {
				reason = "transfer refused"
			}
			rows = append(rows, row{text: "⚠ " + reason, alert: true})
		case sim.SessionEventParcelReturned:
			rows = append(rows,
				row{text: "◇ " + e.Handle + " released your vessel while you were away"},
				row{text: "  it is back on your slate — throttle zero, main engine, no hold"})
		case sim.SessionEventReleaseRefused:
			rows = append(rows,
				row{text: "⚠ release refused — " + e.Handle + "'s vessel sits under yours", alert: true},
				row{text: "  hand control back [J], then they release"})
		case sim.SessionEventControlReclaimed:
			rows = append(rows,
				row{text: "⚠ " + e.Handle + " took the stack back while you were away", alert: true},
				row{text: "  they were riding in it and your seat was empty"})
		case sim.SessionEventDockLost:
			rows = append(rows, row{text: "⚠ dock with " + e.Handle + " ended — the stack no longer exists", alert: true})
		case sim.SessionEventTargetLockLost:
			msg := "⚠ target lock lost on reconnect"
			if e.Handle != "" {
				msg = "⚠ target lock on " + e.Handle + " lost on reconnect"
			}
			rows = append(rows, row{text: msg, alert: true})
		case sim.SessionEventRendezvousDegraded:
			rows = append(rows, row{text: "⚠ rendezvous encounter degraded", alert: true})
		case sim.SessionEventWentQuiet:
			// ADR 0036: addressed at the partner holding the Commitment, so
			// it names what is still being held up rather than merely
			// reporting that someone stopped answering.
			held := e.Detail
			if held == "" {
				held = "commitment"
			}
			plain("◇ " + e.Handle + " went quiet — " + held + " held")
		case sim.SessionEventBack:
			plain("◇ " + e.Handle + " is back")
		case sim.SessionEventResumed:
			// Opens the replay of the interval this player missed (ADR 0036
			// S6). Sim-time, not wall clock: what matters is how far their
			// craft flew, which under warp bears no relation to how long
			// their laptop was shut.
			resumed := "◇ resumed — " + readout.Duration(e.Elapsed) + " ran while you were away"
			if e.Detail != "" {
				// The replay is bounded so it cannot bury the orbit view; say
				// so rather than truncating in silence.
				resumed += " (" + e.Detail + ")"
			}
			plain(resumed)
		case sim.SessionEventTimedOut:
			rows = append(rows, row{text: "⚠ " + e.Handle + "'s session timed out — they never came back", alert: true})
		case sim.SessionEventServerRestart:
			rows = append(rows, row{text: "⚠ server restarting — reconnect in a moment, progress saved", alert: true})
		}
	}
	if len(rows) == 0 {
		return nil
	}
	// Depth cap (#280): show the tail — the newest moments — and drop the
	// oldest, exactly as CHAT does. A two-row event (the undock refusal
	// with its way out) is trimmed as rows, not as events; losing the lead
	// of a stale pair costs less than an occluded flight view, and both
	// rows survive whenever the pair is inside the cap.
	if len(rows) > sessionEventChipDepth {
		rows = rows[len(rows)-sessionEventChipDepth:]
	}
	lines := []string{v.theme.Primary.Render("SESSION")}
	for _, r := range rows {
		if r.alert {
			lines = append(lines, v.theme.Alert.Render(r.text))
		} else {
			lines = append(lines, v.theme.Dim.Render(r.text))
		}
	}
	return lines
}

// chatChipTTL is deliberately long against the 6 s session-moment TTL
// (ADR 0035 §2): missing "X joined" is fine; missing "burning in 30 s"
// because you were looking at the navball is not. Depth shows the tail
// of the conversation, not one nudge. Both playtest-tunable.
const (
	chatChipTTL   = 30 * time.Second
	chatChipDepth = 4
)

// buildChatChip renders the recent chat lines (ADR 0035 S4) — its own
// builder and corner so chat volume and session moments never contend.
// A DM renders visibly distinct from a broadcast: ">gern: …" on the
// sender's echo, "gern>you: …" for the recipient — ASCII '>' on
// purpose; the arrow runes are EastAsian-ambiguous width (the ☾-class
// trap). Nil when quiet or aged out.
func (v *OrbitView) buildChatChip(w *sim.World) []string {
	if len(w.ChatLines) == 0 {
		return nil
	}
	self := ""
	if w.Session != nil {
		self = w.Session.Self
	}
	now := time.Now()
	var rows []string
	for _, l := range w.ChatLines {
		if now.Sub(l.At) > chatChipTTL {
			continue
		}
		switch {
		case l.To == "":
			rows = append(rows, v.theme.Dim.Render(l.Handle+": "+l.Text))
		case l.Owner == self:
			rows = append(rows, v.theme.Warning.Render(">"+l.ToHandle+": "+l.Text))
		default:
			rows = append(rows, v.theme.Warning.Render(l.Handle+">you: "+l.Text))
		}
	}
	if len(rows) == 0 {
		return nil
	}
	if len(rows) > chatChipDepth {
		rows = rows[len(rows)-chatChipDepth:]
	}
	return append([]string{v.theme.Primary.Render("CHAT")}, rows...)
}

// buildRendezvousChip is the persistent Rendezvous Warp surface (v0.29
// S2, ADR 0034 v0.29 addendum) — one chip, three states, priority
// engaged > armed > invited:
//   - coasting: the shared coast runs — countdown to τ, the committed
//     approach, the live recomputed approach, the degrade warning, and
//     the cancel key;
//   - armed-waiting: the viewer Engaged, the partner hasn't — the warp
//     holds (no solo drift) so the chip says why time isn't moving;
//   - invited: a partner armed toward the viewer — the anti-overlook
//     join prompt ([y] responds from here, no trip to the Session
//     screen).
//
// Across a subspace gap (#250) the armed and invited states swap their
// call to action for an attribution: the armed line names who is ahead
// and points at Sync instead of blaming the partner, and a Blocked
// invite renders dimmed with [y] suppressed instead of vanishing.
//
// Nil when the state machine is idle, which is almost always.
func (v *OrbitView) buildRendezvousChip(w *sim.World) []string {
	now := w.Clock.SimTime
	switch {
	case w.RendezvousUnplanned():
		return v.rendezvousUnplannedLines(w)
	case w.RendezvousApproachPhase():
		return v.rendezvousApproachLines(w)
	case w.RendezvousWarpEngaged():
		aw := w.AutoWarp
		tauIn := aw.T.Sub(now)
		if tauIn < 0 {
			tauIn = 0
		}
		lines := []string{
			v.theme.Primary.Render("RENDEZVOUS"),
			"  coasting with " + aw.RendezvousHandle + " to the encounter",
			chipRow("τ in:", readout.Duration(tauIn)),
		}
		if arm := w.RendezvousArm; arm != nil {
			lines = append(lines, chipRow("committed:", readout.Distance(arm.CommittedCA)))
			if line := rendezvousMeetingLine(arm.MeetingPlaceLabel, arm.MeetingLaps); line != "" {
				lines = append(lines, line)
			}
			// ADR 0039 S3 / #281: the trend across waypoint re-derivations —
			// distinct from the degrade warning below, which compares
			// against a baseline that re-bases every waypoint and so can
			// never catch a standing intent that worsens a little each
			// time. Silent until there are two committed CAs to compare.
			lines = append(lines, rendezvousTrendLines(*arm, v.theme)...)
		}
		if w.RendezvousApproachM > 0 {
			lines = append(lines, chipRow("approach:", readout.Distance(w.RendezvousApproachM)))
		}
		if line := v.rendezvousHoldOrPaceLine(w, aw.RendezvousHandle); line != "" {
			lines = append(lines, line)
		}
		// Standing away line (#253): the partner's session flies on under
		// the Commitment Reprieve with nobody at the controls. State-driven
		// like the hold line above — the went-quiet SESSION chip expires in
		// 6 s while Away lasts hours by design, and this is precisely the
		// fact a player weighing the encounter needs on screen.
		//
		// The "z" glyph must stay width-1: padChipBlock measures chip lines
		// in terminal cells (lipgloss.Width) but splitStyledCells splices
		// per rune, so a width-2 emoji (💤) desyncs the two and overflows
		// the canvas row by one cell for every away line overlaid.
		if w.RendezvousPartnerAway {
			lines = append(lines, "  "+v.theme.Warning.Render("z "+aw.RendezvousHandle+" is away — their session is still flying"))
		}
		if w.RendezvousDegraded {
			lines = append(lines, "  "+v.theme.Alert.Render("⚠ encounter degraded — partner drifted off the plan"))
		}
		return append(lines, v.theme.Dim.Render("  [/] cancel"))
	case w.RendezvousArm != nil:
		arm := w.RendezvousArm
		status := v.theme.Warning.Render("  armed → " + arm.Handle + CraftTag(arm.CraftName) + " — waiting for them to join")
		switch wt := w.RendezvousWait; wt.Reason {
		case sim.RendezvousWaitSubspaceGap:
			// #250: "waiting for them to join" would blame the partner for
			// a gap the viewer (or the partner) warped open — name who is
			// ahead and the actual fix instead. Sync is forward-only, so
			// the fix depends on direction: the laggard comes forward.
			who := "you are " + readout.Duration(wt.AheadBy) + " ahead of " + arm.Handle
			fix := "they must Sync to you"
			if wt.AheadBy < 0 {
				who = arm.Handle + " is " + readout.Duration(-wt.AheadBy) + " ahead of you"
				fix = "Sync to rejoin"
			}
			status = v.theme.Alert.Render("  cannot couple — " + who + " — " + fix)
		case sim.RendezvousWaitSelf:
			// #260: same misattribution family — the partner DID join, and
			// the viewer's own Sync or node-chase is what defers the coast.
			// Own the wait instead of blaming them or advising a Sync.
			status = v.theme.Warning.Render("  your Auto-Warp is running — coast starts when it releases")
		}
		armTauIn := arm.Tau.Sub(now)
		if armTauIn < 0 {
			armTauIn = 0
		}
		lines := []string{
			v.theme.Primary.Render("RENDEZVOUS"),
			status,
			chipRow("τ in:", readout.Duration(armTauIn)),
			chipRow("CA:", readout.Distance(arm.CommittedCA)),
		}
		if line := rendezvousMeetingLine(arm.MeetingPlaceLabel, arm.MeetingLaps); line != "" {
			lines = append(lines, line)
		}
		return append(lines, v.theme.Dim.Render("  [/] cancel"))
	case w.RendezvousInvite != nil:
		inv := w.RendezvousInvite
		if inv.Blocked {
			// #250: the invite is real but unjoinable across the subspace
			// gap — a dimmed attribution with the join key suppressed, so
			// the prompt explains itself instead of vanishing. Sync is
			// forward-only: a viewer who is ahead cannot Sync back, the
			// initiator has to come forward.
			gap := "subspace gap, Sync to join"
			if inv.AheadBy > 0 {
				gap = "subspace gap, they must Sync to you"
			}
			lines := []string{
				v.theme.Primary.Render("RENDEZVOUS"),
				v.theme.Dim.Render("  ◇ " + inv.Handle + CraftTag(inv.CraftName) + " wants to rendezvous — " + gap),
			}
			lines = append(lines, rendezvousInviteEncounterLines(inv, now)...)
			if line := rendezvousMeetingLine(inv.MeetingPlaceLabel, inv.MeetingLaps); line != "" {
				lines = append(lines, line)
			}
			return lines
		}
		lines := []string{
			v.theme.Primary.Render("RENDEZVOUS"),
			v.theme.Warning.Render("  ◇ " + inv.Handle + CraftTag(inv.CraftName) + " wants to rendezvous"),
		}
		lines = append(lines, rendezvousInviteEncounterLines(inv, now)...)
		if line := rendezvousMeetingLine(inv.MeetingPlaceLabel, inv.MeetingLaps); line != "" {
			lines = append(lines, line)
		}
		// Name the seat at the moment it is taken (ADR 0037 §2): roles
		// are fixed at invite time, so this prompt is the only place the
		// asymmetry is a choice rather than a later surprise.
		return append(lines, v.theme.Warning.Render("  [y] join as copilot — "+inv.Handle+" sets the pair's warp"))
	}
	return nil
}

// rendezvousMeetingLine renders the Meeting Place + lap count row (ADR
// 0045 S7, #400) — agreement state named on both sides' RENDEZVOUS chip,
// carried verbatim from whichever side committed it (RendezvousArm.
// MeetingPlaceLabel on the initiator, RendezvousInvite.MeetingPlaceLabel
// on the accepter before joining, RendezvousArm.MeetingPlaceLabel again
// after — see SetRendezvousMeeting). "" (render nothing) whenever the
// commit's source wasn't a planted Meeting Burn node — including the
// whole agreed-no-plan state, which never had one.
func rendezvousMeetingLine(placeLabel string, laps int) string {
	if placeLabel == "" {
		return ""
	}
	return chipRow("meeting:", fmt.Sprintf("%s — %d laps", placeLabel, laps))
}

// rendezvousInviteEncounterLines renders the invite's τ/CA rows — nil
// (render nothing) when inv.Tau is zero (finding 2, batch review):
// refreshRendezvousInvite now surfaces a zero-τ invite for ADR 0045 S7's
// "agreed, no plan yet" state (#400); a negative duration is clamped to
// zero here so rendering these rows unconditionally doesn't fabricate a
// "τ in: 0s / CA: 0 m": an imminent zero-metre encounter that was never
// computed, to the player deciding whether to accept.
func rendezvousInviteEncounterLines(inv *sim.RendezvousInvite, now time.Time) []string {
	if inv.Tau.IsZero() {
		return nil
	}
	invTauIn := inv.Tau.Sub(now)
	if invTauIn < 0 {
		invTauIn = 0
	}
	return []string{
		chipRow("τ in:", readout.Duration(invTauIn)),
		chipRow("CA:", readout.Distance(inv.CA)),
	}
}

// rendezvousUnplannedLines is the RENDEZVOUS chip's fifth state (ADR
// 0045 S7, #400): Engaged, but neither a planted node nor the 4h
// current-course search found an encounter to commit to at Engage
// time — "we are going to meet", agreed, with nothing planned yet.
// Distinct from rendezvousApproachLines: that state is a demotion FROM a
// real coast; this agreement never had one to demote from, so it never
// sets Approach and never renders the seat/rate rows that assume it.
//
// Two sub-states once mutual (w.RendezvousMutualUnplanned): the
// initiator is told to plan (K, then Engage again to commit it — see
// EngageRendezvousWarpAs's "replaces any prior arm"), the accepter is
// told they're holding for the initiator's call — ADR 0037's "they do
// not vote" applies here too. Before mutual, the line is the same
// "waiting for them to join" every other pre-coast state uses.
func (v *OrbitView) rendezvousUnplannedLines(w *sim.World) []string {
	arm := w.RendezvousArm
	lines := []string{
		v.theme.Primary.Render("RENDEZVOUS"),
		"  agreed with " + arm.Handle + CraftTag(arm.CraftName),
	}
	switch {
	case w.RendezvousMutualUnplanned && arm.Initiator:
		lines = append(lines, v.theme.Dim.Render("  no plan yet — pick a Meeting Place [K], then Engage to commit"))
	case w.RendezvousMutualUnplanned:
		lines = append(lines, v.theme.Dim.Render("  no plan yet — holding for "+arm.Handle+"'s call"))
	default:
		lines = append(lines, v.theme.Warning.Render("  waiting for them to join"))
	}
	return append(lines, v.theme.Dim.Render("  [/] cancel"))
}

// rendezvousTrendLines renders the CA trend row (ADR 0039 S3 / #281):
// silent before the first waypoint advance (arm.PrevCommittedCASet
// false — a single CA has no trend), a quiet "shrinking" line when the
// latest waypoint improved on the one before it, or a two-line warning
// naming both the numbers and the doctrine's own diagnosis when it
// didn't. Equal values (no change) render nothing — neither claim is
// true of a flat trend.
func rendezvousTrendLines(arm sim.RendezvousArm, theme Theme) []string {
	if !arm.PrevCommittedCASet {
		return nil
	}
	switch {
	case arm.CommittedCA < arm.PrevCommittedCA:
		return []string{theme.Dim.Render("  CA " + readout.Distance(arm.CommittedCA) + " ↘ shrinking")}
	case arm.CommittedCA > arm.PrevCommittedCA:
		return []string{
			theme.Alert.Render("  ⚠ CA growing each pass (" + readout.Distance(arm.PrevCommittedCA) + " → " + readout.Distance(arm.CommittedCA) + ")"),
			theme.Alert.Render("    — phasing direction is wrong"),
		}
	}
	return nil
}

// rendezvousApproachLines is the RENDEZVOUS chip's fourth state: the
// standing terminal phase (ADR 0037 §1/§3). The τ handoff has released
// the driver and handed the ship back, but the pair is still time-locked,
// and a persistent constraint on the player's warp must be represented by
// persistent state — #305's 30m12s clamp was announced by exactly one
// 6-second transient.
//
// Four facts, in the order a pilot needs them: who you're flying with,
// which seat you hold, what the pair's clock is doing, and — only when it
// isn't you — what is holding it there. That last row is the answer to
// "why do my warp keys do nothing".
func (v *OrbitView) rendezvousApproachLines(w *sim.World) []string {
	arm := w.RendezvousArm
	rr := w.RendezvousRate
	handle := arm.Handle
	if rr.Handle != "" {
		handle = rr.Handle
	}
	lines := []string{
		v.theme.Primary.Render("RENDEZVOUS"),
		"  approach with " + handle + CraftTag(arm.CraftName),
	}
	// The seat is only meaningful once both sides have resolved it; an
	// unseated pair is on plain min-wins and saying "pilot" would be a lie.
	switch rr.Seat {
	case sim.RendezvousSeatPilot:
		lines = append(lines, chipRow("seat:", "pilot — your warp keys fly the pair"))
	case sim.RendezvousSeatCopilot:
		lines = append(lines, chipRow("seat:", "copilot — [,] brakes the pair, [.] follows"))
	}
	lines = append(lines, chipRow("rate:", WarpLabel(w.EffectiveWarp())))
	if held := rendezvousHoldLabel(w.RendezvousRateHold(), handle); held != "" {
		lines = append(lines, chipRow("held:", held))
	}
	if line := v.rendezvousHoldOrPaceLine(w, handle); line != "" {
		lines = append(lines, line)
	}
	// Standing away line (#253) — same reasoning as on the coasting state:
	// Away lasts hours, the went-quiet moment lasts six seconds. "z" stays
	// width-1 (see buildRendezvousChip's note on the ☾-class trap).
	if w.RendezvousPartnerAway {
		lines = append(lines, "  "+v.theme.Warning.Render("z "+handle+" is away — their session is still flying"))
	}
	return append(lines, v.theme.Dim.Render("  [/] cancel"))
}

// rendezvousHoldOrPaceLine is the RENDEZVOUS chip's standing line for a
// leader that has pulled ahead of its partner (#395, ADR 0045 S2, closing
// #279). Two mutually exclusive world states, two distinct lines:
//   - w.RendezvousHold (a genuinely stopped partner): the old "⏸ holding"
//     line, unchanged. A rate you cannot explain is a bug (v0.30 lesson),
//     but a partner who is literally paused explains itself.
//   - w.RendezvousPaced (a live partner, the leader is being paced back
//     instead of frozen at the boundary): a steady line naming the
//     current rate and who it's paced to, replacing the old flicker
//     between "coasting with X" and "⏸ holding" every report cycle — the
//     #279 bug was exactly that pair alternating in lockstep with the
//     relay report cadence.
//
// Empty when neither applies — the ordinary case, full rate, nothing to
// explain.
func (v *OrbitView) rendezvousHoldOrPaceLine(w *sim.World, handle string) string {
	switch {
	case w.RendezvousHold:
		return "  " + v.theme.Warning.Render("⏸ holding — waiting for "+handle)
	case w.RendezvousPaced:
		return "  " + v.theme.Warning.Render("coasting "+WarpLabel(w.EffectiveWarp())+" — paced to "+handle)
	}
	return ""
}

// rendezvousHoldLabel names what is holding the pair's rate when it isn't
// the viewer's own selection. Empty when it is — a "held: you" row would
// be noise on the one state that needs no explanation.
func rendezvousHoldLabel(h sim.RendezvousRateHolder, handle string) string {
	switch h {
	case sim.RendezvousRateFollowing:
		return handle + "'s warp — they fly the clock"
	case sim.RendezvousRatePartnerBraking:
		return handle + " braking the pair"
	case sim.RendezvousRatePartnerBurning:
		return handle + " burning"
	case sim.RendezvousRatePartnerPaused:
		return handle + " paused"
	}
	return ""
}

// buildTimeLockChip is the minimal standing surface for a warp lock with
// NO agreement behind it (ADR 0037 §3): two players drifted close and
// slow, proximity co-warp coupled them, and min-wins is quietly setting
// the player's rate. Who, and what the clock is doing — the RENDEZVOUS
// chip carries the same facts with far more context inside an agreement,
// so this stays out of its way there. #328 review: a docked rider's
// coupling comes from WithDockCoupling, not an agreement either — the
// standing DOCKED block already says "riding in X's stack", which is
// the same fact ("your clock isn't yours right now, X's is") stated in
// flight terms, so this stays out of ITS way too rather than stacking a
// second "warp locked with X" line directly above it.
//
// The v0.30 lesson (a silent no-op reads as broken) applied to warp: a
// lock on the player's time is always explained on screen — somewhere.
func (v *OrbitView) buildTimeLockChip(w *sim.World) []string {
	if !w.CoWarp.Coupled || v.rendezvousExplainsCoupledLock(w) || v.dockGuestExplainsCoupledLock(w) {
		return nil
	}
	partners := strings.Join(w.CoWarp.Partners, ", ")
	if partners == "" {
		return nil
	}
	return []string{
		v.theme.Primary.Render("TIME LOCK"),
		v.theme.Dim.Render("  warp locked with " + partners + " — " + WarpLabel(w.EffectiveWarp())),
	}
}

// dockGuestExplainsCoupledLock reports whether the standing DOCKED block
// (buildDockGuestChip) already explains the exact co-warp lock
// buildTimeLockChip would otherwise state (#328 review, mirroring
// rendezvousExplainsCoupledLock's specificity discipline). True only
// when this player is riding as a DockGuest AND the CoWarp coupling
// actually names that same stack owner — a rider coincidentally also
// proximity-coupled to a third, unrelated player still needs TIME LOCK
// to explain THAT lock, since DOCKED never mentions them.
func (v *OrbitView) dockGuestExplainsCoupledLock(w *sim.World) bool {
	dg := w.DockGuest
	if dg == nil {
		return false
	}
	for _, p := range w.CoWarp.Partners {
		if p == dg.OwnerHandle {
			return true
		}
	}
	return false
}

// rendezvousExplainsCoupledLock reports whether the RENDEZVOUS chip is
// already rendering the exact lock buildTimeLockChip would otherwise
// state (ADR 0037 §3 review). The old gate suppressed TIME LOCK whenever
// ANY RendezvousArm existed, which was too broad two ways: armed-waiting
// (RendezvousArm != nil but no reciprocal arm yet — RENDEZVOUS shows
// "waiting for them to join", never a rate) and an arm toward player A
// while the viewer is actually co-warp coupled to an unrelated player B
// (RENDEZVOUS never mentions B at all). Suppression is correct only when
// BOTH hold: the arm has become a standing agreement that narrates a rate
// (coasting or the demoted terminal phase — RendezvousWarpEngaged /
// RendezvousApproachPhase), AND the viewer is actually coupled with that
// same partner right now.
func (v *OrbitView) rendezvousExplainsCoupledLock(w *sim.World) bool {
	arm := w.RendezvousArm
	if arm == nil || !(w.RendezvousWarpEngaged() || w.RendezvousApproachPhase()) {
		return false
	}
	for _, p := range w.CoWarp.Partners {
		if p == arm.Handle {
			return true
		}
	}
	return false
}

// WarpLabel renders a warp factor the way the title-bar clock chip does
// ("1000x"), so a rate quoted in a chip row or a toast reads identically
// to the one in the clock. ASCII 'x', not '×': chip glyphs must stay
// width-1 (see the away line in buildRendezvousChip).
func WarpLabel(f float64) string { return fmt.Sprintf("%.0fx", f) }

// CraftTag renders a vessel name as a parenthetical to hang off a
// player's handle — "gern (Relay Tug-1)" (#295). Empty for an unnamed
// craft (an older peer's report carries no active-craft marker), so the
// caller's line degrades to the bare handle instead of an empty "()".
// Exported because the flight view's arm toast composes the same line.
func CraftTag(name string) string {
	if name == "" {
		return ""
	}
	return " (" + name + ")"
}

// buildDockGuestChip is the rider-view standing DOCKED block (ADR 0038
// S4): while one of this player's craft rides in another player's stack,
// this renders on EVERY frame — not only while the owner is away, which
// was #253's older and narrower treatment — naming the ride and the way
// out. Half of every dock used to be experienced as a crash (#301, the
// absorbed seat's whole flight UI blanking with no explanation); this is
// the standing surface that replaces the silence.
//
// The exit list forks on whether the stack owner has a live Session right
// now (w.DockOwnerOnline, the same presence gate ADR 0040 §4's empty-seat
// reclaim already uses): a connected owner — even an idle/Away one — gets
// the ask-first phrasing, since undocking is theirs to grant; an owner
// with no live Session at all is the empty-seat case, where [J] grants
// instantly (ADR 0040 §4), so the row says so and the now-meaningless
// "ask to undock" drops — there is nobody to ask.
//
// #253's away line survives as an EXTRA row on this same block rather
// than a second, competing one — "one surface, not two" (ADR 0038
// consequences). An away owner is by definition still connected
// (Server.isAway is false for an offline fingerprint), so the away row
// and the empty-seat exit fork never both fire.
func (v *OrbitView) buildDockGuestChip(w *sim.World) []string {
	dg := w.DockGuest
	if dg == nil {
		return nil
	}
	handle := dg.OwnerHandle
	if handle == "" {
		handle = "their"
	}
	lines := []string{
		v.theme.Primary.Render("DOCKED"),
		"  " + v.theme.Dim.Render("◇ riding in "+handle+"'s stack"),
	}
	if w.DockOwnerOnline() {
		lines = append(lines,
			v.theme.Dim.Render("  [J] request control"),
			// #330: relabel to [U] — Undock is bound uppercase
			// (input.go, key.NewBinding(key.WithKeys("U"))) and there is
			// no lowercase u binding anywhere in internal/tui. A rider
			// following the lowercase advertisement pressed a dead key.
			// docs/controls.md and the F1 overlay both already say U.
			v.theme.Dim.Render("  [U] ask to undock"),
		)
	} else {
		lines = append(lines, v.theme.Warning.Render("  [J] take the stick (pilot's gone)"))
	}
	if dg.OwnerAway {
		// "z" not 💤 — chip glyphs must be width-1 (see the away line in
		// buildRendezvousChip for why).
		lines = append(lines, "  "+v.theme.Warning.Render("z "+handle+" is away — their session is still flying"))
	}
	return lines
}

// buildDockGuestChipCompact is DOCKED's Compact Form (ADR 0046 / #422):
// the ride plus a single combined exits row, dropping the OwnerAway
// line. DOCKED is chipPriorityForced — the rider's only surviving route
// to [J]/[U] once absorbed into another player's stack (#328) — so even
// its Compact Form must keep both exit keys legible, just on one row
// instead of two.
func (v *OrbitView) buildDockGuestChipCompact(w *sim.World) []string {
	dg := w.DockGuest
	if dg == nil {
		return nil
	}
	handle := dg.OwnerHandle
	if handle == "" {
		handle = "their"
	}
	if w.DockOwnerOnline() {
		return []string{
			v.theme.Primary.Render("DOCKED") + "  " + v.theme.Dim.Render("riding in "+handle+"'s stack"),
			v.theme.Dim.Render("  [J] control · [U] undock"),
		}
	}
	return []string{
		v.theme.Primary.Render("DOCKED") + "  " + v.theme.Dim.Render("riding in "+handle+"'s stack"),
		v.theme.Warning.Render("  [J] take the stick (pilot's gone)"),
	}
}

// sendoffChipLines renders the MISSION chip's whole-Program-complete state
// (#426 item F, decision 9) — the same one line the ladder screen's
// active-card slot shows, plus the Challenge-ladder offer when
// World.LadderSendoff says to carry it.
func (v *OrbitView) sendoffChipLines(text string, offerChallenges bool) []string {
	lines := []string{
		v.theme.Primary.Render("MISSION"),
		"  " + text,
	}
	if offerChallenges {
		lines = append(lines, v.theme.Dim.Render("  [2] turn on the Challenge ladder"))
	}
	return lines
}

// missionChipLines is the pure content selector behind buildMissionsChip,
// split out so both the (World-armed) failure flash and the active-mission
// forms are unit-testable without a live World. A live flash takes precedence
// over the active mission — the mission that just failed is more urgent to
// surface than the next one in the ladder.
// missionChipWrapWidth caps the MISSION chip's width (ADR 0046 / #422):
// the tutorial hint text used to size the chip to its longest line — 87
// columns for "Warp to your burn ([G]) or fire manually ([b])…" — wide
// enough to overdraw the navball box beside it at 120 columns
// (2026-09-02 UX review, gameplay-flow-progression-09.txt). The hint now
// wraps to this width instead of setting the box width.
const missionChipWrapWidth = 40

// wrapChipText greedily word-wraps s to at most width visible columns
// per line, never splitting a word. A single word longer than width is
// left whole on its own line rather than hard-cut (chip text is short
// English sentences, not data that needs mid-word breaks).
func wrapChipText(s string, width int) []string {
	words := strings.Fields(s)
	if len(words) == 0 {
		return nil
	}
	var out []string
	line := words[0]
	for _, wd := range words[1:] {
		if lipgloss.Width(line)+1+lipgloss.Width(wd) > width {
			out = append(out, line)
			line = wd
			continue
		}
		line += " " + wd
	}
	out = append(out, line)
	return out
}

func (v *OrbitView) missionChipLines(flash string, flashing bool, m *missions.Mission, relayCount int) []string {
	if flashing {
		return []string{
			v.theme.Alert.Render("MISSION"),
			v.theme.Alert.Render("  ✗ " + flash),
		}
	}
	if m == nil {
		return nil
	}
	header := v.theme.Primary.Render("MISSION") + "  " + m.Name
	obj, ok := m.CurrentObjective()
	if !ok {
		// An InProgress mission always has a current (non-Passed) objective;
		// defend against an empty/all-passed mission slipping through anyway.
		return []string{header}
	}
	passed, total := m.Progress()
	lines := []string{
		header,
		fmt.Sprintf("  %s %s  %d/%d", hudNodeMarker, obj.Label(), passed, total),
	}
	// #426: a relay_coverage objective's raw progress ("0/1" — one countable
	// deploy-and-verify goal) doesn't tell the player how close the live
	// count is to the target, so it gets its own live row alongside the
	// generic N/M one.
	if obj.Kind == missions.KindRelayCoverage {
		lines = append(lines, fmt.Sprintf("  relays online %d/%d", relayCount, obj.Params.MinRelays))
	}
	// Tutorial steps surface their instruction ("Press [v] …") in-flight so the
	// player learns the control without opening the missions screen (Slice 7
	// playtest feedback). Challenge steps stay the clean one-liner. Wrapped
	// to missionChipWrapWidth (ADR 0046) rather than left to set the box's
	// width — a single long hint used to overdraw whatever sat beside it.
	if m.Program == missions.ProgramTutorial && obj.Description != "" {
		for _, row := range wrapChipText(obj.Description, missionChipWrapWidth) {
			lines = append(lines, v.theme.Dim.Render("    "+row))
		}
	}
	return lines
}

// attitudeHoldLabel names the ATTITUDE chip's hold: row in whichever
// frame the nav: row above it is actually reading against (#421). A
// held BurnMode is fixed in its own frame at the moment it's set
// (BurnPrograde always means orbit-frame prograde, never target-frame —
// see BurnDirectionWithTarget), and while EngineRCS is active a w/s/a/d
// pulse never touches AttitudeMode at all (internal/sim/rcs.go: "RCS is
// a 6-axis translation tool"), so a player who cycles nav: to TARGET and
// starts pulsing toward the other vessel keeps seeing whatever the SAS
// was last explicitly set to hold — commonly the plain orbit-frame
// "Prograde" from before the switch, disagreeing with the nav: row right
// above it (six-lane UX review, 2026-09-02).
//
// This remaps the four base axis-equivalent orbit-frame modes
// (Prograde / Retrograde / RadialOut / RadialIn) onto their
// NavTarget-vocabulary counterparts — mirroring ResolveAttitudeIntent's
// own (intent, NavMode) → BurnMode mapping in reverse — whenever
// NavMode is NavTarget and a relative target actually resolves, so the
// two rows can never name different frames. Already-Target-relative
// modes, NormalPlus/Minus (no target-frame counterpart:
// ResolveAttitudeIntent leaves them orbit-frame under NavTarget too),
// and every non-axis mode (Surface*, PlaneChange, Vector) pass through
// unchanged. Display-only — never mutates the craft's actual held
// AttitudeMode or its physical nose direction.
//
// The maintainer's own follow-up ("hold should also denote surface,
// orbit, or target in its readout"): every hold: row, not just this
// one, must name its frame using the same three words the navball
// button already uses (navModeLabel) so the two never disagree on
// screen.
//
// The tag names the frame of the HELD MODE, never the current NavMode
// (round 2 review, R2-F1, correcting an earlier version of this
// function that tagged frame-agnostic modes with whatever nav: showed).
// No held BurnMode actually reads NavMode: internal/spacecraft never
// consults it, and the nose direction is
// BurnDirectionWithTarget(AttitudeMode, rT, vT) with no nav argument at
// all (internal/sim/navball.go). So Surface Prograde/Retrograde always
// tag SURF, every target-relative mode (Target/AntiTarget/Target
// Prograde/Target Retrograde, whether held directly or produced by the
// remap above) always tags TGT, and every other mode (Prograde,
// Retrograde, RadialOut/In, NormalPlus/Minus, PlaneChange, Vector) is
// orbit-frame by construction and always tags ORBIT, whatever NavMode
// says. Tagging any of these with the current nav frame instead would
// assert a frame the hold does not have: a held Prograde is orbit-frame
// prograde under nav:SURFACE too (the nose does not move), and there is
// no surface-frame equivalent of Normal+/- at all.
func attitudeHoldLabel(w *sim.World, mode spacecraft.BurnMode) string {
	if w.NavMode == sim.NavTarget && w.HasRelativeTarget() {
		switch mode {
		case spacecraft.BurnPrograde:
			mode = spacecraft.BurnTargetPrograde
		case spacecraft.BurnRetrograde:
			mode = spacecraft.BurnTargetRetrograde
		case spacecraft.BurnRadialOut:
			mode = spacecraft.BurnTarget
		case spacecraft.BurnRadialIn:
			mode = spacecraft.BurnAntiTarget
		}
	}
	frame := sim.NavOrbit
	switch mode {
	case spacecraft.BurnSurfacePrograde, spacecraft.BurnSurfaceRetrograde:
		frame = sim.NavSurface
	case spacecraft.BurnTarget, spacecraft.BurnAntiTarget, spacecraft.BurnTargetPrograde, spacecraft.BurnTargetRetrograde:
		frame = sim.NavTarget
	}
	return fmt.Sprintf("%s (%s)", mode.String(), navModeLabel(frame))
}

// buildCommsChip surfaces the active probe's CommNet link state (ADR 0027 /
// C2-7): DIRECT (linked straight to a ground station), CONNECTED via N hops
// (through relays), or NO SIGNAL. Hidden for a crewed vessel — it is never
// command-gated — and for debris / no visible craft. assembleChips
// force-shows it while a just-blocked command is flashing (CommBlockedFlash),
// so the player learns why a command was refused even with the chip toggled
// off; otherwise it honours the Settings toggle + F2 declutter like any chip.
func (v *OrbitView) buildCommsChip(w *sim.World) []string {
	c := w.ActiveCraft()
	if c == nil || !w.CraftVisibleHere() {
		return nil
	}
	if c.Crewed || !c.Controllable {
		return nil // crewed craft are never gated; debris has no link to show
	}
	_, hops, connected := w.ActiveCommPath()
	return v.commsChipLines(hops, connected, w.CommGraph.Reason(c.ID))
}

// commsChipLines is the pure content selector behind buildCommsChip, split
// out so the DIRECT / CONNECTED / NO SIGNAL forms are unit-testable without a
// live World. A connected probe reads DIRECT for a single hop (straight to a
// station) or "CONNECTED via N hops" through relays; a disconnected probe
// reads NO SIGNAL in the alert style plus the classified cause (#221):
// name the cause AND the fix, and never steer at the wrong remedy — an
// unclassified disconnect degrades to the bare form rather than guess.
func (v *OrbitView) commsChipLines(hops int, connected bool, reason sim.CommDisconnectReason) []string {
	if !connected {
		lines := []string{
			v.theme.Alert.Render("COMMS"),
			v.theme.Alert.Render("  ⚠ NO SIGNAL"),
		}
		switch reason {
		case sim.CommDisconnectBlocked:
			lines = append(lines, v.theme.Dim.Render("  no station in view — relay needed"))
		case sim.CommDisconnectOutOfRange:
			lines = append(lines, v.theme.Dim.Render("  out of range — stronger antenna needed"))
		}
		return lines
	}
	status := fmt.Sprintf("CONNECTED via %d hops", hops)
	if hops <= 1 {
		status = "DIRECT"
	}
	return []string{
		v.theme.Primary.Render("COMMS"),
		"  " + status,
	}
}

// buildFrameTransitionChip surfaces the next SOI / frame transition implied
// by the planted-node chain. Returns nil when none is queued.
func (v *OrbitView) buildFrameTransitionChip(w *sim.World) []string {
	ft, ok := w.NextFrameTransition()
	if !ok {
		return nil
	}
	toName := ft.To
	if b, found := bodies.LookupByID(w.Systems, ft.To); found {
		toName = b.EnglishName
	}
	fromName := ft.From
	if b, found := bodies.LookupByID(w.Systems, ft.From); found {
		fromName = b.EnglishName
	}
	dur := ft.When.Sub(w.Clock.SimTime)
	when := v.theme.Warning.Render("now")
	if dur > 0 {
		when = readout.Countdown(dur)
	}
	return []string{
		v.theme.Primary.Render("FRAME TRANSITION"),
		fmt.Sprintf("  %s → %s", fromName, v.theme.Warning.Render(toName)),
		fmt.Sprintf("  at %s  (node #%d)", when, ft.NodeIndex+1),
	}
}

// buildCaptureChip surfaces the post-capture orbit at the last frame-
// changing planted node so the player catches retrograde-capture gotchas
// before firing. Returns nil when no arrival preview is available.
func (v *OrbitView) buildCaptureChip(w *sim.World) []string {
	cap, ok := w.ArrivalCapturePreview()
	if !ok {
		return nil
	}
	lines := []string{
		v.theme.Primary.Render("CAPTURE PREVIEW"),
		chipRow("primary:", cap.Primary.EnglishName),
	}
	if cap.Approximate {
		dirLabel := v.theme.Warning.Render("prograde")
		if cap.RetrogradeCapture {
			dirLabel = v.theme.Alert.Render("retrograde")
		}
		lines = append(lines,
			chipRow(readout.LabelArrival, readout.Speed(cap.ApproachSpeed)+" relative"),
			chipRow("direction:", dirLabel+" capture predicted"),
			v.theme.Dim.Render("  (intercept too central for orbit-element preview)"),
		)
		return lines
	}
	primaryR := cap.Primary.RadiusMeters()
	incDeg := cap.Inclination * 180 / math.Pi
	incLabel := readout.Angle(incDeg)
	switch {
	case cap.Hyperbolic:
		incLabel = v.theme.Alert.Render("escape — capture failed")
	case incDeg > 90:
		incLabel = v.theme.Alert.Render(incLabel + " (retrograde)")
	case incDeg > 30:
		incLabel = v.theme.Warning.Render(incLabel)
	}
	lines = append(lines, chipRow(readout.LabelIncl, incLabel))
	if !cap.Hyperbolic {
		capPeAlt := cap.PeriapsisM - primaryR
		capPeRow := chipRow(readout.LabelPe, readout.Distance(capPeAlt))
		if capPeAlt < 0 {
			capPeRow = v.theme.Warning.Render(capPeRow)
		}
		lines = append(lines,
			chipRow(readout.LabelAp, readout.Distance(cap.ApoapsisM-primaryR)),
			capPeRow,
		)
	}
	return lines
}

// buildChuteChip surfaces the parachute deploy state + surface-relative
// descent rate (the only window onto the canopy until ViewLanding lands).
// Returns nil for craft without a chute in flight.
func (v *OrbitView) buildChuteChip(w *sim.World) []string {
	c := w.ActiveCraft()
	if c == nil || !shouldShowChuteHUD(c) {
		return nil
	}
	stateLabel := c.ChuteState.String()
	switch c.ChuteState {
	case spacecraft.ChuteDeployed:
		stateLabel = v.theme.Primary.Render(stateLabel)
	case spacecraft.ChuteArmed:
		stateLabel = v.theme.Warning.Render(stateLabel)
	default:
		stateLabel = v.theme.Dim.Render(stateLabel)
	}
	vRel := physics.AirRelativeVelocity(c.State.R, c.State.V, c.Primary)
	var descentRate float64
	if rNorm := c.State.R.Norm(); rNorm > 0 {
		rHat := c.State.R.Scale(1 / rNorm)
		descentRate = -(vRel.X*rHat.X + vRel.Y*rHat.Y + vRel.Z*rHat.Z)
	}
	rateLabel := readout.Speed(descentRate)
	if vRel.Norm() >= sim.CrashVCritMps {
		rateLabel = v.theme.Alert.Render(
			fmt.Sprintf("%s (rel speed > %s = CRASH on contact)", readout.Speed(descentRate), readout.Speed(sim.CrashVCritMps)))
	}
	lines := []string{
		v.theme.Primary.Render("CHUTE"),
		fmt.Sprintf("  state:        %s", stateLabel),
		fmt.Sprintf("  descent rate: %s", rateLabel),
	}
	if c.ChuteState == spacecraft.ChuteStowed {
		lines = append(lines, v.theme.Dim.Render("  [space] arms the chute on a bare capsule"))
	}
	return lines
}

// landedPlaneNormalOK reports whether the ACTIVE vessel's own landed
// state defines a trustworthy orbital plane (ADR 0050 decision 8,
// extended by review r1 F3: the original guard only covered a TARGET's
// normal). While Landed, c.State.V IS the true co-rotation velocity
// (see craftOrbitNormalForRelativeIncl's own comment), so r x v here is
// exactly the same magnitude quantity a landed TARGET's rT x vT is
// (sim.targetPlaneNormalRelativeTo), not spacecraft.HeadingOrbitNormal's
// heading-rotated output, which crosses r against a unit direction
// rather than the actual co-rotation velocity and so has a different
// scale. Always true for a craft that isn't Landed (nothing here
// applies to a real orbit). Shared by both the depart: row (which reads
// spacecraft.HeadingOrbitNormal directly, not this function) and
// craftOrbitNormalForRelativeIncl's Δincl normal below, so the two rows
// withhold together at a pole instead of independently drifting.
func landedPlaneNormalOK(c *spacecraft.Spacecraft) bool {
	if !c.Landed {
		return true
	}
	n := c.State.R.Cross(c.State.V)
	omegaR := render.BodySpinOmegaWorld(c.Primary)
	omega := orbital.Vec3{X: omegaR.X, Y: omegaR.Y, Z: omegaR.Z}
	return orbital.PlaneNormalOK(n, omega.Norm(), c.Primary.RadiusMeters())
}

// craftOrbitNormalForRelativeIncl returns the orbital-plane normal used
// to compute a Δincl plane angle to a target, and whether it is
// trustworthy. While Landed there is no real orbit yet: the craft's
// actual State.V is always the due-east surface co-rotation velocity
// regardless of the commanded Heading Trim (ApplyHeadingTrim only
// rotates a *burn* direction, never the pre-ignition landed state), so
// reading c.State.R.Cross(c.State.V) directly would silently assume
// due-east even after the player has trimmed away from it. Route
// through spacecraft.HeadingOrbitNormal instead, which derives the
// plane an ascent lit NOW at the commanded heading would leave (ADR
// 0049 decision 11); it ticks live as the player warps on the pad
// because the pad's position (hence the local horizon frame) sweeps
// with the primary's rotation. Once airborne the real state vector is
// authoritative again: c.State.V then reflects whatever plane the
// craft actually flew into.
//
// ok is false when Landed at a pole (review r1 F3, landedPlaneNormalOK):
// HeadingOrbitNormal's own guard is an exact Norm() == 0 test that a
// real pole's floating-point residue never trips, which is why an
// unguarded Δincl wandered at the shipped North Pole preset.
//
// v0.42+ (ADR 0049 decisions 10-11).
func craftOrbitNormalForRelativeIncl(c *spacecraft.Spacecraft) (orbital.Vec3, bool) {
	if c.Landed {
		if !landedPlaneNormalOK(c) {
			return orbital.Vec3{}, false
		}
		spinAxisR := render.BodyRotationAxisWorld(c.Primary)
		spinAxis := orbital.Vec3{X: spinAxisR.X, Y: spinAxisR.Y, Z: spinAxisR.Z}
		if h, ok := spacecraft.HeadingOrbitNormal(c.State.R, spinAxis, c.HeadingTrim); ok {
			return h, true
		}
		return orbital.Vec3{}, false
	}
	return c.State.R.Cross(c.State.V), true
}

// deltaInclLabel renders a Δincl value the same way at every call site
// (review r1 F2): styled Warning past 30°, and tagged "(due east)" when
// dueEast is true: a landed vessel target's plane is its co-rotation
// state, which equals the due-east launch plane at every latitude
// (decision 7), not a real orbit, so the row says so. Before this, the
// TARGET chip's TargetCraft branch tagged it and the pad's
// landedInclHeadingRows didn't, so a player who only ever sees the
// compact TARGET chip at 140x40 (no Δincl at all) saw an untagged
// figure on the pad with no way to know it assumed due east. ok is
// false when the two normals don't define an angle (relativePlaneAngleDeg).
func (v *OrbitView) deltaInclLabel(nCraft, nTarget orbital.Vec3, dueEast bool) (string, bool) {
	di, ok := relativePlaneAngleDeg(nCraft, nTarget)
	if !ok {
		return "", false
	}
	label := readout.Angle(di)
	if di > 30 {
		label = v.theme.Warning.Render(label)
	}
	if dueEast {
		label += " (due east)"
	}
	return label, true
}

// relativePlaneAngleDeg is the plane angle between two orbital-plane
// normals, folded to [0, 90]: two coplanar orbits (normals parallel OR
// antiparallel, prograde vs retrograde in the same plane) both read
// 0°. ok is false when either normal is degenerate (zero vector).
// Shared by the TARGET chip's Δincl row and the pad's (ADR 0049
// decision 11).
func relativePlaneAngleDeg(nCraft, nTarget orbital.Vec3) (float64, bool) {
	if nCraft.Norm() == 0 || nTarget.Norm() == 0 {
		return 0, false
	}
	cos := nCraft.Dot(nTarget) / (nCraft.Norm() * nTarget.Norm())
	if cos > 1 {
		cos = 1
	} else if cos < -1 {
		cos = -1
	}
	ang := math.Acos(cos) * 180 / math.Pi
	return math.Min(ang, 180-ang), true
}

// unfoldedPlaneAngleDeg is the plane angle between two normals over
// the full [0, 180] range, unlike relativePlaneAngleDeg's fold to
// [0, 90]. ADR 0050 decision 1: the `depart:` row is an inclination
// like `incl:`, not a coplanarity test, so a normal nearly antiparallel
// to the reference reads close to 180°, not close to 0° (the ADR's own
// exoplanet pads read 61°..118°, which relativePlaneAngleDeg could
// never produce). ok is false when either normal is degenerate (zero
// vector).
func unfoldedPlaneAngleDeg(a, b orbital.Vec3) (float64, bool) {
	if a.Norm() == 0 || b.Norm() == 0 {
		return 0, false
	}
	cos := a.Dot(b) / (a.Norm() * b.Norm())
	if cos > 1 {
		cos = 1
	} else if cos < -1 {
		cos = -1
	}
	return math.Acos(cos) * 180 / math.Pi, true
}

// departReferenceNormal is ADR 0050 decision 2's reference plane for
// the `depart:` row: the orbital-plane normal of the world the craft
// is standing on or orbiting, i.e. primary's own orbit around ITS
// primary (orbital.OrbitNormalWorld(primary)), in world/ecliptic axes.
// Not the ecliptic itself: on a moon this is the moon's own orbit
// around its planet, which is the plane a departure to that planet (or
// beyond) actually wants. ok is false for a primary with no orbit (a
// system star): decision 2 says the normal is zero, so no row.
func departReferenceNormal(primary bodies.CelestialBody) (orbital.Vec3, bool) {
	ref := orbital.OrbitNormalWorld(primary)
	if ref.Norm() == 0 {
		return orbital.Vec3{}, false
	}
	return ref, true
}

// departRowHidden reports whether the `depart:` row can ever move for
// primary (ADR 0050 decision 3), and the angle eps that decides it: a
// pad's launch-plane normal precesses about primary's own spin axis as
// the pad rotates under warp, so if the reference normal sits on that
// axis (or its antipode) the angle to it is fixed no matter the
// heading or the hour. eps = unfoldedPlaneAngleDeg(spin axis,
// reference normal); the row is hidden when eps folds (parallel or
// antiparallel treated alike) to under 0.005 degrees. eps is also
// departSwing's own input, since the swing amplitude is set by exactly
// this angle.
func departRowHidden(spinAxis, refNormal orbital.Vec3) (eps float64, hidden bool) {
	eps, ok := unfoldedPlaneAngleDeg(spinAxis, refNormal)
	if !ok {
		return 0, true
	}
	folded := math.Min(eps, 180-eps)
	return eps, folded < 0.005
}

// departSwing returns the pad's `depart:` bounds (ADR 0050 decision 4):
// as the pad rotates under warp, a launch at inclination padIncl (the
// existing incl: row, fixed against the spin axis) reaches a departure
// angle that sweeps from low up to high, because the launch-plane
// normal precesses about the spin axis at (angular) radius padIncl
// while the reference normal sits eps away from that same axis. best
// is the low end: the closest a wait can bring the departure angle,
// mirroring the Inclination Floor idiom directly above it. Verified
// against the ADR's own table (both the closed form here and a
// 3600-sample sweep over a full rotation) before this function was
// written; see the ADR 0050 progress log, Slice 3.
func departSwing(padIncl, eps float64) (low, high, best float64) {
	low = math.Abs(padIncl - eps)
	high = padIncl + eps
	if high > 180 {
		high = 360 - high
	}
	return low, high, low
}

// targetLeadLabel renders World.TargetLeadAngleDeg's reading as a chip
// value (#287): phasing direction is the first decision of any
// rendezvous, and a bare signed number is exactly the kind of thing
// that's ambiguous in the seat at the moment it matters — so the sign
// is always paired with a plain "ahead"/"behind" word rather than left
// for the pilot to decode a convention. "—" when the reading isn't
// meaningful (different primary / no shared SOI, or a degenerate orbit)
// rather than a misleading number.
func targetLeadLabel(angleDeg float64, ok bool) string {
	if !ok {
		return "—"
	}
	switch {
	case angleDeg > 0:
		return fmt.Sprintf("%+.0f° (ahead)", angleDeg)
	case angleDeg < 0:
		return fmt.Sprintf("%+.0f° (behind)", angleDeg)
	default:
		return "0° (aligned)"
	}
}

// closestApproachRows computes the TCA/CA rows against the current
// relative target (craft or ghost) — shared by both TARGET chip
// branches so the approach math lives once.
func (v *OrbitView) closestApproachRows(w *sim.World, c *spacecraft.Spacecraft) []string {
	rT, vT, ok := w.TargetStateRelativeToActivePrimary()
	if !ok {
		return nil
	}
	active := orbital.Vec3State{R: c.State.R, V: c.State.V}
	target := orbital.Vec3State{R: rT, V: vT}
	mu := c.Primary.GravitationalParameter()
	// closestApproachHorizonSec (orbit_target_markers.go) — shared with
	// the map's ✕ marker so the chip's numbers and the marker's position
	// always describe the same encounter.
	tCA, distCA, _, err := planner.NextClosestApproach(active, target, c.Primary, mu, closestApproachHorizonSec)
	if err != nil {
		return nil
	}
	return []string{
		chipRow(readout.LabelTCA, readout.Countdown(time.Duration(tCA*float64(time.Second)))),
		chipRow(readout.LabelApproach, readout.Distance(distCA)),
	}
}

// buildSOIPassChip surfaces the always-on SOI Pass readout (ADR 0019): the
// upcoming foreign-SOI encounter of the live trajectory — independent of the
// Target slot. With no node planted it shows the single live pass (body,
// Perilune altitude or IMPACT, Time to Perilune). With a node planted it
// stacks the dual arc (ADR 0019 D): a `planned` line (the node-modified
// path's safe periapsis) and a `no-burn` line (the counterfactual Impact the
// burn corrects). Returns nil when there is no pass, or when the Pass Body is
// also the current body Target — the TARGET chip already covers it, so the
// readouts de-dupe into one (ADR 0019 E).
func (v *OrbitView) buildSOIPassChip(w *sim.World) []string {
	c := w.ActiveCraft()
	if c == nil || !w.CraftVisibleHere() {
		return nil
	}
	arc := v.cachedSOIPass(w)

	// The body the chip names: the planned pass when present (the path the
	// craft will fly), else the counterfactual/live pass.
	var body bodies.CelestialBody
	switch {
	case arc.plOK:
		body = arc.planned.Body
	case arc.cfOK:
		body = arc.counterfactual.Body
	default:
		return nil
	}

	// De-dupe with TARGET: if the player has targeted the very body the
	// pass crosses, the TARGET chip's peri/TCA rows already cover it.
	if w.Target.Kind == sim.TargetBody {
		sysT := w.System()
		if w.Target.BodyIdx > 0 && w.Target.BodyIdx < len(sysT.Bodies) &&
			sysT.Bodies[w.Target.BodyIdx].ID == body.ID {
			return nil
		}
	}

	nameStyle := lipgloss.NewStyle().Foreground(render.ColorFor(body)).Bold(true)
	lines := []string{
		v.theme.Primary.Render("SOI PASS"),
		chipRow("body:", nameStyle.Render(body.EnglishName)),
	}
	periValue := func(p sim.SOIPass) string {
		if p.Impact {
			return v.theme.Warning.Render("IMPACT")
		}
		return readout.Distance(p.PeriluneAltitude())
	}
	if arc.hasNodes {
		// Dual arc: planned (bright path) + no-burn (counterfactual). The
		// planned path's SOI-entry clock rides under it (ADR 0021 C: the
		// Entry glyph marks where, the chip carries when).
		if arc.plOK {
			lines = append(lines, chipRow("planned:", periValue(arc.planned)))
			if arc.planned.HasEntryTime {
				lines = append(lines, chipRow("  "+readout.LabelEntry, readout.Countdown(time.Duration(arc.planned.TimeToEntry*float64(time.Second)))))
			}
			lines = append(lines, chipRow("  "+readout.LabelPeri, readout.Countdown(time.Duration(arc.planned.TimeToPerilune*float64(time.Second)))))
		}
		if arc.cfOK {
			lines = append(lines, chipRow("no-burn:", periValue(arc.counterfactual)))
		}
		return lines
	}
	// Single live pass (no node planted). entry: is the predicted SOI-entry
	// clock — the ring crossing the Entry glyph marks (ADR 0021 C).
	if arc.counterfactual.HasEntryTime {
		lines = append(lines, chipRow(readout.LabelEntry, readout.Countdown(time.Duration(arc.counterfactual.TimeToEntry*float64(time.Second)))))
	}
	lines = append(lines,
		chipRow("perilune:", periValue(arc.counterfactual)),
		chipRow(readout.LabelTCA, readout.Countdown(time.Duration(arc.counterfactual.TimeToPerilune*float64(time.Second)))))
	return lines
}

// chipValueCol is the display column a chip row's value begins at, shared
// by the ORBIT and TARGET chips so the two line up when stacked in the same
// corner. The buildOrbitMetricsChip rows are hand-padded to this column.
const chipValueCol = 13

// orbitDirectionLabel renders the prograde/retrograde orbit-direction
// readout for an equatorial-frame inclination (radians). i > 90° means
// the orbit runs retrograde — against the primary's spin. This is the
// instrument that disambiguates a genuine orbit reversal from a
// projection / day-night-shading artifact near the disk edge (issue
// #63): the on-screen position can mislead, but the direction label is
// ground truth. Prograde is the unremarkable case (plain text);
// retrograde is flagged.
func (v *OrbitView) orbitDirectionLabel(incRad float64) string {
	if incRad > math.Pi/2 {
		return v.theme.Alert.Render("retrograde")
	}
	return "prograde"
}

// launchChipValueCol is buildLaunchChip's own value column, one wider
// than chipValueCol: every hand-formatted row in that chip (altitude:/
// vert:/horiz:/fpa:/orbit fpa:/TWR:/hold:/trim:/Ap:/Pe:/apo:/Δv→circ:/
// burn:) lands its value at column 14, not chipValueCol's 13 (item4-B
// review finding 5: the pad's heading:/incl:/Δincl: rows used plain
// chipRow and sat one column left of every sibling row in the same
// chip).
const launchChipValueCol = 14

// chipRow formats a "  label   value" telemetry row with the value pinned
// to chipValueCol regardless of label width, so a chip's values share one
// column instead of drifting per label. Padding is measured in display
// cells (lipgloss.Width), so multibyte labels like "Δincl:" and styled values
// align correctly where byte-counted %-Ns padding would not. Distance /
// duration / speed / angle formatting lives in internal/tui/readout (ADR
// 0049); this helper only lays the label and an already-formatted value
// out in one column.
func chipRow(label, value string) string {
	return chipRowAt(label, value, chipValueCol)
}

// chipRowAt is chipRow with an explicit value column, for a chip like
// buildLaunchChip's SURFACE whose hand-formatted rows already sit at a
// different column (launchChipValueCol) than the shared chipValueCol.
func chipRowAt(label, value string, col int) string {
	prefix := "  " + label
	pad := col - lipgloss.Width(prefix)
	if pad < 1 {
		pad = 1
	}
	return prefix + strings.Repeat(" ", pad) + value
}

// boxValueCol is value1's column (ADR 0051 decision 4, "two quantities
// per row"), matching the existing chipValueCol convention, shared by
// every box (chipRowAt's own convention, unaffected by this fix).
const boxValueCol = 13

// boxCols is one instrument box's column layout for its OWN chipRow2/
// chipRow3 calls: where the second (and third) cell's LABEL is pinned,
// and how far that cell's own value sits after its label. Fix note (the
// alignment bug this replaces): chipRow2/chipRow3 used to share ONE
// column set across all eight boxes, sized to the single widest first
// value anywhere on the HUD, every box inherited that width even when
// its own values were much shorter (TARGET's "371.6 Mm" paid for
// PROPELLANT's "3518 / 18872 m/s"), which is what pushed the TARGET/
// NAVIGATION boxes wide enough to eat into the launch view's canvas and
// hide the LUT crown glyph (TestLaunchTowerRendersAtPad). Per box
// instead (re-grill: "per-box column sized to fit that box's widest
// first value"): label2/label3 are pinned to a column sized for THAT
// box's own typical first/second cell width, with at least one column
// of daylight; an unusually wide value for that box still pushes the
// next label right by at least one space (chipCellAt's own clamp)
// rather than colliding, it just isn't the box's normal-case column.
type boxCols struct {
	label2, gap2 int
	label3, gap3 int // chipRow3 only; zero when a box never uses chipRow3
}

var (
	// ENGINE: value1 is the throttle row ("100% idle" .. "100% ● FIRING
	// 59m59s", ~20 cells); label2 is always "mode:" (5).
	engineCols = boxCols{label2: 35, gap2: 7}
	// PROPELLANT: value1's widest row is the Δv pair ("18872 / 99999
	// m/s", ~18 cells); label2's widest text is "Δv→circ:" (8).
	propellantCols = boxCols{label2: 33, gap2: 10}
	// GUIDANCE: value1's widest row is hold: ("Target Prograde
	// (TARGET)", ~24 cells, the common target-relative case. The rarer
	// "Surface Retrograde (SURFACE)" pushes nav: right rather than
	// colliding); label2's widest text is "orbit fpa:" (10).
	guidanceCols = boxCols{label2: 39, gap2: 12}
	// NAVIGATION: value1's widest common row is incl:/depart: ("28.61°
	// (min 28.61°)", ~20 cells); label2's widest text is "period:" (7).
	// label3 (the depart:/e:/dir: row only) follows e:'s own fixed-width
	// value (%.4f, always 6 cells) after label2's cell.
	navigationCols = boxCols{label2: 35, gap2: 9, label3: 52, gap3: 6}
	// TARGET: value1's widest common row is range:/Ap: (short distances,
	// ~10 cells); label2's widest text is "approach:" (9). label3
	// follows closing:'s own widest common value ("+3639.71 m/s", ~12
	// cells) after label2's cell.
	targetCols = boxCols{label2: 25, gap2: 11, label3: 50, gap3: 7}
)

// chipRow2 formats a row carrying two labelled quantities (ADR 0051
// decision 4): the first cell via chipRowAt (value1 pinned to
// boxValueCol), the second via chipCellAt (label2 pinned to cols'
// label2, value2 pinned cols.gap2 cells after it), so two rows in the
// same box whose first values differ wildly in width still start their
// second LABEL at the same screen column, which is what makes a box's
// second cells read as one column rather than drifting per row. label2
// == "" means this row has nothing in its second cell (a dash row with
// no sibling quantity, e.g. ENGINE's bare node: row with only a dash
// value): the row then reads exactly as chipRowAt's single-value form,
// with no trailing padding.
func chipRow2(cols boxCols, label1, value1, label2, value2 string) string {
	row := chipRowAt(label1, value1, boxValueCol)
	if label2 == "" {
		return row
	}
	return chipCellAt(row, label2, value2, cols.label2, cols.gap2)
}

// chipRow3 is chipRow2 extended to a third labelled quantity, for the
// three-per-row TARGET cells (decision 11: range/closing/rel,
// Ap/Pe/incl) and NAVIGATION's depart:/e:/dir: row (decision 14). label3
// == "" drops the third cell, matching chipRow2's own empty-label
// convention.
func chipRow3(cols boxCols, label1, value1, label2, value2, label3, value3 string) string {
	row := chipRow2(cols, label1, value1, label2, value2)
	if label3 == "" {
		return row
	}
	return chipCellAt(row, label3, value3, cols.label3, cols.gap3)
}

// chipCellAt appends one labelled quantity onto an already-formatted
// row: the LABEL is pinned to startCol, measured in display cells
// (lipgloss.Width, never byte-counted %-Ns padding, so multibyte labels
// and already-styled/ANSI-wrapped row content still line up); a row
// already wider than startCol (an unusually long earlier cell) still
// gets pushed right by at least one space rather than colliding with
// what came before. The VALUE is then pinned valueGap cells after the
// label's own start, not a separately fixed absolute column, so every
// row sharing this cell's label column and gap also shares the value's
// column, whatever that particular row's label text happens to be.
func chipCellAt(row, label, value string, startCol, valueGap int) string {
	pad := startCol - lipgloss.Width(row)
	if pad < 1 {
		pad = 1
	}
	row += strings.Repeat(" ", pad) + label
	valPad := valueGap - lipgloss.Width(label)
	if valPad < 1 {
		valPad = 1
	}
	return row + strings.Repeat(" ", valPad) + value
}
