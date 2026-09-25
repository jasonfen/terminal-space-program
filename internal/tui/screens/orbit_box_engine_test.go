// ADR 0051 slice 2a: the ENGINE box's node-row precedence (decision 12,
// re-grill Q2/Q4) is the load-bearing logic in this box, a live burn
// must outrank a braking start, which must outrank a queued node, which
// must outrank a dash. Each test below sabotage-checks one rung of that
// order rather than only asserting the happy path, per the "prove a
// guard goes red against the unfixed behaviour" discipline (memory:
// guard tests sabotage-first).

package screens

import (
	"strings"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// TestEngineBoxNodeRowDashWithNoCraftActivity: the pad, no node, no burn,
// not descending, the row reads a bare dash rather than vanishing
// (decision 2, every row always present).
func TestEngineBoxNodeRowDashWithNoCraftActivity(t *testing.T) {
	v := NewOrbitView(launchThemeForTest())
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	lines := v.buildEngineBox(w)
	if len(lines) != 4 {
		t.Fatalf("buildEngineBox returned %d lines, want 4 (title, throttle/mode, TWR, node)", len(lines))
	}
	if !strings.Contains(lines[3], "node:") || !strings.Contains(lines[3], "—") {
		t.Errorf("node row = %q, want a dash cell", lines[3])
	}
}

// TestEngineBoxNodeRowQueuedNode: with a node planted and nothing else
// going on, the row reads the queued node.
func TestEngineBoxNodeRowQueuedNode(t *testing.T) {
	v := NewOrbitView(launchThemeForTest())
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()
	c.Nodes = append(c.Nodes, spacecraft.ManeuverNode{
		DV:          150,
		TriggerTime: w.Clock.SimTime.Add(10 * time.Minute),
		Mode:        spacecraft.BurnPrograde,
	})
	lines := v.buildEngineBox(w)
	if !strings.Contains(lines[3], "ignition") && !strings.Contains(lines[3], "in") {
		t.Errorf("node row with a queued node = %q, want an ignition/event line, not a dash", lines[3])
	}
	if strings.Contains(lines[3], "—") {
		t.Errorf("node row = %q, should not be a dash once a node is queued", lines[3])
	}
}

// TestEngineBoxNodeRowOverBudgetIsBareGlyph: re-grill Q4, an over-budget
// queued node shows only the alarm glyph, never the words "exceeds
// budget". Sabotage-first: a naive port of the retired nextQueuedNodeLine
// would still print "exceeds budget by", so this fails against that
// unfixed behaviour and passes only once the words are actually gone.
func TestEngineBoxNodeRowOverBudgetIsBareGlyph(t *testing.T) {
	v := NewOrbitView(launchThemeForTest())
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()
	// An absurdly large Δv guarantees OverBudget trips regardless of the
	// stack's actual remaining Δv.
	c.Nodes = append(c.Nodes, spacecraft.ManeuverNode{
		DV:          1e9,
		TriggerTime: w.Clock.SimTime.Add(10 * time.Minute),
		Mode:        spacecraft.BurnPrograde,
	})
	if _, over := c.Nodes[0].OverBudget(c); !over {
		t.Fatal("setup: expected this absurd Δv to be over budget")
	}
	lines := v.buildEngineBox(w)
	if !strings.Contains(lines[3], "⚠") {
		t.Errorf("node row for an over-budget node = %q, want the ⚠ glyph", lines[3])
	}
	if strings.Contains(lines[3], "exceeds budget") {
		t.Errorf("node row = %q, the words must move to the glossary/planner, not print inline (re-grill Q4)", lines[3])
	}
}

// TestEngineBoxNodeRowLiveBurnOutranksQueuedNode: a live ActiveBurn must
// win over a queued node sitting right behind it (decision 12's
// precedence order), not just "either renders something".
func TestEngineBoxNodeRowLiveBurnOutranksQueuedNode(t *testing.T) {
	v := NewOrbitView(launchThemeForTest())
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()
	c.Nodes = append(c.Nodes, spacecraft.ManeuverNode{
		DV: 100, TriggerTime: w.Clock.SimTime.Add(10 * time.Minute), Mode: spacecraft.BurnPrograde,
	})
	c.ActiveBurn = &spacecraft.ActiveBurn{
		Mode:        spacecraft.BurnPrograde,
		DVRemaining: 42,
		EndTime:     w.Clock.SimTime.Add(30 * time.Second),
	}
	lines := v.buildEngineBox(w)
	if !strings.Contains(lines[3], "burning") {
		t.Errorf("node row with a live burn AND a queued node = %q, want the burning line (live burn outranks queued)", lines[3])
	}
	if strings.Contains(lines[3], "ignition in") {
		t.Errorf("node row = %q, should not describe the queued node while a burn is live", lines[3])
	}
}

// TestEngineBoxNodeRowBrakingStartOutranksQueuedNode: while descending
// with a safe braking start available and a (later) node also queued,
// the braking start wins, the descent alarm's forced priority (decision
// 12) must not be pre-empted by an unrelated queued node.
func TestEngineBoxNodeRowBrakingStartOutranksQueuedNode(t *testing.T) {
	v := NewOrbitView(launchThemeForTest())
	w := descendingMoonCraft(t, 20_000, 120)
	c := w.ActiveCraft()
	c.Nodes = append(c.Nodes, spacecraft.ManeuverNode{
		DV: 50, TriggerTime: w.Clock.SimTime.Add(2 * time.Hour), Mode: spacecraft.BurnPrograde,
	})
	stopDat := v.cachedDescentStop(w, c)
	if !stopDat.hasBurnAt {
		t.Fatal("setup: expected a resolvable braking start for this descent (matches launch_descent_cache_test.go's own fixture)")
	}
	lines := v.buildEngineBox(w)
	if !strings.Contains(lines[3], "braking burn") {
		t.Errorf("node row = %q, want the braking-start line to win over the queued node", lines[3])
	}
}

// TestEngineBoxNodeRowOverflowCount: engineQueuedNodeLine appends a Dim
// "+N [m]" count when the active craft has more than one node queued,
// with no count at all for a single queued node. The short form (review
// finding 1, 2026-09-25) replaced "(+N more → [m])", which pushed this
// row wide enough to overlap NAVIGATION at 138 columns; see
// TestEngineNavigationNoOverlapWithQueuedNodes for that composed-frame
// proof. Sabotage-first (see this test's own red proof in the slice 2a
// fixes log): removing the `len(c.Nodes) > 1` count branch leaves this
// red.
func TestEngineBoxNodeRowOverflowCount(t *testing.T) {
	v := NewOrbitView(launchThemeForTest())
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()

	// One queued node: no overflow count at all.
	c.Nodes = []spacecraft.ManeuverNode{
		{DV: 100, TriggerTime: w.Clock.SimTime.Add(10 * time.Minute), Mode: spacecraft.BurnPrograde},
	}
	lines := v.buildEngineBox(w)
	if strings.Contains(lines[3], "[m]") {
		t.Errorf("node row with a single queued node = %q, should not carry an overflow count", lines[3])
	}

	// Two queued nodes: "+1 [m]".
	c.Nodes = append(c.Nodes, spacecraft.ManeuverNode{
		DV: 80, TriggerTime: w.Clock.SimTime.Add(30 * time.Minute), Mode: spacecraft.BurnPrograde,
	})
	lines = v.buildEngineBox(w)
	if !strings.Contains(lines[3], "+1 [m]") {
		t.Errorf("node row with two queued nodes = %q, want the overflow count +1 [m]", lines[3])
	}
}

// TestEngineBoxTWRWillNotLiftJudgesMax: decision 13b, the verdict judges
// the MAXIMUM-throttle figure, not the current one. A craft idling at 0%
// throttle with a max TWR comfortably above 1 must NOT read "will not
// lift", even though its CURRENT TWR is exactly zero.
func TestEngineBoxTWRWillNotLiftJudgesMax(t *testing.T) {
	v := NewOrbitView(launchThemeForTest())
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()
	c.Throttle = 0
	lines := v.buildEngineBox(w)
	if strings.Contains(lines[2], "will not lift") {
		t.Errorf("TWR row = %q, current throttle is 0%% but max TWR clears 1.0 — must not read will-not-lift", lines[2])
	}
	if !strings.Contains(lines[2], "0.00") {
		t.Errorf("TWR row = %q, want the current (zero) TWR figure shown", lines[2])
	}
}

// TestEngineNavigationNoOverlapWithQueuedNodes is finding 1 of the
// slices 2-4 review (adr0051-review-slices2to4-20260925.md): ENGINE's
// node row with two or more queued nodes grew wide enough
// (72 cells, box 74) to paint under NAVIGATION (68 wide in any coast),
// even though the composed frame is only 138 columns. Composed at the
// real Render path (not the separate buildEngineBox/buildNavigationBox
// calls TestNavigationBoxWidthAtDesignSizeWithPlan used, which never
// actually overlay the two boxes), so this is a positive control for the
// real overflow, not just the two builders' own outputs.
func TestEngineNavigationNoOverlapWithQueuedNodes(t *testing.T) {
	v := NewOrbitView(launchThemeForTest())
	v.Resize(DesignWidth, DesignHeight)
	w := inclinedCircularEarthOrbitCraft(t, 45, 500e3)
	c := w.ActiveCraft()
	c.Nodes = []spacecraft.ManeuverNode{
		{DV: 3100, TriggerTime: w.Clock.SimTime.Add(time.Hour), Mode: spacecraft.BurnPrograde},
		{DV: 80, TriggerTime: w.Clock.SimTime.Add(2 * time.Hour), Mode: spacecraft.BurnPrograde},
		{DV: 60, TriggerTime: w.Clock.SimTime.Add(3 * time.Hour), Mode: spacecraft.BurnPrograde},
	}

	out := v.Render(w, 0, DesignWidth, DesignHeight)
	assertNoChipRectOverlaps(t, v.chipRects)

	engineLines := v.buildEngineBox(w)
	nodeLine := engineLines[3]
	if !strings.Contains(nodeLine, "[m]") {
		t.Fatalf("setup: expected ENGINE's node row to carry the overflow indicator: %q", nodeLine)
	}
	if !strings.HasSuffix(strings.TrimRight(nodeLine, " "), ")") && !strings.HasSuffix(strings.TrimRight(nodeLine, " "), "]") {
		t.Errorf("ENGINE node row does not end cleanly: %q", nodeLine)
	}
	_ = out
}

// TestEngineThrottleLabelClockDirectionConsistent (review finding 5,
// 2026-09-25): under the identical "● FIRING" glyph, a manual burn's
// throttle cell counted UP (T+, elapsed since ignition) while a node
// (ActiveBurn) burn's counted DOWN (T-, remaining), with nothing on the
// cell saying which convention applied. The node row (engineBurnLine)
// already prints "N left" for a node burn, so the throttle cell drops
// its own clock there rather than showing a second, oppositely-signed
// one; a manual burn, which has no node row to carry that information,
// keeps its elapsed T+.
func TestEngineThrottleLabelClockDirectionConsistent(t *testing.T) {
	v := NewOrbitView(launchThemeForTest())
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()

	c.ManualBurn = &spacecraft.ManualBurn{StartTime: w.Clock.SimTime.Add(-90 * time.Second)}
	label := v.engineThrottleLabel(w, c)
	if !strings.Contains(label, "T+") {
		t.Errorf("manual burn throttle label = %q, want an elapsed T+ clock", label)
	}
	c.ManualBurn = nil

	c.ActiveBurn = &spacecraft.ActiveBurn{
		Mode:        spacecraft.BurnPrograde,
		DVRemaining: 100,
		EndTime:     w.Clock.SimTime.Add(30 * time.Second),
	}
	label = v.engineThrottleLabel(w, c)
	if strings.Contains(label, "T+") || strings.Contains(label, "T-") {
		t.Errorf("node burn throttle label = %q, want no clock at all (the node row already carries the remaining time)", label)
	}
	if !strings.Contains(label, "FIRING") {
		t.Errorf("node burn throttle label = %q, want it to still read FIRING", label)
	}
}
