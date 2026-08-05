package screens

import (
	"strings"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/render"
	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// Inspect (ADR 0041 §3 / #346) — the map answers "what is this?" for one
// entity at a time, on demand. These tests pin the four properties that
// make that trustworthy: the cycle is deterministic, it offers exactly
// what the frame drew, the answer is visible (flare + one chip), and it
// never becomes standing ink.

// newInspectTestView builds an orbit view at a real-terminal size. 80×24
// is the floor the project supports, so the chip placement and the cycle
// have to hold there, not only on a wide window.
func newInspectTestView(cols, rows int) *OrbitView {
	v := NewOrbitView(ghostTestTheme())
	v.Resize(cols, rows)
	return v
}

// inspectCycle steps the highlight all the way round and returns the owner
// keys it visited, in order. It stops at the wrap-through-none slot, which
// is the cycle's own end condition.
func inspectCycle(v *OrbitView) []string {
	var seen []string
	for i := 0; i < len(v.inspectables)+1; i++ {
		v.InspectNext()
		if !v.Inspecting() {
			break
		}
		seen = append(seen, v.inspectRef.OwnerKey())
	}
	return seen
}

// TestInspectCycleOrderIsDeterministic: two identical frames must offer the
// same entities in the same order. Cycle order is the map's own draw order
// (ADR 0020 D's deterministic overlap priority), so a player who learns
// "two presses gets me the Moon" can rely on it for as long as the frame
// holds.
func TestInspectCycleOrderIsDeterministic(t *testing.T) {
	w, _, _ := leoWorld(t)
	w.Focus = sim.Focus{Kind: sim.FocusCraft}

	v1 := newInspectTestView(200, 60)
	v1.Render(w, 0, 200, 60)
	first := inspectCycle(v1)

	v2 := newInspectTestView(200, 60)
	v2.Render(w, 0, 200, 60)
	second := inspectCycle(v2)

	if len(first) == 0 {
		t.Fatal("nothing was inspectable in a frame with a vessel and bodies on screen")
	}
	if strings.Join(first, ",") != strings.Join(second, ",") {
		t.Errorf("cycle order differed between identical frames:\n  %v\n  %v", first, second)
	}

	// Re-cycling the SAME view must also repeat: the lap wraps through
	// "nothing inspected" and starts again from the top.
	again := inspectCycle(v1)
	if strings.Join(first, ",") != strings.Join(again, ",") {
		t.Errorf("second lap of the same view differed:\n  %v\n  %v", first, again)
	}
}

// TestInspectCycleWrapsThroughNothingInspected: the lap ends in the empty
// state rather than jumping straight back to the first entity, so stepping
// is also the way OUT of Inspect.
func TestInspectCycleWrapsThroughNothingInspected(t *testing.T) {
	w, _, _ := leoWorld(t)
	w.Focus = sim.Focus{Kind: sim.FocusCraft}
	v := newInspectTestView(80, 24)
	v.Render(w, 0, 80, 24)

	n := v.InspectableCount()
	if n == 0 {
		t.Fatal("nothing inspectable at 80x24")
	}
	for i := 0; i < n; i++ {
		v.InspectNext()
		if !v.Inspecting() {
			t.Fatalf("cycle emptied after %d of %d entities", i+1, n)
		}
	}
	v.InspectNext()
	if v.Inspecting() {
		t.Errorf("cycle did not wrap through the nothing-inspected state after %d entities", n)
	}
}

// TestInspectSetHoldsOnlyWhatIsDrawn is the ADR's "if it isn't on screen,
// it isn't inspectable" rule. A second vessel parked far outside the
// framed view is not offered — the set is built at the draw sites, so
// nothing that didn't ink can appear in it.
func TestInspectSetHoldsOnlyWhatIsDrawn(t *testing.T) {
	w, _, _ := leoWorld(t)
	w.Focus = sim.Focus{Kind: sim.FocusCraft}
	active := w.ActiveCraft()

	// A sister vessel a long way off along +X: same primary, same frame,
	// but nowhere near the canvas fit to the active vessel's LEO.
	far := *active
	far.ID = active.ID + 1
	far.Name = "Faraway"
	far.State.R = active.State.R.Add(orbital.Vec3{X: 5e11})
	w.Crafts = append(w.Crafts, &far)

	near := *active
	near.ID = active.ID + 2
	near.Name = "Nearby"
	near.State.R = active.State.R.Add(orbital.Vec3{X: 5_000})
	w.Crafts = append(w.Crafts, &near)

	v := newInspectTestView(200, 60)
	v.Render(w, 0, 200, 60)

	var names []string
	for _, it := range v.inspectables {
		names = append(names, it.name)
	}
	joined := strings.Join(names, ",")
	if !strings.Contains(joined, "Nearby") {
		t.Errorf("an on-screen sister vessel is missing from the inspectable set: %v", names)
	}
	if strings.Contains(joined, "Faraway") {
		t.Errorf("an off-screen vessel is inspectable: %v", names)
	}
}

// TestInspectFlaresAndNamesExactlyOne: the answer has to be visible. The
// inspected entity redraws in the flare colour and gets a name chip beside
// it — and only one chip, because Inspect is on-demand identity, never
// standing ink.
func TestInspectFlaresAndNamesExactlyOne(t *testing.T) {
	w, _, _ := leoWorld(t)
	w.Focus = sim.Focus{Kind: sim.FocusCraft}
	v := newInspectTestView(200, 60)
	v.Render(w, 0, 200, 60)

	if n := v.canvas.CountColor(render.ColorInspect); n != 0 {
		t.Fatalf("the map flares %d cells with nothing inspected — Inspect is standing ink", n)
	}

	// Find the active vessel, which is always drawn when focused on it.
	activeRef, target := InspectRef{}, ""
	for _, it := range v.inspectables {
		if it.ref.Kind == InspectVessel && it.ref.CraftID == w.ActiveCraft().ID {
			activeRef, target = it.ref, it.name
			break
		}
	}
	if target == "" {
		t.Fatal("the active vessel is not inspectable in a frame focused on it")
	}

	// The vessel's name also appears in the always-on VESSEL chip, so the
	// name chip's arrival is measured as a DELTA: inspecting must add
	// exactly one more mention of the name to the frame, not one per entity.
	baseline := strings.Count(stripANSI(v.Render(w, 0, 200, 60)), target)

	v.inspectRef = activeRef
	out := stripANSI(v.Render(w, 0, 200, 60))
	if n := v.canvas.CountColor(render.ColorInspect); n == 0 {
		t.Error("inspecting the active vessel lit no flare-coloured cells")
	}
	if got := strings.Count(out, target); got != baseline+1 {
		t.Errorf("inspecting added %d mentions of %q (baseline %d), want exactly 1 name chip",
			got-baseline, target, baseline)
	}

	v.InspectClear()
	out = stripANSI(v.Render(w, 0, 200, 60))
	if n := v.canvas.CountColor(render.ColorInspect); n != 0 {
		t.Errorf("clearing Inspect left %d flare cells behind", n)
	}
	if got := strings.Count(out, target); got != baseline {
		t.Errorf("after InspectClear the name appears %d times, want the %d-mention baseline — the chip is standing ink",
			got, baseline)
	}
}

// TestPlantedNodeIsInspectableAndNotTargetable: the inspectable set is not
// only vessels and bodies — a planted burn is a thing on the map with an
// identity ("Node 1"), so it answers too. It is deliberately NOT
// targetable: the Target slot has no node kind, and Enter must refuse
// rather than pretend.
func TestPlantedNodeIsInspectableAndNotTargetable(t *testing.T) {
	v := newSOIPassTestView()
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	setupKernCursorTransfer(t, w)
	w.ViewMode = sim.ViewTilted
	w.Focus = sim.Focus{Kind: sim.FocusCraft}

	v.Render(w, 0, 200, 60) // establishes baseScale before zooming out
	for i := 0; i < 5; i++ {
		v.ZoomOut()
	}
	v.Render(w, 0, 200, 60)

	var node *inspectable
	for i := range v.inspectables {
		if v.inspectables[i].ref.Kind == InspectNode {
			node = &v.inspectables[i]
			break
		}
	}
	if node == nil {
		t.Skip("no planted node landed on the canvas in this fixture")
	}
	if node.name != "Node 1" {
		t.Errorf("node chip name = %q, want %q", node.name, "Node 1")
	}
	if node.targetable {
		t.Error("a planted node reports itself targetable — the Target slot has no node kind")
	}

	v.inspectRef = node.ref
	v.Render(w, 0, 200, 60)
	if n := v.canvas.CountColor(render.ColorInspect); n == 0 {
		t.Error("inspecting a planted node lit no flare-coloured cells")
	}
}

// TestInspectByOwnerMatchesTheKeyCycle: the mouse path must set exactly
// what the key cycle sets. If those ever diverge, a click and a keypress
// would put the map in two different states with the same appearance.
func TestInspectByOwnerMatchesTheKeyCycle(t *testing.T) {
	w, _, _ := leoWorld(t)
	w.Focus = sim.Focus{Kind: sim.FocusCraft}
	v := newInspectTestView(200, 60)
	v.Render(w, 0, 200, 60)

	if v.InspectableCount() < 2 {
		t.Skip("need at least two inspectable entities to discriminate")
	}
	want := v.inspectables[1].ref

	ref, ok := v.InspectByOwner(want.OwnerKey())
	if !ok {
		t.Fatalf("InspectByOwner(%q) found nothing drawn this frame", want.OwnerKey())
	}
	if ref != want {
		t.Errorf("InspectByOwner resolved %+v, want %+v", ref, want)
	}
	if got, _, ok := v.InspectedRef(); !ok || got != want {
		t.Errorf("InspectedRef after a click = %+v (ok=%v), want %+v", got, ok, want)
	}

	// An owner key from no drawn entity must not move the highlight.
	if _, ok := v.InspectByOwner("v:999999"); ok {
		t.Error("InspectByOwner accepted a key belonging to nothing on screen")
	}
	if got, _, _ := v.InspectedRef(); got != want {
		t.Errorf("a miss moved the highlight to %+v", got)
	}
}

// TestInspectClearsOnFramingEvent: the highlight belongs to the frame the
// player is looking at, so refocusing ends it rather than leaving a flare
// on something they've stopped looking for.
func TestInspectClearsOnFramingEvent(t *testing.T) {
	w, _, _ := leoWorld(t)
	w.Focus = sim.Focus{Kind: sim.FocusCraft}
	v := newInspectTestView(200, 60)
	v.Render(w, 0, 200, 60)
	v.InspectNext()
	if !v.Inspecting() {
		t.Fatal("nothing inspectable to start from")
	}

	w.Focus = sim.Focus{Kind: sim.FocusSystem}
	v.Render(w, 0, 200, 60)
	if v.Inspecting() {
		t.Error("Inspect survived a focus change (a Framing Event)")
	}
}

// TestInspectableSetSurvivesNarrowTerminal: 80×24 is the supported floor,
// and the whole feature is worthless if the cycle or the chip falls over
// there. The chip must be inside the frame, not overflowing it.
func TestInspectableSetSurvivesNarrowTerminal(t *testing.T) {
	w, _, _ := leoWorld(t)
	w.Focus = sim.Focus{Kind: sim.FocusCraft}
	v := newInspectTestView(80, 24)
	plain := stripANSI(v.Render(w, 0, 80, 24))
	if v.InspectableCount() == 0 {
		t.Fatal("nothing inspectable at 80x24")
	}
	v.InspectNext()
	if !v.Inspecting() {
		t.Fatal("stepping the highlight found nothing at 80x24")
	}
	flared := stripANSI(v.Render(w, 0, 80, 24))

	// The chip must fit INSIDE the frame it is drawn on: at the supported
	// floor it is easy to push a block off the right edge, where
	// overlayStyledBlock would silently clip it (or a naive join would
	// widen the row).
	if got, want := maxLineWidth(flared), maxLineWidth(plain); got != want {
		t.Errorf("Inspect changed the frame's widest row at 80x24: %d cells, want %d", got, want)
	}
	if got, want := strings.Count(flared, "\n"), strings.Count(plain, "\n"); got != want {
		t.Errorf("Inspect changed the frame's row count at 80x24: %d, want %d", got, want)
	}
	if name := v.InspectedName(); name != "" && !strings.Contains(flared, name) {
		t.Errorf("the name chip (%q) did not fit on an 80x24 frame", name)
	}
}

func maxLineWidth(s string) int {
	max := 0
	for _, line := range strings.Split(s, "\n") {
		if n := len([]rune(line)); n > max {
			max = n
		}
	}
	return max
}

// TestInspectRoundTripLeavesTheFrameUnchanged: Inspect integrates with the
// existing map — the body Cursor included — rather than replacing it. A
// full trip in and out of Inspect must return the exact same frame, so
// nothing it draws can leak into the ordinary render.
func TestInspectRoundTripLeavesTheFrameUnchanged(t *testing.T) {
	w, _, _ := leoWorld(t)
	w.Focus = sim.Focus{Kind: sim.FocusCraft}

	v := newInspectTestView(200, 60)
	plain := v.Render(w, 1, 200, 60)

	v.InspectNext()
	if !v.Inspecting() {
		t.Fatal("nothing inspectable to round-trip through")
	}
	if flared := v.Render(w, 1, 200, 60); flared == plain {
		t.Fatal("inspecting changed nothing on screen — the fixture can't detect a leak")
	}
	v.InspectClear()
	if got := v.Render(w, 1, 200, 60); got != plain {
		t.Error("a round trip through Inspect changed the ordinary frame")
	}
}
