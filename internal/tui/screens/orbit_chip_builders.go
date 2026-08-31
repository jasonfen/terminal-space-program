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
)

// This file holds the chip builders transplanted from renderHUD's
// per-block code (ADR 0010 / v0.13 slice 2). Each returns the chip's
// styled lines (a bare colored header + rows) or nil when the block isn't
// contextually relevant — the "relevant" half of the render rule. The old
// section() divider is dropped: a chip's header doubles as its label.
// Arithmetic and labels mirror the originals so the readouts are
// unchanged; only the placement (canvas corner vs. tall column) differs.

// assembleChips gathers every relevant + enabled Chip for the current
// world state, in composite order. Top-left holds the pinned VESSEL core
// plus the phase-transient stack; top-right holds Orbit metrics with the
// Target readout stacked beneath it; Stages is bottom-left and Nodes is
// bottom-right (above the navball). Declutter is honoured inside
// chipEnabled, so a decluttered frame returns no chips.
func (v *OrbitView) assembleChips(w *sim.World) []builtChip {
	var chips []builtChip
	// Pinned core telemetry — top of the top-left stack. Unlike every
	// other chip it is always rendered: never settings-toggled (core
	// telemetry is fixed, ADR 0010) and never hidden by declutter — F2
	// must not be able to hide fuel/Δv mid-burn. v0.13 playtest move:
	// VESSEL/PROPELLANT left the right-hand column to live on the canvas.
	if lines := v.buildVesselChip(w); lines != nil {
		chips = append(chips, builtChip{corner: cornerTopLeft, lines: lines, priority: chipPriorityCore})
	}
	// MEETING PLAN (ADR 0045 S6, #399): the picker holds keyboard focus
	// while open (app.go's key intercept claims ←/→/↑/↓/Enter/Esc before
	// they can reach camera pan or anything else), so unlike every other
	// chip it bypasses chipEnabled — a modal the player just summoned with
	// K must not silently vanish under F2 declutter while it's still
	// eating their keystrokes. chipPriorityForced (not Core) keeps it
	// below VESSEL/ORBIT in a genuine overflow, but never dropped for
	// space like an ordinary contextual chip.
	if lines := v.buildMeetingPickerChip(); lines != nil {
		chips = append(chips, builtChip{corner: cornerTopLeft, lines: lines, priority: chipPriorityForced})
	}
	add := func(id settings.Chip, corner chipCorner, lines []string) {
		if lines == nil || !v.chipEnabled(id) {
			return
		}
		chips = append(chips, builtChip{id: id, corner: corner, lines: lines})
	}
	// addPriority is add's twin for the handful of chips that must never
	// be silently dropped by admitChipsByBudget when a corner overflows
	// (#328/#334) — see the chipPriority* doc comment in orbit_chips.go.
	addPriority := func(id settings.Chip, corner chipCorner, lines []string, priority int) {
		if lines == nil || !v.chipEnabled(id) {
			return
		}
		chips = append(chips, builtChip{id: id, corner: corner, lines: lines, priority: priority})
	}
	// PROXIMITY (ADR 0043) is the close-range view's own instrument panel,
	// so inside that view it sits directly under VESSEL, ahead of
	// everything else in the top-left stack. Ordering is load-bearing:
	// admitChipsByBudget drops later normal-priority chips first, so at
	// 80×24 the readout the player is flying the last kilometres on wins
	// the space over the transient chips behind it. Nil in every other
	// ViewMode — on the map, the TARGET chip already carries these
	// numbers and a second copy would be pure clutter.
	add("", cornerTopLeft, v.buildProximityChip(w))
	// The current goal sits directly under the pinned VESSEL chip — "who I am"
	// then "what I'm doing" in the top-left status corner (ADR 0025 / Slice 5).
	add(settings.ChipMissions, cornerTopLeft, v.buildMissionsChip(w))
	// Top-left transient stack (stacking order = listed order, downward).
	// The in-flight ● BURNS readout used to live here; v0.16 folds it into
	// the bottom-right NODES chip (a live burn is the firing head of the
	// burn schedule). See the force-show path below.
	add(settings.ChipFrameTransition, cornerTopLeft, v.buildFrameTransitionChip(w))
	add(settings.ChipCapture, cornerTopLeft, v.buildCaptureChip(w))
	add(settings.ChipLaunch, cornerTopLeft, v.buildLaunchChip(w))
	add(settings.ChipDescent, cornerTopLeft, v.buildDescentChip(w))
	// DESCENDING (issue #348 §4): a one-line pointer at the launch/surface
	// jump key, offered the moment the active vessel's trajectory is
	// forecast to reach the ground — the map-screen mirror of the
	// CLOSE RANGE hint below (same "teach the key once, then get out of
	// the way" always-on + self-limiting treatment). Placed beside
	// DESCENT/CHUTE — the other own-craft-state chips — rather than in
	// the Target-oriented top-right stack.
	add("", cornerTopLeft, v.buildLaunchHintChip(w))
	add(settings.ChipChute, cornerTopLeft, v.buildChuteChip(w))
	add(settings.ChipAttitude, cornerTopLeft, v.buildAttitudeChip(w))
	// SESSION moments (v0.27 S6 / ADR 0034): join/leave/sync events as
	// a transient top-left chip. Always-on when events are fresh (empty
	// id — moments are too short-lived to warrant a Settings toggle);
	// declutter still clears it via the empty-id path.
	add("", cornerTopLeft, v.buildSessionEventsChip(w))
	// RENDEZVOUS (v0.29 S2): the persistent Rendezvous Warp surface —
	// join prompt / armed-waiting / coasting readout. Always-on while
	// the state machine is live (empty id): the join prompt is the
	// anti-overlook affordance and the coast readout carries the cancel
	// key; F2 declutter still clears it like SESSION.
	add("", cornerTopLeft, v.buildRendezvousChip(w))
	// TIME LOCK (ADR 0037 §3): the minimal standing line for a plain
	// proximity lock — no agreement, so nothing else on screen would say
	// the player's warp is being held. Always-on for the same reason
	// RENDEZVOUS is: it explains a constraint the player can't otherwise
	// see. Nil inside an agreement, where the chip above says it better.
	add("", cornerTopLeft, v.buildTimeLockChip(w))
	// DOCKED (ADR 0038 S4): the rider-view standing block — unconditional
	// while one of this player's craft rides in another player's stack
	// (names the ride + the exits), with #253's owner-away line folded in
	// as an extra row rather than a second surface. Always-on like
	// RENDEZVOUS (empty id); F2 declutter still clears it. #328: this is
	// the rider's only surviving route to [J] request control / [U]
	// undock once absorbed into another player's stack, so it carries
	// chipPriorityForced — admitChipsByBudget must never silently drop it
	// for space the way it dropped every ordinary chip ahead of it.
	addPriority("", cornerTopLeft, v.buildDockGuestChip(w), chipPriorityForced)
	// COMMS link status for the active probe (ADR 0027 / C2-7), beneath the
	// vessel-state readouts. Force-shown while a just-blocked command is
	// flashing (CommBlockedFlash) — bypassing the toggle + declutter — so the
	// NO SIGNAL reason for a refused command is never hidden; otherwise it
	// honours the toggle like any chip.
	if lines := v.buildCommsChip(w); lines != nil {
		if _, flashing := w.CommBlockedFlash(); flashing || v.chipEnabled(settings.ChipComms) {
			chips = append(chips, builtChip{id: settings.ChipComms, corner: cornerTopLeft, lines: lines})
		}
	}
	// Top-right stack: Orbit metrics on top, the Target readout beneath it
	// (append order = top-to-bottom). Orbit metrics is always-on (empty id):
	// the current orbit (apo/peri/incl) is never user-hideable from the
	// Settings screen, mirroring the always-on ● BURNS readout — both are
	// too load-bearing to toggle off. F2 declutter still clears them.
	addPriority("", cornerTopRight, v.buildOrbitMetricsChip(w), chipPriorityCore)
	// PROJECTED ORBIT sits to the LEFT of the always-on ORBIT readout (issue
	// #63 follow-up) so current + projected show together during a burn
	// without growing the top-right column's height — leaving vertical room
	// for TARGET to clear the bottom-right NODES chip. Toggleable, unlike the
	// load-bearing live ORBIT beside it. leftOfPrev falls back to normal
	// stacking when ORBIT is suppressed (e.g. ascent), so it's never orphaned.
	if lines := v.buildProjectedOrbitChip(w); lines != nil && v.chipEnabled(settings.ChipProjectedOrbit) {
		chips = append(chips, builtChip{id: settings.ChipProjectedOrbit, corner: cornerTopRight, lines: lines, leftOfPrev: true})
	}
	// CLOSE RANGE (ADR 0043): a one-line pointer at the Proximity View
	// jump key, offered when an approach crosses inside the range at which
	// the game already treats two vessels as flying together. It sits
	// directly above TARGET because it is a fact ABOUT the target — the
	// next thing to know after the range readout that triggered it — and
	// always-on (empty id) for the same reason the RENDEZVOUS join prompt
	// is: a chip that teaches a key must not be toggle-able into silence
	// before it has been read once. Self-limiting rather than standing:
	// sim's crossing state machine retires it the moment the player acts,
	// and it never renders inside the view it advertises.
	add("", cornerTopRight, v.buildProximityHintChip(w))
	add(settings.ChipTarget, cornerTopRight, v.buildTargetChip(w))
	// SOI PASS sits beneath TARGET — the upcoming encounter of the live
	// path, always-on and Target-independent (ADR 0019). De-dupes with
	// TARGET inside the builder when they name the same body.
	add(settings.ChipSOIPass, cornerTopRight, v.buildSOIPassChip(w))
	// Remaining fixed corners.
	add(settings.ChipStages, cornerBottomLeft, v.buildStagesChip(w))
	// CHAT stacks under STAGES, its own corner slot away from the
	// session moments (ADR 0035 §2). Always-on like SESSION — a
	// coordination line must not be togglable into silence.
	add("", cornerBottomLeft, v.buildChatChip(w))
	// NODES (bottom-right) now also carries any in-flight burn as its
	// firing head (v0.16). A live burn is safety-critical, so when one is
	// in flight the chip force-shows — bypassing both the ChipNodes
	// Settings toggle and F2 declutter — so it can never be hidden.
	// #293 extends the same force-show rationale to the staleness
	// hazard: once more than one node is queued on the ACTIVE craft,
	// every node behind the first was computed against an orbit that no
	// longer exists once the first one fires, so the count must be
	// visible the same way a live burn is. #333: this is strictly
	// per-craft (activeCraftQueuedNodes), not the old fleet-wide sum — a
	// different craft's queue firing doesn't stale the one this player
	// is watching, so a small constellation with one node per vessel no
	// longer force-shows a chip the player explicitly decluttered. With
	// ≤1 node queued on the active craft and nothing burning, the chip
	// honours the toggle + declutter like any chip.
	if lines := v.buildNodesChip(w); lines != nil {
		forced := v.anyActiveBurn(w) || activeCraftQueuedNodes(w) > 1
		if forced || v.chipEnabled(settings.ChipNodes) {
			// #334: only a genuinely FORCED render (bypassing the toggle)
			// gets chipPriorityForced's "never drop for space, clamp
			// instead" guarantee. A merely toggle-enabled NODES chip is a
			// normal-priority chip like any other — if it can't fit, the
			// player's own toggle choice is what loses, not a silent
			// safety-critical readout.
			priority := chipPriorityNormal
			if forced {
				priority = chipPriorityForced
			}
			chips = append(chips, builtChip{id: settings.ChipNodes, corner: cornerBottomRight, lines: lines, priority: priority})
		}
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
			resumed := "◇ resumed — " + compactDuration(e.Elapsed) + " ran while you were away"
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
		lines := []string{
			v.theme.Primary.Render("RENDEZVOUS"),
			"  coasting with " + aw.RendezvousHandle + " to the encounter",
			chipRow("τ in:", compactDuration(aw.T.Sub(now))),
		}
		if arm := w.RendezvousArm; arm != nil {
			lines = append(lines, chipRow("committed:", formatRangeM(arm.CommittedCA)))
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
			lines = append(lines, chipRow("approach:", formatRangeM(w.RendezvousApproachM)))
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
			who := "you are " + compactDuration(wt.AheadBy) + " ahead of " + arm.Handle
			fix := "they must Sync to you"
			if wt.AheadBy < 0 {
				who = arm.Handle + " is " + compactDuration(-wt.AheadBy) + " ahead of you"
				fix = "Sync to rejoin"
			}
			status = v.theme.Alert.Render("  cannot couple — " + who + " — " + fix)
		case sim.RendezvousWaitSelf:
			// #260: same misattribution family — the partner DID join, and
			// the viewer's own Sync or node-chase is what defers the coast.
			// Own the wait instead of blaming them or advising a Sync.
			status = v.theme.Warning.Render("  your Auto-Warp is running — coast starts when it releases")
		}
		lines := []string{
			v.theme.Primary.Render("RENDEZVOUS"),
			status,
			chipRow("τ in:", compactDuration(arm.Tau.Sub(now))),
			chipRow("CA:", formatRangeM(arm.CommittedCA)),
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
// "agreed, no plan yet" state (#400), and compactDuration clamps a
// negative duration to zero, so rendering these rows unconditionally
// showed a fabricated "τ in: 0s / CA: 0 m" — an imminent zero-metre
// encounter that was never computed — to the player deciding whether to
// accept.
func rendezvousInviteEncounterLines(inv *sim.RendezvousInvite, now time.Time) []string {
	if inv.Tau.IsZero() {
		return nil
	}
	return []string{
		chipRow("τ in:", compactDuration(inv.Tau.Sub(now))),
		chipRow("CA:", formatRangeM(inv.CA)),
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
		return []string{theme.Dim.Render("  CA " + formatRangeM(arm.CommittedCA) + " ↘ shrinking")}
	case arm.CommittedCA > arm.PrevCommittedCA:
		return []string{
			theme.Alert.Render("  ⚠ CA growing each pass (" + formatRangeM(arm.PrevCommittedCA) + " → " + formatRangeM(arm.CommittedCA) + ")"),
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

// anyActiveBurn reports whether any craft in the slate has an in-flight
// finite/planted burn (ActiveBurn). Drives the NODES chip force-show
// (v0.16) so a live burn is never hidden by the toggle or declutter.
func (v *OrbitView) anyActiveBurn(w *sim.World) bool {
	for _, c := range w.Crafts {
		if c != nil && c.ActiveBurn != nil {
			return true
		}
	}
	return false
}

// buildMissionsChip is the in-flight mission checklist chip (ADR 0025
// §"Player surface" / Slice 5). A one-liner: the active mission's name plus
// its current objective and N/M progress, so the player always sees "what
// now" without opening the missions screen (which carries the full ladder +
// hint text). On a mission failure it flashes "✗ <name> failed: <reason>" for
// a few wall-clock seconds (World.MissionFailFlash) before advancing to the
// next mission. Returns nil when no mission is active and nothing is flashing.
// Honours the Settings toggle + F2 declutter like any chip (no force-show —
// a failed mission isn't safety-critical the way a live burn is).
func (v *OrbitView) buildMissionsChip(w *sim.World) []string {
	flash, flashing := w.MissionFailFlash()
	return v.missionChipLines(flash, flashing, w.ActiveMission())
}

// missionChipLines is the pure content selector behind buildMissionsChip,
// split out so both the (World-armed) failure flash and the active-mission
// forms are unit-testable without a live World. A live flash takes precedence
// over the active mission — the mission that just failed is more urgent to
// surface than the next one in the ladder.
func (v *OrbitView) missionChipLines(flash string, flashing bool, m *missions.Mission) []string {
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
	// Tutorial steps surface their instruction ("Press [v] …") in-flight so the
	// player learns the control without opening the missions screen (Slice 7
	// playtest feedback). Challenge steps stay the clean one-liner.
	if m.Program == missions.ProgramTutorial && obj.Description != "" {
		lines = append(lines, v.theme.Dim.Render("    "+obj.Description))
	}
	return lines
}

// buildAttitudeChip surfaces the held attitude / nav mode / engine mode /
// manual-burn state. Always relevant for a visible craft (the old block
// dropped the hold row during ascent to save column height; a corner chip
// doesn't compete for that height, so it shows the full set).
func (v *OrbitView) buildAttitudeChip(w *sim.World) []string {
	c := w.ActiveCraft()
	if c == nil || !w.CraftVisibleHere() {
		return nil
	}
	manualState := "idle"
	if c.ManualBurn != nil {
		elapsed := w.Clock.SimTime.Sub(c.ManualBurn.StartTime).Seconds()
		manualState = fmt.Sprintf(v.theme.Warning.Render("● firing T+%.1fs"), elapsed)
	}
	return []string{
		v.theme.Primary.Render("ATTITUDE"),
		fmt.Sprintf("  nav:     %s", w.NavMode),
		fmt.Sprintf("  hold:    %s", c.AttitudeMode.String()),
		fmt.Sprintf("  engine:  %s", c.EngineMode.String()),
		fmt.Sprintf("  manual:  %s", manualState),
	}
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

// activeBurnLines renders the in-flight burn entries across the whole
// craft slate — mode, Δv remaining, and a T-countdown (or a STALLED
// warning) — each as a ● firing line in the warning style so it reads as
// safety-critical. Returns nil when nothing is burning. v0.16 folds this
// into buildNodesChip as the firing head of the burn schedule (it was the
// standalone ● BURNS chip); walking all crafts still means a burn on a
// non-active vessel can't sneak by.
func (v *OrbitView) activeBurnLines(w *sim.World) []string {
	var lines []string
	for i, c := range w.Crafts {
		if c == nil || c.ActiveBurn == nil {
			continue
		}
		ab := c.ActiveBurn
		remaining := ab.EndTime.Sub(w.Clock.SimTime).Seconds()
		if remaining < 0 {
			remaining = 0
		}
		tag := fmt.Sprintf("vessel %d", i+1)
		if i == w.ActiveCraftIdx {
			tag += " (active)"
		}
		if c.BurnStalled() {
			lines = append(lines,
				v.theme.Warning.Render(fmt.Sprintf("  ● %s — %s, Δv %.0f m/s", tag, ab.Mode.String(), ab.DVRemaining)),
				v.theme.Warning.Render("    ⚠ STALLED — stage to resume (x to cancel)"),
			)
		} else {
			lines = append(lines,
				v.theme.Warning.Render(fmt.Sprintf("  ● %s — %s, Δv %.0f m/s, T-%.0fs",
					tag, ab.Mode.String(), ab.DVRemaining, remaining)),
			)
		}
	}
	return lines
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
		when = formatCountdown(dur)
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
		fmt.Sprintf("  primary:    %s", cap.Primary.EnglishName),
	}
	if cap.Approximate {
		dirLabel := v.theme.Warning.Render("prograde")
		if cap.RetrogradeCapture {
			dirLabel = v.theme.Alert.Render("retrograde")
		}
		lines = append(lines,
			fmt.Sprintf("  approach:   %.0f m/s relative", cap.ApproachSpeed),
			fmt.Sprintf("  direction:  %s capture predicted", dirLabel),
			v.theme.Dim.Render("  (intercept too central for orbit-element preview)"),
		)
		return lines
	}
	primaryR := cap.Primary.RadiusMeters()
	incDeg := cap.Inclination * 180 / math.Pi
	incLabel := fmt.Sprintf("%.1f°", incDeg)
	switch {
	case cap.Hyperbolic:
		incLabel = v.theme.Alert.Render("escape — capture failed")
	case incDeg > 90:
		incLabel = v.theme.Alert.Render(incLabel + " (retrograde)")
	case incDeg > 30:
		incLabel = v.theme.Warning.Render(incLabel)
	}
	lines = append(lines, fmt.Sprintf("  inclin.:    %s", incLabel))
	if !cap.Hyperbolic {
		lines = append(lines,
			fmt.Sprintf("  Ap:         %.0f km alt", (cap.ApoapsisM-primaryR)/1000),
			fmt.Sprintf("  Pe:         %.0f km alt", (cap.PeriapsisM-primaryR)/1000),
		)
	}
	return lines
}

// buildLaunchChip is the ascent instrument cluster (altitude / vertical &
// horizontal velocity / flight-path angle / TWR / SAS / trim plus the live
// ap/pe/Δv→circ prediction). Returns nil when the craft isn't ascending.
// Transplanted verbatim from renderHUD's LAUNCH block; the ascent-trend
// cache (v.ascentTrend*) is mutated here exactly as before.
func (v *OrbitView) buildLaunchChip(w *sim.World) []string {
	c := w.ActiveCraft()
	if c == nil || !shouldShowLaunchHUD(c) {
		return nil
	}
	omegaRender := render.BodySpinOmegaWorld(c.Primary)
	omega := orbital.Vec3{X: omegaRender.X, Y: omegaRender.Y, Z: omegaRender.Z}
	vRel := c.State.V.Sub(omega.Cross(c.State.R))
	rNorm := c.State.R.Norm()
	var vVert, vHoriz, fpaDeg, fpaOrbitDeg float64
	hasFPA := false
	hasFPAOrbit := false
	if rNorm > 0 {
		rHat := c.State.R.Scale(1 / rNorm)
		vVert = vRel.X*rHat.X + vRel.Y*rHat.Y + vRel.Z*rHat.Z
		vHorizVec := vRel.Sub(rHat.Scale(vVert))
		vHoriz = vHorizVec.Norm()
		if vRel.Norm() > 1.0 {
			fpaDeg = math.Atan2(vVert, vHoriz) * 180 / math.Pi
			hasFPA = true
		}
		vOrbit := c.State.V
		if vOrbit.Norm() > 1.0 {
			vVertOrbit := vOrbit.X*rHat.X + vOrbit.Y*rHat.Y + vOrbit.Z*rHat.Z
			vHorizOrbit := vOrbit.Sub(rHat.Scale(vVertOrbit)).Norm()
			fpaOrbitDeg = math.Atan2(vVertOrbit, vHorizOrbit) * 180 / math.Pi
			hasFPAOrbit = true
		}
	}
	twrLabel := "—"
	if c.Thrust > 0 && c.TotalMass() > 0 {
		gSurface := c.Primary.GravitationalParameter() / (c.Primary.RadiusMeters() * c.Primary.RadiusMeters())
		twr := c.Thrust * c.EffectiveThrottle() / (c.TotalMass() * gSurface)
		twrLabel = fmt.Sprintf("%.2f", twr)
		if twr < 1.0 {
			twrLabel = v.theme.Alert.Render(twrLabel + " (will not lift)")
		}
	}
	altAGL := c.Altitude()
	altLabel := fmt.Sprintf("%.0f m", nzero(altAGL, 0))
	if altAGL >= 1000 {
		altLabel = fmt.Sprintf("%.2f km", altAGL/1000)
	}
	sasLabel := c.AttitudeMode.String()
	trimDeg := c.PitchTrim * 180 / math.Pi
	trimLabel := fmt.Sprintf("%+.1f°", nzero(trimDeg, 1))
	if math.Abs(trimDeg) > 0.05 {
		trimLabel = v.theme.Warning.Render(trimLabel)
	}
	fpaLabel := "—"
	if hasFPA {
		fpaLabel = fmt.Sprintf("%.0f° (90 = up, 0 = horiz)", nzero(fpaDeg, 0))
	}
	fpaOrbitLabel := "—"
	if hasFPAOrbit {
		fpaOrbitLabel = fmt.Sprintf("%.0f° (inertial)", nzero(fpaOrbitDeg, 0))
	}
	lines := []string{
		v.theme.Primary.Render("SURFACE"),
		fmt.Sprintf("  altitude:   %s", altLabel),
		fmt.Sprintf("  v_vert:     %.1f m/s", nzero(vVert, 1)),
		fmt.Sprintf("  v_horiz:    %.0f m/s (surface-rel)", vHoriz),
		fmt.Sprintf("  fpa:        %s", fpaLabel),
		fmt.Sprintf("  fpa_orbit:  %s", fpaOrbitLabel),
		fmt.Sprintf("  twr:        %s", twrLabel),
		fmt.Sprintf("  sas:        %s", sasLabel),
		fmt.Sprintf("  trim:       %s", trimLabel),
	}
	mu := c.Primary.GravitationalParameter()
	primaryR := c.Primary.RadiusMeters()
	frame := orbital.ReferenceFrameForPrimary(c.Primary)
	el := orbital.ElementsFromStateInFrame(c.State.R, c.State.V, mu, frame)
	var (
		apoAlt, periAlt float64
		apoFinite       bool
	)
	if !math.IsNaN(el.A) && !math.IsInf(el.A, 0) && el.A > 0 && el.E < 1 {
		apoAlt = el.Apoapsis() - primaryR
		periAlt = el.Periapsis() - primaryR
		apoFinite = true
	}
	inclLabel := "—"
	inclRowLabel := "incl.:      "
	if !math.IsNaN(el.I) && !math.IsInf(el.I, 0) {
		inclLabel = fmt.Sprintf("%.2f°", el.I*180/math.Pi)
	}
	if c.Landed {
		inclRowLabel = "launch lat: "
		inclLabel = fmt.Sprintf("%.1f° (locked)", c.LaunchLatDeg)
	}
	apLabel, peLabel, ttaLabel, dvCircLabel, tBurnLabel := "—", "—", "—", "—", "—"
	trendLabel := ""
	var dvCirc float64
	// While Landed the craft sits at the apoapsis of its co-rotation
	// pseudo-orbit (apoapsis ≈ the launch radius), so apoAlt and rApo hover
	// at exactly zero and the apoAlt>0 / rApo>primaryR gates flip on
	// numerical noise tick-to-tick — flashing ap / t_to_apo / Δv→circ
	// between a value and "—". The pad pseudo-orbit isn't a real orbit, so
	// suppress these predictions until the craft actually lifts off; the
	// pad cares about TWR / launch-lat / SAS, which render regardless.
	if apoFinite && !c.Landed {
		apLabel = formatAltKm(apoAlt)
		peLabel = formatAltKm(periAlt)
		now := w.Clock.SimTime
		if v.ascentTrendCraft == c && !v.ascentTrendTime.IsZero() {
			dt := now.Sub(v.ascentTrendTime).Seconds()
			if dt > 1e-6 {
				rate := (el.Apoapsis() - v.ascentTrendApoM) / dt
				switch {
				case rate > 1.0:
					trendLabel = " (climbing)"
				case rate < -1.0:
					trendLabel = " (falling)"
				default:
					trendLabel = " (steady)"
				}
			}
		}
		v.ascentTrendCraft = c
		v.ascentTrendApoM = el.Apoapsis()
		v.ascentTrendTime = now
		if apoAlt > 0 {
			ttaSec := orbital.TimeToApoapsis(orbital.Vec3State{R: c.State.R, V: c.State.V}, mu)
			if ttaSec > 0 {
				ttaLabel = formatDurationShort(ttaSec)
			}
		}
		rApo := el.Apoapsis()
		if rApo > primaryR && el.A > 0 {
			vAtApo := math.Sqrt(mu * (2/rApo - 1/el.A))
			vCircAtApo := math.Sqrt(mu / rApo)
			dvCirc = vCircAtApo - vAtApo
			if dvCirc > 0 {
				dvCircLabel = fmt.Sprintf("%.0f m/s (impulsive)", dvCirc)
			}
		}
	} else {
		v.ascentTrendCraft = nil
	}
	if dvCirc > 0 && c.Thrust > 0 && c.TotalMass() > 0 {
		thrust := c.Thrust * c.EffectiveThrottle()
		if thrust <= 0 {
			thrust = c.Thrust
		}
		tBurnSec := dvCirc * c.TotalMass() / thrust
		tBurnLabel = formatDurationShort(tBurnSec)
	}
	lines = append(lines,
		fmt.Sprintf("  ap:         %s%s", apLabel, trendLabel),
		fmt.Sprintf("  pe:         %s", peLabel),
		fmt.Sprintf("  %s%s", inclRowLabel, inclLabel),
		fmt.Sprintf("  t_to_apo:   %s", ttaLabel),
		fmt.Sprintf("  Δv→circ:    %s", dvCircLabel),
		fmt.Sprintf("  t_burn:     %s", tBurnLabel),
	)
	if apoFinite && !c.Landed && apoAlt > launchMissionFloorM {
		orbitStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#3DDC84")).Bold(true)
		lines = append(lines, "  "+orbitStyle.Render("● ORBIT READY — coast to ap, press C to plant circularise"))
	}
	if apoFinite && !c.Landed {
		if progress := launchMissionProgress(w, c, periAlt); progress != "" {
			lines = append(lines, "  "+progress)
		}
	}
	return lines
}

// buildDescentChip is the airless-body terminal-approach cluster
// (altitude / v_vert / v_horiz / fpa / twr / sas). Returns nil unless the
// craft is in a powered descent. Mutually exclusive with the LAUNCH chip
// via the same Atmosphere gate the originals used.
func (v *OrbitView) buildDescentChip(w *sim.World) []string {
	c := w.ActiveCraft()
	if c == nil || !shouldShowDescentHUD(c) {
		return nil
	}
	altAGL := c.Altitude()
	omegaRender := render.BodySpinOmegaWorld(c.Primary)
	omega := orbital.Vec3{X: omegaRender.X, Y: omegaRender.Y, Z: omegaRender.Z}
	vRel := c.State.V.Sub(omega.Cross(c.State.R))
	rNorm := c.State.R.Norm()
	var vVert, vHoriz, fpaDeg float64
	hasFPA := false
	if rNorm > 0 {
		rHat := c.State.R.Scale(1 / rNorm)
		vVert = vRel.X*rHat.X + vRel.Y*rHat.Y + vRel.Z*rHat.Z
		vHorizVec := vRel.Sub(rHat.Scale(vVert))
		vHoriz = vHorizVec.Norm()
		if vRel.Norm() > 1.0 {
			fpaDeg = math.Atan2(vVert, vHoriz) * 180 / math.Pi
			hasFPA = true
		}
	}
	twrLabel := "—"
	if c.Thrust > 0 && c.TotalMass() > 0 {
		gSurface := c.Primary.GravitationalParameter() / (c.Primary.RadiusMeters() * c.Primary.RadiusMeters())
		twr := c.Thrust * c.EffectiveThrottle() / (c.TotalMass() * gSurface)
		twrLabel = fmt.Sprintf("%.2f", twr)
		if twr < 1.0 {
			twrLabel = v.theme.Alert.Render(twrLabel + " (can't hover)")
		}
	}
	altLabel := fmt.Sprintf("%.0f m", nzero(altAGL, 0))
	if altAGL >= 1000 {
		altLabel = fmt.Sprintf("%.2f km", altAGL/1000)
	}
	fpaLabel := "—"
	if hasFPA {
		fpaLabel = fmt.Sprintf("%.0f° (0 = horiz, −90 = straight down)", nzero(fpaDeg, 0))
	}
	vHorizLabel := fmt.Sprintf("%.0f m/s (surface-rel)", vHoriz)
	if vHoriz > sim.CrashVCritMps {
		vHorizLabel = v.theme.Alert.Render(
			fmt.Sprintf("%.0f m/s (> %.0f = CRASH on contact)", vHoriz, sim.CrashVCritMps))
	}
	return []string{
		v.theme.Primary.Render("DESCENT"),
		fmt.Sprintf("  altitude:   %s", altLabel),
		fmt.Sprintf("  v_vert:     %.1f m/s", nzero(vVert, 1)),
		fmt.Sprintf("  v_horiz:    %s", vHorizLabel),
		fmt.Sprintf("  fpa:        %s", fpaLabel),
		fmt.Sprintf("  twr:        %s", twrLabel),
		fmt.Sprintf("  sas:        %s", c.AttitudeMode.String()),
	}
}

// buildLaunchHintChip tells the player the launch/surface view is worth
// a look at the one moment it starts being useful — issue #348 §4's
// mirror of buildProximityHintChip below. The gate is exactly
// sim.DescentCorridorFor's own forecast (the once-per-crossing
// discipline and its dismiss live in sim.World.LaunchHintActive), so
// this hint always agrees with whatever the DESCENT CORRIDOR chip would
// say once you actually jump into the surface view — it just says so
// one screen early.
func (v *OrbitView) buildLaunchHintChip(w *sim.World) []string {
	if !w.LaunchHintActive() {
		return nil
	}
	return []string{
		v.theme.Primary.Render("DESCENDING") + " — [V] launch/surface view",
	}
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
	rateLabel := fmt.Sprintf("%.1f m/s", descentRate)
	if vRel.Norm() >= sim.CrashVCritMps {
		rateLabel = v.theme.Alert.Render(
			fmt.Sprintf("%.1f m/s (|v_rel| > %.0f = CRASH on contact)", descentRate, sim.CrashVCritMps))
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

// buildOrbitMetricsChip is the always-on top-right ORBIT readout: the
// live current orbit shape (altitude / apo / peri / t→apo / t→peri /
// inclination / direction). Suppressed during ascent — the LAUNCH chip
// already carries ap/pe there — and for degenerate / hyperbolic states.
// The projected post-burn orbit is a SEPARATE chip
// (buildProjectedOrbitChip, issue #63 follow-up) so the player sees the
// current and projected orbits side by side while planning a burn,
// instead of the live orbit being replaced by the projection.
func (v *OrbitView) buildOrbitMetricsChip(w *sim.World) []string {
	if !w.CraftVisibleHere() {
		// ADR 0038 S4 part 3 ("badged panels"): riding in another player's
		// stack is the commonest reason CraftVisibleHere is false with no
		// active craft — and it's exactly when there IS a live orbit to
		// show, the stack's. buildDockGuestOrbitChip returns nil for every
		// other !CraftVisibleHere case (no DockGuest, or no ghost report
		// yet), so the ORBIT chip's existing silence is unchanged there.
		return v.buildDockGuestOrbitChip(w)
	}
	c := w.ActiveCraft()
	if c == nil {
		return nil
	}
	// Live current orbit shape. Suppressed during ascent (LAUNCH chip
	// carries ap/pe) and for degenerate/hyperbolic states.
	if shouldShowLaunchHUD(c) {
		return nil
	}
	// #375: a Landed craft carries no orbit (craftHasOrbit) — on an
	// airless primary shouldShowLaunchHUD above never fires (no
	// Atmosphere to gate ascent on), so without this the co-rotation
	// pseudo-orbit would render here as a real ellipse. Swap in the
	// facts that ARE true on the ground rather than leaving the chip
	// blank — the same move buildLaunchChip already makes (incl. →
	// launch lat) — since a chip that vanishes reads as broken.
	if !craftHasOrbit(c) {
		return v.buildLandedOrbitChip(c)
	}
	mu := c.Primary.GravitationalParameter()
	frame := orbital.ReferenceFrameForPrimary(c.Primary)
	el := orbital.ElementsFromStateInFrame(c.State.R, c.State.V, mu, frame)
	if math.IsNaN(el.A) || math.IsInf(el.A, 0) || el.A <= 0 || el.E >= 1 {
		return nil
	}
	primaryR := c.Primary.RadiusMeters()
	apoAlt := el.Apoapsis() - primaryR
	periAlt := el.Periapsis() - primaryR
	st := orbital.Vec3State{R: c.State.R, V: c.State.V}
	lines := []string{
		v.theme.Primary.Render("ORBIT"),
		chipRow("altitude:", formatChipKm(c.Altitude())),
		chipRow("Ap:", formatChipKm(apoAlt)),
	}
	// On a circular orbit the apsides are not locatable points (#286), so
	// the countdowns say "—" rather than the constant half-period the
	// underlying helpers fall back to. A frozen number that looks live is
	// worse than an honest blank: players read it as phase information and
	// tried to time rendezvous off two craft that both showed P/2.
	apsisTime := func(secs float64) (string, bool) {
		if !orbital.ApsisDefined(el.E) {
			return "—", true
		}
		if secs < 0 {
			return "", false
		}
		return formatDurationShort(secs), true
	}
	if s, ok := apsisTime(orbital.TimeToApoapsis(st, mu)); ok {
		lines = append(lines, chipRow("t→Ap:", s))
	}
	lines = append(lines, chipRow("Pe:", formatChipKm(periAlt)))
	if s, ok := apsisTime(orbital.TimeToPeriapsis(st, mu)); ok {
		lines = append(lines, chipRow("t→Pe:", s))
	}
	// Full orbital period, alongside the apsis-time readouts — the number
	// a comsat placement is tuned to (e.g. a synchronous or semi-
	// synchronous period for steady ground coverage). a > 0 and e < 1 are
	// guaranteed above, so the period is finite.
	period := 2 * math.Pi * math.Sqrt(el.A*el.A*el.A/mu)
	lines = append(lines, chipRow("period:", formatPeriod(period)))
	lines = append(lines, chipRow("inclin.:", fmt.Sprintf("%.2f°", el.I*180/math.Pi)))
	lines = append(lines, chipRow("direction:", v.orbitDirectionLabel(el.I)))
	if periAlt < 0 {
		lines = append(lines, "  "+v.theme.Alert.Render("⚠ PERIAPSIS BELOW SURFACE"))
	}
	return lines
}

// buildLandedOrbitChip is buildOrbitMetricsChip's landed branch (#375).
// A parked craft's (R, ω×R) co-rotation state resolves through
// ElementsFromState to a valid-looking ellipse (apoapsis pinned at the
// vessel, periapsis a few metres from the primary's centre, sign-
// flipping at the display quantum tick to tick), so the chip must not
// read elements at all while Landed. Instead it shows the facts that
// ARE true on the ground — body, landed lat/lon, altitude (always 0),
// and surface co-rotation speed (c.State.V IS ω×R for a Landed craft,
// per integrateLanded) — the same swap buildLaunchChip already makes
// (incl. → launch lat) rather than leaving the chip blank.
func (v *OrbitView) buildLandedOrbitChip(c *spacecraft.Spacecraft) []string {
	lat, lon := c.SurfaceLatLon()
	return []string{
		v.theme.Primary.Render("ORBIT"),
		chipRow("body:", c.Primary.EnglishName),
		chipRow("landed at:", fmt.Sprintf("%.1f°, %.1f°", lat, lon)),
		chipRow("altitude:", "0.0 km"),
		chipRow("co-rotation:", fmt.Sprintf("%.1f m/s", c.State.V.Norm())),
	}
}

// buildDockGuestOrbitChip is the ORBIT chip's badged rider-view sibling
// (ADR 0038 S4 part 3): while riding in another player's stack, the
// guest's own Crafts slate is empty, so the live-craft ORBIT readout above
// has nothing to draw — exactly when there IS an orbit worth showing, the
// stack's. Mirrors buildOrbitMetricsChip's own-craft element derivation
// (same ElementsFromStateInFrame call) but reads the ghost's
// primary-relative state instead of a local craft's, and headers with the
// owner's handle so the numbers are never mistaken for the player's own
// ship. Returns nil with no DockGuest, no ghost report yet, or a
// degenerate/hyperbolic resolved orbit — the caller (buildOrbitMetricsChip)
// falls through to its existing silent nil in all of those.
func (v *OrbitView) buildDockGuestOrbitChip(w *sim.World) []string {
	g, primary, ok := w.DockGuestStackGhost()
	if !ok {
		return nil
	}
	mu := primary.GravitationalParameter()
	frame := orbital.ReferenceFrameForPrimary(*primary)
	el := orbital.ElementsFromStateInFrame(g.RelPos, g.Vel, mu, frame)
	if math.IsNaN(el.A) || math.IsInf(el.A, 0) || el.A <= 0 || el.E >= 1 {
		return nil
	}
	primaryR := primary.RadiusMeters()
	apoAlt := el.Apoapsis() - primaryR
	periAlt := el.Periapsis() - primaryR
	header := "ORBIT"
	if w.DockGuest.OwnerHandle != "" {
		header = "ORBIT — " + w.DockGuest.OwnerHandle + "'s stack"
	}
	lines := []string{
		v.theme.Primary.Render(header),
		chipRow("Ap:", formatChipKm(apoAlt)),
		chipRow("Pe:", formatChipKm(periAlt)),
		chipRow("inclin.:", fmt.Sprintf("%.2f°", el.I*180/math.Pi)),
	}
	if periAlt < 0 {
		lines = append(lines, "  "+v.theme.Alert.Render("⚠ PERIAPSIS BELOW SURFACE"))
	}
	return lines
}

// buildProjectedOrbitChip is the PROJECTED ORBIT readout — the projected
// post-burn orbit once resolved nodes (or a live burn) are planted,
// expressed in the primary's reference frame. Returns nil when no
// projection is available, so it surfaces only while a burn is
// planned/in flight, stacked beneath the always-on ORBIT chip. Split out
// of buildOrbitMetricsChip (issue #63 follow-up) so the current and
// projected orbits show simultaneously rather than the projection
// replacing the live readout.
func (v *OrbitView) buildProjectedOrbitChip(w *sim.World) []string {
	if !w.CraftVisibleHere() || w.ActiveCraft() == nil {
		return nil
	}
	state, primary, ok := w.PredictedFinalOrbit()
	if !ok {
		return nil
	}
	mu := primary.GravitationalParameter()
	frame := orbital.ReferenceFrameForPrimary(primary)
	ro := orbital.OrbitReadoutInFrame(state.R, state.V, mu, frame)
	primaryR := primary.RadiusMeters()
	lines := []string{
		v.theme.Primary.Render("PROJECTED ORBIT"),
		fmt.Sprintf("  primary:   %s", primary.EnglishName),
	}
	if ro.Hyperbolic {
		lines = append(lines,
			"  "+v.theme.Warning.Render("hyperbolic — escape"),
			fmt.Sprintf("  Pe:        %.1f km alt", (ro.PeriMeters-primaryR)/1000),
			fmt.Sprintf("  e:         %.3f", ro.Eccentricity),
		)
	} else {
		// Elliptical: a = (apo + peri)/2 from the apsis radii, so the
		// resulting period is shown alongside Ap/Pe for tuning a comsat
		// insertion burn to a target period.
		projA := (ro.ApoMeters + ro.PeriMeters) / 2
		projPeriod := 2 * math.Pi * math.Sqrt(projA*projA*projA/mu)
		lines = append(lines,
			fmt.Sprintf("  Ap:        %.1f km alt", (ro.ApoMeters-primaryR)/1000),
			fmt.Sprintf("  Pe:        %.1f km alt", (ro.PeriMeters-primaryR)/1000),
			fmt.Sprintf("  period:    %s", formatPeriod(projPeriod)),
			fmt.Sprintf("  inclin.:   %.2f°", ro.Inclination*180/math.Pi),
			fmt.Sprintf("  direction: %s", v.orbitDirectionLabel(ro.Inclination)),
		)
		const equatorialTol = 1e-3
		if ro.Inclination < equatorialTol || math.Abs(ro.Inclination-math.Pi) < equatorialTol {
			lines = append(lines, v.theme.Dim.Render("  AN/DN:     equatorial"))
		} else {
			lines = append(lines,
				fmt.Sprintf("  AN angle:  %.1f°", normalizeDeg(ro.AscNode*180/math.Pi)),
				fmt.Sprintf("  DN angle:  %.1f°", normalizeDeg(ro.DescNode*180/math.Pi)),
			)
		}
	}
	return lines
}

// buildTargetChip surfaces the unified Target slot — a body (name, Δi,
// range) or a craft (name/role, orbit shape, range, |v_rel|, closing,
// closest-approach, rendezvous advisory, DOCK READY). Returns nil when no
// target is set. Transplanted from renderHUD's TARGET block.
func (v *OrbitView) buildTargetChip(w *sim.World) []string {
	c := w.ActiveCraft()
	if c == nil || w.Target.Kind == sim.TargetNone {
		return nil
	}
	switch w.Target.Kind {
	case sim.TargetBody:
		sysT := w.System()
		if w.Target.BodyIdx <= 0 || w.Target.BodyIdx >= len(sysT.Bodies) {
			return nil
		}
		b := sysT.Bodies[w.Target.BodyIdx]
		nameStyle := lipgloss.NewStyle().Foreground(render.ColorFor(b)).Bold(true)
		lines := []string{
			v.theme.Primary.Render("TARGET"),
			chipRow("body:", nameStyle.Render(b.EnglishName)),
		}
		mu := c.Primary.GravitationalParameter()
		frame := orbital.ReferenceFrameForPrimary(c.Primary)
		ro := orbital.OrbitReadoutInFrame(c.State.R, c.State.V, mu, frame)
		if !ro.Hyperbolic {
			nCraft := c.State.R.Cross(c.State.V)
			nTarget := orbital.OrbitNormalWorld(b)
			var di float64
			if nCraft.Norm() > 0 && nTarget.Norm() > 0 {
				cos := nCraft.Dot(nTarget) / (nCraft.Norm() * nTarget.Norm())
				if cos > 1 {
					cos = 1
				} else if cos < -1 {
					cos = -1
				}
				ang := math.Acos(cos) * 180 / math.Pi
				di = math.Min(ang, 180-ang)
			}
			diLabel := fmt.Sprintf("%.2f°", di)
			if di > 30 {
				diLabel = v.theme.Warning.Render(diLabel)
			}
			lines = append(lines, chipRow("Δi:", diLabel))
		}
		rangeM := w.BodyPosition(b).Sub(w.CraftInertial()).Norm()
		lines = append(lines, chipRow("range:", formatRangeM(rangeM)))
		// Predicted closest approach along the projected orbit — updates live
		// as the player hand-flies a correction, so they can judge where the
		// transfer actually passes the target rather than eyeballing the
		// dashed curve. Perilune altitude when the path enters the SOI
		// (negative ⇒ surface impact), else the flyby miss distance.
		if ap, ok := w.PredictedTargetApproach(); ok {
			if ap.EntersSOI {
				alt := ap.Dist - b.RadiusMeters()
				if alt <= 0 {
					lines = append(lines, chipRow("perilune:", v.theme.Warning.Render("IMPACT")))
				} else {
					lines = append(lines, chipRow("perilune:", fmt.Sprintf("%.0f km", alt/1000)))
				}
			} else {
				lines = append(lines, chipRow("approach:", formatRangeM(ap.Dist)))
			}
			lines = append(lines, chipRow("TCA:", formatTCA(ap.TCA)))
		}
		return lines
	case sim.TargetCraft:
		tc, _, ok := w.ResolveTargetCraft()
		if !ok {
			return nil
		}
		lines := []string{v.theme.Primary.Render("TARGET"), chipRow("vessel:", tc.Name)}
		if craftHasOrbit(tc) {
			tMu := tc.Primary.GravitationalParameter()
			tFrame := orbital.ReferenceFrameForPrimary(tc.Primary)
			tEl := orbital.ElementsFromStateInFrame(tc.State.R, tc.State.V, tMu, tFrame)
			if tEl.A > 0 && !math.IsNaN(tEl.A) && !math.IsInf(tEl.A, 0) {
				tPrimaryR := tc.Primary.RadiusMeters()
				lines = append(lines,
					chipRow("Ap:", formatChipKm(tEl.Apoapsis()-tPrimaryR)),
					chipRow("Pe:", formatChipKm(tEl.Periapsis()-tPrimaryR)),
					chipRow("inclin.:", fmt.Sprintf("%.2f°", tEl.I*180/math.Pi)),
				)
			}
		} else {
			// #375: a landed target's (R, ω×R) co-rotation state is not an
			// orbit — swap Ap/Pe/inclin. for its landing site rather than
			// reading elements off the pseudo-orbit. Range / |v_rel| /
			// closing below stay meaningful (relative-state math, not
			// elements) so they're untouched.
			tLat, tLon := tc.SurfaceLatLon()
			lines = append(lines, chipRow("landed at:", fmt.Sprintf("%.1f°, %.1f°", tLat, tLon)))
		}
		var rRel, vRelVec orbital.Vec3
		if tc.Primary.ID == c.Primary.ID {
			rRel = tc.State.R.Sub(c.State.R)
			vRelVec = tc.State.V.Sub(c.State.V)
		} else {
			tcInertial := w.BodyPosition(tc.Primary).Add(tc.State.R)
			rRel = tcInertial.Sub(w.CraftInertial())
			vRelVec = w.CraftInertialVelocity(tc).Sub(w.CraftInertialVelocity(c))
		}
		rangeM := rRel.Norm()
		vRel := vRelVec.Norm()
		var closing float64
		if rangeM > 0 {
			closing = -rRel.Dot(vRelVec) / rangeM
		}
		leadDeg, leadOK := w.TargetLeadAngleDeg()
		lines = append(lines,
			chipRow("range:", formatRangeM(rangeM)),
			chipRow("|v_rel|:", fmt.Sprintf("%.2f m/s", vRel)),
			chipRow("closing:", fmt.Sprintf("%+.2f m/s", closing)),
			chipRow("lead:", targetLeadLabel(leadDeg, leadOK)),
		)
		if tc.Primary.ID == c.Primary.ID {
			// #375 follow-up: closestApproachRows Kepler-propagates the
			// target's (R, V) forward to find the encounter — feeding it a
			// landed target's (R, ω×R) co-rotation state would propagate
			// the very pseudo-orbit the rest of #375 suppresses (periapsis
			// metres from the primary's centre) and print a TCA/CA pair for
			// a phantom trajectory. Range/closing above stay meaningful for
			// a landed target (relative-state math, not propagation) so
			// only this predicted-encounter row group is gated.
			if craftHasOrbit(tc) {
				lines = append(lines, v.closestApproachRows(w, c)...)
			}
			if rangeM < 50 && vRel < 0.1 {
				dockStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#3DDC84")).Bold(true)
				lines = append(lines, "  "+dockStyle.Render("DOCK READY"))
			}
		}
		return lines
	case sim.TargetGhost:
		// v0.27 review follow-up: a remote player's craft. Same rows as
		// a local craft target — orbit, range, |v_rel|, closing, CA/TCA
		// — resolved from the ghost slate (already at this world's
		// sim-time). No DOCK READY: cross-player docking is v0.28.
		g, gPrimary, ok := w.ResolveTargetGhost()
		if !ok {
			// #294 review finding 4: the lock survives an unresolved ghost
			// (Kind stays TargetGhost so a later resolve re-latches it —
			// see World.HasRelativeTarget / World.TargetName) — a bare
			// "return nil" here reads as no target at all, indistinguishable
			// from TargetNone. Show the pending state instead so the player
			// can tell "still locked, just waiting" from "lost".
			return []string{
				v.theme.Primary.Render("TARGET"),
				chipRow("ghost:", w.TargetName()),
				chipRow("status:", "signal not yet resolved"),
			}
		}
		lines := []string{v.theme.Primary.Render("TARGET"), chipRow("ghost:", w.TargetName())}
		gRel := g.Pos.Sub(w.BodyPosition(gPrimary))
		gMu := gPrimary.GravitationalParameter()
		gFrame := orbital.ReferenceFrameForPrimary(gPrimary)
		gEl := orbital.ElementsFromStateInFrame(gRel, g.Vel, gMu, gFrame)
		if gEl.A > 0 && !math.IsNaN(gEl.A) && !math.IsInf(gEl.A, 0) {
			gPrimaryR := gPrimary.RadiusMeters()
			lines = append(lines,
				chipRow("Ap:", formatChipKm(gEl.Apoapsis()-gPrimaryR)),
				chipRow("Pe:", formatChipKm(gEl.Periapsis()-gPrimaryR)),
				chipRow("inclin.:", fmt.Sprintf("%.2f°", gEl.I*180/math.Pi)),
			)
		}
		rT, vT, ok := w.TargetStateRelativeToActivePrimary()
		if !ok {
			return lines
		}
		rRel := rT.Sub(c.State.R)
		vRelVec := vT.Sub(c.State.V)
		rangeM := rRel.Norm()
		vRel := vRelVec.Norm()
		var closing float64
		if rangeM > 0 {
			closing = -rRel.Dot(vRelVec) / rangeM
		}
		leadDeg, leadOK := w.TargetLeadAngleDeg()
		lines = append(lines,
			chipRow("range:", formatRangeM(rangeM)),
			chipRow("|v_rel|:", fmt.Sprintf("%.2f m/s", vRel)),
			chipRow("closing:", fmt.Sprintf("%+.2f m/s", closing)),
			chipRow("lead:", targetLeadLabel(leadDeg, leadOK)),
		)
		if gPrimary.ID == c.Primary.ID {
			lines = append(lines, v.closestApproachRows(w, c)...)
		}
		return lines
	}
	return nil
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
		chipRow("TCA:", formatTCA(tCA)),
		chipRow("CA:", formatRangeM(distCA)),
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
		return fmt.Sprintf("%.0f km", p.PeriluneAltitude()/1000)
	}
	if arc.hasNodes {
		// Dual arc: planned (bright path) + no-burn (counterfactual). The
		// planned path's SOI-entry clock rides under it (ADR 0021 C: the
		// Entry glyph marks where, the chip carries when).
		if arc.plOK {
			lines = append(lines, chipRow("planned:", periValue(arc.planned)))
			if arc.planned.HasEntryTime {
				lines = append(lines, chipRow("  T-entry:", formatTCA(arc.planned.TimeToEntry)))
			}
			lines = append(lines, chipRow("  T-peri:", formatTCA(arc.planned.TimeToPerilune)))
		}
		if arc.cfOK {
			lines = append(lines, chipRow("no-burn:", periValue(arc.counterfactual)))
		}
		return lines
	}
	// Single live pass (no node planted). T-entry is the predicted SOI-entry
	// clock — the ring crossing the Entry glyph marks (ADR 0021 C).
	if arc.counterfactual.HasEntryTime {
		lines = append(lines, chipRow("T-entry:", formatTCA(arc.counterfactual.TimeToEntry)))
	}
	lines = append(lines,
		chipRow("perilune:", periValue(arc.counterfactual)),
		chipRow("TCA:", formatTCA(arc.counterfactual.TimeToPerilune)))
	return lines
}

// chipValueCol is the display column a chip row's value begins at, shared
// by the ORBIT and TARGET chips so the two line up when stacked in the same
// corner. The buildOrbitMetricsChip rows are hand-padded to this column.
const chipValueCol = 13

// chipRow formats a "  label   value" telemetry row with the value pinned
// to chipValueCol regardless of label width — so a chip's values share one
// column instead of drifting per label. Padding is measured in display
// cells (lipgloss.Width), so multibyte labels like "Δi:" and styled values
// align correctly where byte-counted %-Ns padding would not.
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

func chipRow(label, value string) string {
	prefix := "  " + label
	pad := chipValueCol - lipgloss.Width(prefix)
	if pad < 1 {
		pad = 1
	}
	return prefix + strings.Repeat(" ", pad) + value
}

// formatRangeM renders a distance with AU / km / m bands matching the
// thresholds the TARGET block used inline.
func formatRangeM(rangeM float64) string {
	switch {
	case rangeM > bodies.AU/10:
		return fmt.Sprintf("%.3f AU", rangeM/bodies.AU)
	case rangeM > 1e6:
		return fmt.Sprintf("%.0f km", rangeM/1000)
	case rangeM > 1000:
		return fmt.Sprintf("%.2f km", rangeM/1000)
	default:
		return fmt.Sprintf("%.0f m", rangeM)
	}
}

// nzero snaps a value whose magnitude rounds to zero at `decimals` places
// to +0, so a quantity that jitters across zero (v_vert / fpa / altitude
// on the pad, where the co-rotation state carries sub-unit noise) doesn't
// flicker a "-0" / "-0.0" sign each frame. Only the sign of an
// already-zero display changes; non-zero values pass through untouched.
func nzero(x float64, decimals int) float64 {
	scale := math.Pow(10, float64(decimals))
	if math.Round(x*scale) == 0 {
		return 0
	}
	return x
}

// formatChipKm renders a metres reading as a one-decimal kilometre
// string with nzero applied (#375), so an altitude/apsis that legitimately
// sits at the display quantum — the co-rotation noise a near-zero orbit
// carries, not just a Landed craft's pseudo-orbit — can't flip a "-0.0"
// sign from one tick to the next. Shared by the ORBIT / TARGET chips'
// altitude, Ap, and Pe rows.
func formatChipKm(m float64) string {
	return fmt.Sprintf("%.1f km", nzero(m/1000, 1))
}

// formatTCA renders a time-to-closest-approach with s / min / h bands.
func formatTCA(sec float64) string {
	switch {
	case sec >= 3600:
		return fmt.Sprintf("%.2fh", sec/3600)
	case sec >= 60:
		return fmt.Sprintf("%.1fmin", sec/60)
	default:
		return fmt.Sprintf("%.0fs", sec)
	}
}
