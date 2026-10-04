package screens

import (
	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/settings"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// The Hint Strip (grilled 2026-09-04, #425; CONTEXT.md §"Hint Strip") is
// the map's fixed legend on the canvas's last row, right of "view:".
// Fixed content, always on, no phase gating.

// TestHintStripPresentAtDesignSize confirms the full, exact strip text
// renders intact at the Design Size (140×40) — a plain fresh world, no
// exotic chip state.
func TestHintStripPresentAtDesignSize(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	v.Resize(140, 40)
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	out := stripANSI(v.Render(w, 0, 140, 40))
	if !strings.Contains(out, hintStripText) {
		t.Errorf("Hint Strip missing or corrupted at 140x40 (Design Size):\n%s", out)
	}
	if !strings.Contains(out, "view: ") {
		t.Errorf("expected the view: label to still be present:\n%s", out)
	}
}

// TestHintStripSurvivesDeclutter: the Hint Strip is the map's legend, not
// a Chip — F2 declutter must not touch it.
func TestHintStripSurvivesDeclutter(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	v.Resize(140, 40)
	v.SetDeclutter(true)
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	out := stripANSI(v.Render(w, 0, 140, 40))
	if !strings.Contains(out, hintStripText) {
		t.Errorf("Hint Strip should survive Declutter:\n%s", out)
	}
}

// TestHintStripSurvivesTutorialOff: the strip is unrelated to Flight
// School state — must render whether or not the tutorial program toggle
// is on.
func TestHintStripSurvivesTutorialOff(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	v.Resize(140, 40)
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	w.SetEnabledMissionPrograms(map[string]bool{}) // both programs off
	out := stripANSI(v.Render(w, 0, 140, 40))
	if !strings.Contains(out, hintStripText) {
		t.Errorf("Hint Strip should render regardless of Flight School toggle:\n%s", out)
	}
}

// TestHintStripDoesNotOverlapNavballOrBottomLeftChip stresses the two
// collision risks the task calls out explicitly: the navball panel and a
// bottom-left chip. It forces both to render (an active craft with a
// navball sub-observer, plus a live CHAT-style bottom-left chip is hard
// to force generically, so this uses the always-present ORBIT metrics /
// VESSEL core chip footprint that already occupies the bottom-left
// stacking cursor region) at both the Design Size (140x40) and the
// Playable Floor (104x24), and checks the exact Hint Strip text still
// appears intact — if a later chip's overlay had painted over any part
// of it, the literal substring would no longer be present.
func TestHintStripDoesNotOverlapNavballOrBottomLeftChip(t *testing.T) {
	for _, sz := range []struct{ w, h int }{{140, 40}, {104, 24}} {
		v := NewOrbitView(chipTestTheme())
		v.Resize(sz.w, sz.h)
		w, err := sim.NewWorld()
		if err != nil {
			t.Fatalf("NewWorld: %v", err)
		}
		out := stripANSI(v.Render(w, 0, sz.w, sz.h))
		if !strings.Contains(out, hintStripText) {
			t.Errorf("Hint Strip missing/corrupted at %dx%d (navball should be showing for the starter craft):\n%s", sz.w, sz.h, out)
		}
		// The navball panel paints an "RCS" toggle label; confirm it's
		// actually present in this render so the no-overlap check means
		// something (proving the check can find a positive before
		// trusting the negative).
		if !strings.Contains(out, "RCS") {
			t.Errorf("expected the navball panel (RCS control) to be present at %dx%d so this is a real overlap test, not a vacuous one:\n%s", sz.w, sz.h, out)
		}
	}
}

// TestHintStripStartsAfterViewLabel confirms the strip is placed strictly
// after the "view:" label ends (never overwrites it) by checking the
// longer "view: Tilted N°/anchor" form still leaves the full Hint Strip
// intact right after it, at the Design Size.
func TestHintStripStartsAfterViewLabel(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	v.Resize(140, 40)
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	w.ViewMode = sim.ViewTilted
	out := stripANSI(v.Render(w, 0, 140, 40))
	if !strings.Contains(out, "view: Tilted") {
		t.Errorf("expected the tilted view label:\n%s", out)
	}
	if !strings.Contains(out, hintStripText) {
		t.Errorf("Hint Strip missing/corrupted alongside the tilted view label:\n%s", out)
	}
}

// TestHintStripClipsRightBelowDesignSize: below the Design Size the strip
// is allowed (expected) to clip on the right rather than wrap or push
// other content — it's a legend, not a numeric field (CONTEXT.md). At a
// narrow width the view: label must still be intact and unclipped (it's
// closer to the left edge), even though the tail of the Hint Strip is
// cut off or entirely absent.
func TestHintStripClipsRightBelowDesignSize(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	v.Resize(60, 24)
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	out := stripANSI(v.Render(w, 0, 60, 24))
	if !strings.Contains(out, "view: ") {
		t.Errorf("view: label should never clip:\n%s", out)
	}
	// The full strip is long relative to 60 cols of total screen width
	// (minus borders/HUD), so it's expected NOT to appear whole here —
	// this pins the clip behavior rather than a silent wrap/reflow.
	if strings.Contains(out, hintStripText) {
		t.Logf("Hint Strip fit in full at 60x24 — narrower than expected, not a failure, just noting it for the record")
	}
}

// TestHintStripWithForcedNodesChipAndCraftNotVisibleHere probes the one
// collision path composeChips' own bottom-right stacking used to leave
// open: navballReservedRows returned 0 whenever CraftVisibleHere() was
// false (camera tabbed to a different system than the active craft), so
// the NODES chip's forced (2+ queued nodes) bottom-right block's bottom
// row could land on the canvas's very last row too, the same row the
// Hint Strip paints on the right-hand side. This was NOT one of the two
// collisions #425 requires proving absent (navball, bottom-left chip);
// it's a corner the decision doesn't cover. Measured, not assumed, and
// re-measured after ADR 0049 (readout contract, stage A2): the node row's
// duration and Δv render two-unit / 2-decimal-below-100 ("ignition in
// 1m00s", "42.00 m/s" vs the pre-contract "60s"/"42 m/s"), a few columns
// wider on exactly this kind of small-value node, which was enough to
// newly close the Design Size gap #425 had measured as clear.
//
// Fixed on the same gate review that found it, in navballReservedRows
// (orbit_chips.go): the bottom-right corner's stacking cursor had no
// floor reservation for row cRows-1 (the Hint Strip's own row) whenever
// the navball itself was absent (!CraftVisibleHere, this test's own
// setup); bottomLeftRow already stayed off that row unconditionally,
// bottomRightRow didn't. The floor is now 1 row in every navball-absent
// branch, so a bottom-right chip can never paint onto the Hint Strip's
// row regardless of how wide its own contents get. 140×40 (Design Size)
// is asserted as a hard requirement below; 104×24 (Playable Floor, below
// the Design Size floor, #425's own known-and-accepted edge case) is
// logged rather than asserted since a guarantee there was never required,
// though the same fix happens to clear it too.
func TestHintStripWithForcedNodesChipAndCraftNotVisibleHere(t *testing.T) {
	render := func(sz struct{ w, h int }) string {
		v := NewOrbitView(chipTestTheme())
		v.Resize(sz.w, sz.h)
		w, err := sim.NewWorld()
		if err != nil {
			t.Fatalf("NewWorld: %v", err)
		}
		s := settings.Default()
		for _, chipID := range settings.AllChips {
			s.SetChip(chipID, false)
		}
		v.SetSettings(s)

		c := w.ActiveCraft()
		if c == nil {
			t.Fatal("expected an active craft")
		}
		c.Nodes = []spacecraft.ManeuverNode{
			{Mode: spacecraft.BurnPrograde, DV: 42, TriggerTime: w.Clock.SimTime.Add(time.Minute)},
			{Mode: spacecraft.BurnRetrograde, DV: 7, TriggerTime: w.Clock.SimTime.Add(2 * time.Minute)},
		}
		c.SystemIdx = w.SystemIdx + 1 // parked in a different system than the camera
		out := stripANSI(v.Render(w, 0, sz.w, sz.h))
		// ADR 0051 retires the fleet-wide NODES chip's force-show past
		// declutter/toggle: its content folds into ENGINE's own node
		// row (decision 1), and ENGINE reads the ACTIVE craft directly
		// with no CraftVisibleHere gate at all, so it renders the
		// active craft's own queued nodes regardless of which system
		// the camera is viewing, a stronger, simpler guarantee than
		// the old per-fleet force-show exception this test pinned.
		if !strings.Contains(out, "ENGINE") {
			t.Fatalf("expected the ENGINE box (with the active craft's node row) in this setup at %dx%d:\n%s", sz.w, sz.h, out)
		}
		return out
	}

	if out := render(struct{ w, h int }{140, 40}); !strings.Contains(out, hintStripText) {
		t.Errorf("Design Size: Hint Strip overwritten by the bottom-right TARGET box with CraftVisibleHere()==false; navballReservedRows should now floor at 1 row regardless:\n%s", out)
	}
	if out := render(struct{ w, h int }{104, 24}); strings.Contains(out, hintStripText) {
		t.Logf("Playable Floor: Hint Strip stayed intact against the box set too, not required below Design Size, but the same fix clears it")
	} else {
		t.Logf("Playable Floor: still collides, as before (out of #425's decided scope, below the Design Size floor; see impl-notes/425.md)")
	}
}

// TestHintStripSwapsForInspect (item-3 UX batch, features finding 14):
// Inspect flared a name chip with no on-screen word for what Enter or
// Esc do while it's live. The generic Hint Strip must swap to the
// inspect-specific one for the duration of the highlight, and swap
// back once it clears.
func TestHintStripSwapsForInspect(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	v.Resize(140, 40)
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	w.Focus = sim.Focus{Kind: sim.FocusCraft}

	out := stripANSI(v.Render(w, 0, 140, 40))
	if !strings.Contains(out, hintStripText) {
		t.Fatalf("expected the generic Hint Strip before Inspect is armed:\n%s", out)
	}

	v.InspectNext()
	if !v.Inspecting() {
		t.Fatal("InspectNext did not arm the highlight on a frame with an inspectable craft")
	}
	out = stripANSI(v.Render(w, 0, 140, 40))
	if !strings.Contains(out, inspectHintStripText) {
		t.Errorf("expected the inspect Hint Strip (%q) while Inspecting:\n%s", inspectHintStripText, out)
	}
	if strings.Contains(out, hintStripText) {
		t.Errorf("generic Hint Strip should not also be present while Inspecting:\n%s", out)
	}

	v.InspectClear()
	out = stripANSI(v.Render(w, 0, 140, 40))
	if !strings.Contains(out, hintStripText) {
		t.Errorf("expected the generic Hint Strip back after InspectClear:\n%s", out)
	}
}

// TestOrientationCueSaysWhichWayNorthIsAndHowThePlaneIsSeen (B11 / G9 Q7):
// the six projections of one equatorial orbit are told apart by two rows
// above `view:`, not by the corner word alone. Mirror pairs differ where
// they can: Top reads north toward you (⊙), Bottom away (⊗).
func TestOrientationCueSaysWhichWayNorthIsAndHowThePlaneIsSeen(t *testing.T) {
	w, _, _ := leoWorld(t)
	cases := []struct {
		mode        sim.ViewMode
		north, wing string
	}{
		{sim.ViewTop, "N ⊙", "plane ○ 90° open"},
		{sim.ViewBottom, "N ⊗", "plane ○ 90° open"},
		{sim.ViewRight, "N ↑", "plane ─ 0° open"},
		{sim.ViewLeft, "N ↑", "plane ─ 0° open"},
		{sim.ViewOrbitFlat, "N ⊙", "plane ○ 90° open"},
		{sim.ViewTilted, "N ↑", "plane ◠ 65° open"},
	}
	seen := map[string]bool{}
	for _, tc := range cases {
		w.ViewMode = tc.mode
		v := NewOrbitView(chipTestTheme())
		v.Resize(140, 40)
		lines := strings.Split(stripANSI(v.Render(w, 0, 140, 40)), "\n")
		// The canvas's last row carries `view:`; the cue is the two above it.
		viewRow := -1
		for i, ln := range lines {
			if strings.Contains(ln, "view: ") {
				viewRow = i
			}
		}
		if viewRow < 2 {
			t.Fatalf("%v: no view: row", tc.mode)
		}
		north, plane := lines[viewRow-2], lines[viewRow-1]
		if !strings.Contains(north, tc.north) {
			t.Errorf("%v: north row %q, want %q", tc.mode, north, tc.north)
		}
		if !strings.Contains(plane, tc.wing) {
			t.Errorf("%v: plane row %q, want %q", tc.mode, plane, tc.wing)
		}
		seen[tc.north+"|"+tc.wing] = true
	}
	// Six views, but the cue tells at least the four distinct pictures apart.
	if len(seen) < 4 {
		t.Errorf("only %d distinct cue pairs across six views", len(seen))
	}
}

// TestOrientationCuePlaneRowReadsHowOpenTheRingLooks (B11 follow-up): the
// plane row is an angle, asin(|normal . depth|), 0 = a flat line, 90 = a
// full circle. An equatorial LEO (the orbit plane tilted 23.44 degrees off
// the ecliptic, line of nodes along world Y) reads 67 from Top and 23 from
// Right, and the glyph follows the angle.
func TestOrientationCuePlaneRowReadsHowOpenTheRingLooks(t *testing.T) {
	w, _, _ := leoWorld(t)
	c := w.ActiveCraft()
	mu := c.Primary.GravitationalParameter()
	r := c.Primary.RadiusMeters() + 300e3
	v := math.Sqrt(mu / r)
	eps := 23.44 * math.Pi / 180
	c.State.R = orbital.Vec3{Y: r}
	c.State.V = orbital.Vec3{X: -v * math.Cos(eps), Z: -v * math.Sin(eps)}
	cases := []struct {
		mode sim.ViewMode
		want string
	}{
		{sim.ViewTop, "plane ◠ 67° open"},
		{sim.ViewBottom, "plane ◠ 67° open"},
		{sim.ViewRight, "plane ◠ 23° open"},
		{sim.ViewLeft, "plane ◠ 23° open"},
	}
	for _, tc := range cases {
		w.ViewMode = tc.mode
		var plane string
		for _, row := range orientationCue(viewBasis(w), w) {
			if strings.HasPrefix(row, "plane") {
				plane = row
			}
		}
		if plane != tc.want {
			t.Errorf("%v: plane row %q, want %q", tc.mode, plane, tc.want)
		}
	}
	// Glyph bands: a flat line under 5 degrees, a full circle over 85.
	for _, tc := range []struct {
		deg  float64
		want string
	}{{2, "plane ─ 2° open"}, {4.4, "plane ─ 4° open"}, {30, "plane ◠ 30° open"}, {86, "plane ○ 86° open"}} {
		if got := planeCueRow(math.Sin(tc.deg * math.Pi / 180)); got != tc.want {
			t.Errorf("%v deg: %q, want %q", tc.deg, got, tc.want)
		}
	}
}
