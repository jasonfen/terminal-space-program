package screens

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// TestTargetLeadLabel is the pure content selector's unit test (issue
// #287): a signed angle is always paired with a plain "ahead"/"behind"
// word, never a bare +/- number a pilot has to decode under pressure,
// and "—" stands in when the reading isn't meaningful.
func TestTargetLeadLabel(t *testing.T) {
	cases := []struct {
		name  string
		angle float64
		ok    bool
		want  string
	}{
		{"ahead", 82, true, "+82° (ahead)"},
		{"behind", -82, true, "-82° (behind)"},
		{"aligned", 0, true, "0° (aligned)"},
		{"not_meaningful", 0, false, "—"},
		{"not_meaningful_nonzero_angle_ignored", 999, false, "—"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := targetLeadLabel(c.angle, c.ok); got != c.want {
				t.Errorf("targetLeadLabel(%v, %v) = %q, want %q", c.angle, c.ok, got, c.want)
			}
		})
	}
}

// leadTestWorld builds a two-craft world (mirrors the sim package's own
// rendezvousTwoCraftWorld) with the sister craft offset along-track by
// deltaDeg on the active craft's own circular orbital plane, and targets
// the sister from the active craft.
func leadTestWorld(t *testing.T, deltaDeg float64) *sim.World {
	t.Helper()
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	if _, err := w.SpawnCraft(sim.SpawnSpec{AltitudeM: 500e3}); err != nil {
		t.Fatalf("SpawnCraft: %v", err)
	}
	active := w.Crafts[0]
	sister := w.Crafts[1]

	h := active.State.R.Cross(active.State.V)
	axis := h.Unit()
	angle := deltaDeg * math.Pi / 180
	cos, sin := math.Cos(angle), math.Sin(angle)
	rotate := func(v orbital.Vec3) orbital.Vec3 {
		return v.Scale(cos).Add(axis.Cross(v).Scale(sin)).Add(axis.Scale(axis.Dot(v) * (1 - cos)))
	}
	sister.State.R = rotate(active.State.R)
	sister.State.V = rotate(active.State.V)
	sister.Primary = active.Primary

	w.ActiveCraftIdx = 0
	w.SetTargetCraft(1)
	return w
}

// TestBuildTargetChipShowsLeadAhead/Behind exercise the issue's own
// reproduction through the render path: the TARGET chip must carry a
// "lead:" row reading the direction in plain words, both ways.
func TestBuildTargetChipShowsLeadAhead(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	w := leadTestWorld(t, 82)
	lines := v.buildTargetChip(w)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "lead:") {
		t.Fatalf("TARGET chip missing lead: row:\n%s", joined)
	}
	if !strings.Contains(joined, "+82° (ahead)") {
		t.Errorf("TARGET chip lead row = %q, want a line containing \"+82° (ahead)\"", joined)
	}
}

func TestBuildTargetChipShowsLeadBehind(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	w := leadTestWorld(t, -82)
	lines := v.buildTargetChip(w)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "-82° (behind)") {
		t.Errorf("TARGET chip lead row = %q, want a line containing \"-82° (behind)\"", joined)
	}
}

// TestBuildTargetChipLeadDashesAcrossPrimaries: a craft target orbiting a
// different primary than the active craft has no shared reference orbit
// to measure a phase angle in — the row must render "—", not a number
// that looks meaningful but isn't (issue #287 decision).
func TestBuildTargetChipLeadDashesAcrossPrimaries(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	w := leadTestWorld(t, 82)
	sister := w.Crafts[1]
	// Move the sister to any other body in the system so its Primary no
	// longer matches the active craft's.
	for _, b := range w.System().Bodies {
		if b.ID != sister.Primary.ID {
			sister.Primary = b
			break
		}
	}
	lines := v.buildTargetChip(w)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "lead:") {
		t.Fatalf("TARGET chip missing lead: row:\n%s", joined)
	}
	if !strings.Contains(joined, "lead:") || !strings.Contains(joined, "—") {
		t.Errorf("cross-primary target's lead row should read —, got:\n%s", joined)
	}
}

