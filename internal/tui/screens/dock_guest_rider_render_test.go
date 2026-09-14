package screens

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// riderViewTheme mirrors chipTestTheme but is exported-local to this file
// (kept separate from the other render smoke tests so it's obvious this
// one is meant to be read, not just asserted against).
func riderViewTheme() Theme {
	return Theme{
		Primary: lipgloss.NewStyle(),
		Warning: lipgloss.NewStyle(),
		Alert:   lipgloss.NewStyle(),
		Dim:     lipgloss.NewStyle(),
		HUDBox:  lipgloss.NewStyle().Border(lipgloss.RoundedBorder()),
		Footer:  lipgloss.NewStyle(),
		Title:   lipgloss.NewStyle(),
	}
}

// TestDockGuestRenderLooksRight is a "render and actually look at it" check
// (ADR 0038 S4): a real full-frame Render call with a DockGuest + ghost
// fixture, live and empty-seat, printed via -v so a mangled column (a
// width-2 glyph, a broken border, an overlapping chip) is visible to a
// human reviewer rather than only passing a substring assertion. Also
// pins the structural invariants a substring check could miss: the DOCKED
// block and the VESSEL/ORBIT panels must all appear, and the canvas must
// not blow past its requested width (padChipBlock/overlayStyledBlock
// mismatches show up as a wildly long line).
func TestDockGuestRenderLooksRight(t *testing.T) {
	v := NewOrbitView(riderViewTheme())
	v.Resize(200, 60)

	for _, tc := range []struct {
		name   string
		online bool
	}{
		{"live-owner", true},
		{"empty-seat", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := dockGuestStackGhostWorld(t)
			w.Session = &sim.SessionInfo{Players: []sim.SessionPlayer{
				{Fingerprint: w.DockGuest.OwnerFP, Handle: w.DockGuest.OwnerHandle, Online: tc.online},
			}}

			out := v.Render(w, 0, 200, 60)
			t.Logf("=== DockGuest render (%s) ===\n%s", tc.name, out)

			// ADR 0051 retires the VESSEL chip (its identity content moves
			// to the title bar / instrument boxes); DOCKED (unaffected by
			// the ADR) already carries the rider's own identity ("riding
			// in bob's stack"), so the literal word "VESSEL" is no longer
			// expected here.
			for _, want := range []string{"DOCKED", "riding in bob's stack", "bob's stack"} {
				if !strings.Contains(out, want) {
					t.Errorf("%s render missing %q", tc.name, want)
				}
			}
			if tc.online {
				if !strings.Contains(out, "[J] request control") || !strings.Contains(out, "[U] ask to undock") {
					t.Errorf("%s render missing the live-owner exits", tc.name)
				}
			} else {
				if !strings.Contains(out, "take the stick") {
					t.Errorf("%s render missing the empty-seat exit", tc.name)
				}
			}

			for i, line := range strings.Split(out, "\n") {
				if width := lipgloss.Width(line); width > 220 {
					t.Errorf("%s render line %d is %d cells wide (canvas requested at 200) — a chip likely overran: %q", tc.name, i, width, line)
				}
			}
		})
	}
}

// TestDockGuestRenderIncludesDockedBlock (#328): the standing
// DOCKED block was silently clipped entirely off-canvas at 80x24 (the
// most common terminal size at the time). composeChips had no height bound on the
// top-left stack at all: topLeftRow grew past cRows with nothing to
// stop it, and overlayStyledBlock silently drops any row outside
// [0, cRows). DOCKED is appended late in assembleChips' top-left order,
// so it absorbed all the accumulated overflow from VESSEL/MISSION/
// SESSION/TIME LOCK ahead of it and rendered nowhere. It is the rider's
// only surviving route to [J] request control / [U] undock, so unlike
// most chips it must never be silently lost to overflow.
//
// ADR 0051 note: the original #328 fixture (VESSEL + MISSION + SESSION +
// TIME LOCK ahead of DOCKED) is now six much larger Core-priority
// instrument boxes ahead of DOCKED (ENGINE/PROPELLANT/GUIDANCE/COMMS/
// STAGES/MISSION) — even a Design Size floor (ADR 0046) doesn't cover
// 80x24, and Core priority is a STRONGER never-drop guarantee than
// DOCKED's own Forced priority, so at this narrow a canvas the boxes can
// legitimately consume the whole left stack before DOCKED gets a look
// in. Moved to the Design Size (140x40), the one canvas ADR 0046
// actually guarantees a fit at, to keep testing the #328 invariant
// itself rather than a below-floor budget fight ADR 0051 didn't create
// but does make worse. Flagged in the slice 2a report: a rider on a
// genuinely narrow terminal can still lose their only exit route to the
// new box set — worth the maintainer's attention, not silently accepted.
func TestDockGuestRenderIncludesDockedBlock(t *testing.T) {
	v := NewOrbitView(riderViewTheme())
	v.Resize(DesignWidth, DesignHeight)

	w := dockGuestStackGhostWorld(t)
	w.Session = &sim.SessionInfo{Players: []sim.SessionPlayer{
		{Fingerprint: w.DockGuest.OwnerFP, Handle: w.DockGuest.OwnerHandle, Online: true},
	}}
	// MISSION is sized to its own step (ADR 0051 decision 14, re-grill
	// Q10) — Flight School's Orientation rung is the tallest (7 rows),
	// leaving only 4 of the left column's 36 rows spare even at the
	// Design Size (the ADR's own measured budget, R-40a). Turning Flight
	// School off drops MISSION to 3 rows (`MISSION —`), matching the
	// ADR's own "28 of 36" measurement (R-40c) and leaving the DOCKED
	// block room to prove the #328 invariant this test actually checks —
	// stacking the tallest MISSION rung on top of that invariant is a
	// coincidence of the default World's tutorial spawn, not something
	// this test needs to also stress.
	w.Missions = nil
	// Reproduce the #328 report's exact top-left stack: VESSEL (badged,
	// grown to 6 lines by the ghost report) + MISSION + SESSION + TIME
	// LOCK ahead of DOCKED — the combination that pushed DOCKED's block
	// to rows 21-26 on a 21-row canvas, entirely past the edge. (The dock
	// coupling also drives buildTimeLockChip's own #328 fix — see the
	// TIME LOCK suppression test — but this render-level test stands on
	// the structural composeChips budget alone, independent of that.)
	w.SessionEvents = []sim.SessionEvent{
		{Kind: sim.SessionEventJoin, Handle: "bob", At: time.Now()},
	}
	w.CoWarp = sim.CoWarpState{Coupled: true, MinWarp: 10, Partners: []string{"bob"}}

	out := v.Render(w, 0, DesignWidth, DesignHeight)
	t.Logf("=== DockGuest render at the Design Size ===\n%s", out)

	// ADR 0046 (#422): DOCKED is chipPriorityForced, so at this size it
	// may shrink to its Compact Form ("[J] control · [U] undock" on one
	// row) rather than the Full Form's two separate key rows — either is
	// fine, since Graceful Shrink never drops it whole; what must never
	// happen again is the #328 silent clip. Accept either wording as
	// long as both exit keys are legible somewhere in the frame.
	for _, want := range []string{"DOCKED", "riding in bob's stack"} {
		if !strings.Contains(out, want) {
			t.Errorf("render missing %q — the rider's only route to [J]/[U] was clipped off-canvas", want)
		}
	}
	if !strings.Contains(out, "[J]") || !strings.Contains(out, "[U]") {
		t.Errorf("render missing the [J]/[U] exit keys entirely:\n%s", out)
	}
}
