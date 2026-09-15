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
// "(+N more → [m])" count when the active craft has more than one node
// queued, with no count at all for a single queued node. Sabotage-first
// (see this test's own red proof in the slice 2a fixes log): removing
// the `len(c.Nodes) > 1` count branch leaves this red.
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
	if strings.Contains(lines[3], "more") {
		t.Errorf("node row with a single queued node = %q, should not carry an overflow count", lines[3])
	}

	// Two queued nodes: "(+1 more → [m])".
	c.Nodes = append(c.Nodes, spacecraft.ManeuverNode{
		DV: 80, TriggerTime: w.Clock.SimTime.Add(30 * time.Minute), Mode: spacecraft.BurnPrograde,
	})
	lines = v.buildEngineBox(w)
	if !strings.Contains(lines[3], "(+1 more → [m])") {
		t.Errorf("node row with two queued nodes = %q, want the overflow count (+1 more → [m])", lines[3])
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