// TestBuildTargetChipBodyTargetHasNoLeadRow: the field is craft-target
// only (issue #287 decision) — a body target must never grow a lead row.
func TestBuildTargetChipBodyTargetHasNoLeadRow(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	w.SetTargetBody(1)
	lines := v.buildTargetChip(w)
	joined := strings.Join(lines, "\n")
	if strings.Contains(joined, "lead:") {
		t.Errorf("body TARGET chip should not have a lead: row:\n%s", joined)
	}
}

// TestTargetChipLeadRowFitsNarrowTerminal renders the full HUD at 80x24 —
// the project's minimum-viable terminal, not a generous one — with a
// craft target set (so the TARGET chip carries the new lead: row) and
// asserts the frame never grows past the terminal height. Two clipping
// defects shipped in this project because every prior chip-growth test
// used 120x40 or 200x60; this one deliberately doesn't.
//
// Note: at 80x24 the always-on ORBIT chip alone already fills the
// top-right corner's chip budget (admitChipsByBudget, chipPriorityNormal
// for TARGET) — a pre-existing space constraint independent of this
// change, reproducible on main without the lead row. So this test does
// not assert TARGET itself is visible in the composed frame; that would
// be testing the corner-budget allocator, out of scope for #287. It
// asserts the one thing in scope: the frame never overflows regardless.
func TestTargetChipLeadRowFitsNarrowTerminal(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	const cols, rows = 80, 24
	v.Resize(cols, rows)
	w := leadTestWorld(t, 82)
	// Plant a node too — the layout this row actually ships into, not
	// TARGET alone in isolation.
	c := w.ActiveCraft()
	c.Nodes = append(c.Nodes, spacecraft.ManeuverNode{
		DV:          50,
		TriggerTime: w.Clock.SimTime.Add(10 * time.Minute),
	})

	out := v.Render(w, 0, cols, rows)
	if h := strings.Count(out, "\n") + 1; h > rows {
		t.Errorf("frame height = %d rows, want <= %d at 80x24 with a craft target set", h, rows)
	}
}

// TestTargetChipLeadRowComposesAtNarrowWidth exercises the actual
// composition path (composeChips, the same corner-placement/clipping
// logic Render uses) with the real TARGET chip content — lead row
// included — at an 80-column canvas, isolated from the unrelated
// ORBIT-vs-TARGET corner-budget competition covered by the note above.
// Confirms the new row: composes without panic, is glyph-width-consistent
// (assertChipCellWidthConsistent — the em dash / degree sign / +- glyphs
// this row introduces must measure the same in lipgloss cells as in
// splitStyledCells, the ☾-class trap noted throughout this file), and
// its rendered column stays within the 80-col canvas.
func TestTargetChipLeadRowComposesAtNarrowWidth(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	w := leadTestWorld(t, 82)
	lines := v.buildTargetChip(w)
	assertChipCellWidthConsistent(t, "TARGET (ahead)", lines)

	const cols, rows = 80, 24
	chips := []builtChip{
		{id: "TARGET", corner: cornerTopRight, lines: lines},
	}
	out := v.composeChips(blankCanvas(cols, rows), cols, rows, 0, 0, 0, chips)
	if !strings.Contains(out, "lead:") {
		t.Fatalf("composed frame missing the lead: row:\n%s", out)
	}
	for _, line := range strings.Split(out, "\n") {
		if width := lipgloss.Width(line); width > cols {
			t.Errorf("composed line exceeds %d cols (%d): %q", cols, width, line)
		}
	}

	// The dashed (cross-primary) form must also stay glyph-width-consistent.
	sister := w.Crafts[1]
	for _, b := range w.System().Bodies {
		if b.ID != sister.Primary.ID {
			sister.Primary = b
			break
		}
	}
	dashLines := v.buildTargetChip(w)
	assertChipCellWidthConsistent(t, "TARGET (dashed)", dashLines)
}
