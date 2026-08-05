package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/tui/screens"
)

// Inspect's App half (ADR 0041 §3 / #346): the key that steps it, the Enter
// that commits it into the Target slot, and the Esc that leaves — plus the
// two contracts it must NOT break (Esc still opens the menu when nothing is
// inspected, and the body Cursor is untouched).

// inspectApp builds an app whose orbit view has rendered at least once, so
// the inspectable set exists — Inspect steps through what the map DREW, so
// there is nothing to step through before the first frame.
func inspectApp(t *testing.T) *App {
	t.Helper()
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	a.Update(tea.WindowSizeMsg{Width: 200, Height: 60})
	a.View()
	return a
}

func pressRune(a *App, r rune) {
	a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
}

// TestInspectKeyIsFreeElsewhereInTheKeymap pins the key choice: `j` must
// not collide with any other binding, or one press would fire two verbs.
func TestInspectKeyIsFreeElsewhereInTheKeymap(t *testing.T) {
	keys := DefaultKeymap()
	if got := keys.Inspect.Keys(); len(got) != 1 || got[0] != "j" {
		t.Fatalf("Inspect.Keys() = %v, want exactly [\"j\"]", got)
	}
	if got := keys.InspectCommit.Keys(); len(got) != 1 || got[0] != "enter" {
		t.Fatalf("InspectCommit.Keys() = %v, want exactly [\"enter\"]", got)
	}

	a := inspectApp(t)
	// `j` must reach Inspect and nothing else: no screen change, no focus
	// or target mutation, no vessel switch.
	before := struct {
		active   screenID
		focus    sim.Focus
		target   sim.Target
		craftIdx int
	}{a.active, a.world.Focus, a.world.Target, a.world.ActiveCraftIdx}

	pressRune(a, 'j')
	if a.active != before.active {
		t.Errorf("[j] changed screen: %v -> %v", before.active, a.active)
	}
	if a.world.Focus != before.focus {
		t.Errorf("[j] moved Focus: %+v -> %+v", before.focus, a.world.Focus)
	}
	if a.world.Target != before.target {
		t.Errorf("[j] moved Target: %+v -> %+v", before.target, a.world.Target)
	}
	if a.world.ActiveCraftIdx != before.craftIdx {
		t.Errorf("[j] switched the active vessel: %d -> %d", before.craftIdx, a.world.ActiveCraftIdx)
	}
	if !a.orbitView.Inspecting() {
		t.Error("[j] did not start Inspect")
	}
}

// TestEscExitsInspectWithoutOpeningTheMenu: Esc's first job while a
// highlight is up is to put the map back. Falling into the save/load modal
// instead would be a jarring non-sequitur right after asking a question.
func TestEscExitsInspectWithoutOpeningTheMenu(t *testing.T) {
	a := inspectApp(t)
	pressRune(a, 'j')
	if !a.orbitView.Inspecting() {
		t.Skip("nothing inspectable in the default world frame")
	}
	orbit := a.active

	a.Update(tea.KeyMsg{Type: tea.KeyEscape})
	if a.orbitView.Inspecting() {
		t.Error("esc did not exit Inspect")
	}
	if a.active != orbit {
		t.Errorf("esc while inspecting left the orbit screen (now %v) — it should only clear the highlight", a.active)
	}

	// With nothing inspected, Esc keeps its existing meaning.
	a.Update(tea.KeyMsg{Type: tea.KeyEscape})
	if a.active == orbit {
		t.Error("esc with nothing inspected no longer opens the menu")
	}
}

// TestEnterCommitsInspectedBodyAsTarget is the hover→commit contract
// generalised: what the highlight is on becomes the Target, the highlight
// comes down, and the body Cursor follows so the planners (H / I / P) and
// the map agree about which body was chosen.
func TestEnterCommitsInspectedBodyAsTarget(t *testing.T) {
	a := inspectApp(t)
	a.world.Focus = sim.Focus{Kind: sim.FocusSystem}
	a.View()

	// Step until a targetable body is under the highlight.
	found := false
	for i := 0; i < 64 && !found; i++ {
		pressRune(a, 'j')
		if !a.orbitView.Inspecting() {
			break
		}
		ref, targetable, ok := a.orbitView.InspectedRef()
		found = ok && targetable && ref.Kind == screens.InspectBody
	}
	if !found {
		t.Skip("no targetable body inspectable in the default system view")
	}
	ref, _, _ := a.orbitView.InspectedRef()

	a.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if a.world.Target.Kind != sim.TargetBody {
		t.Fatalf("Target.Kind after commit = %v, want TargetBody", a.world.Target.Kind)
	}
	got := a.world.System().Bodies[a.world.Target.BodyIdx].ID
	if got != ref.BodyID {
		t.Errorf("committed target body = %q, want the inspected %q", got, ref.BodyID)
	}
	if a.selectedBody != a.world.Target.BodyIdx {
		t.Errorf("body Cursor at %d after commit, want %d — Cursor and Target disagree",
			a.selectedBody, a.world.Target.BodyIdx)
	}
	if a.orbitView.Inspecting() {
		t.Error("committing did not exit Inspect")
	}
}

// TestEnterCommitsInspectedVesselAsTarget: the generalisation past bodies
// is the whole point of Inspect, so a sister vessel must commit the same
// way — through the existing per-craft Target binding.
func TestEnterCommitsInspectedVesselAsTarget(t *testing.T) {
	a := inspectApp(t)
	active := a.world.ActiveCraft()
	if active == nil {
		t.Skip("no active vessel in the default world")
	}
	sister := *active
	sister.Name = "Sister"
	sister.ID = 0
	sister.State.R = active.State.R.Add(orbital.Vec3{X: 20_000})
	a.world.Crafts = append(a.world.Crafts, &sister)
	a.world.EnsureCraftIDs()
	a.world.Focus = sim.Focus{Kind: sim.FocusCraft}
	a.View()

	found := false
	for i := 0; i < 64 && !found; i++ {
		pressRune(a, 'j')
		if !a.orbitView.Inspecting() {
			break
		}
		ref, targetable, ok := a.orbitView.InspectedRef()
		found = ok && targetable && ref.Kind == screens.InspectVessel && ref.CraftID == sister.ID
	}
	if !found {
		t.Skip("the sister vessel did not render into the inspectable set")
	}

	a.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if a.world.Target.Kind != sim.TargetCraft || a.world.Target.CraftID != sister.ID {
		t.Errorf("Target after commit = %+v, want the sister vessel (id %d)", a.world.Target, sister.ID)
	}
	if a.orbitView.Inspecting() {
		t.Error("committing a vessel did not exit Inspect")
	}
}

// TestEnterOnOwnVesselIsANoOpThatSaysSo: a vessel can't be its own target,
// so Enter refuses — visibly. A silent refusal reads as a broken key, and
// the refusal must not clear the Target the player already had.
func TestEnterOnOwnVesselIsANoOpThatSaysSo(t *testing.T) {
	a := inspectApp(t)
	active := a.world.ActiveCraft()
	if active == nil {
		t.Skip("no active vessel in the default world")
	}
	a.world.Focus = sim.Focus{Kind: sim.FocusCraft}
	a.world.SetTargetBody(1)
	before := a.world.Target
	a.View()

	found := false
	for i := 0; i < 64 && !found; i++ {
		pressRune(a, 'j')
		if !a.orbitView.Inspecting() {
			break
		}
		ref, _, ok := a.orbitView.InspectedRef()
		found = ok && ref.Kind == screens.InspectVessel && ref.CraftID == active.ID
	}
	if !found {
		t.Skip("the active vessel did not render into the inspectable set")
	}

	a.statusMsg = ""
	a.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if a.world.Target != before {
		t.Errorf("Enter on your own vessel changed the Target: %+v -> %+v", before, a.world.Target)
	}
	if !a.orbitView.Inspecting() {
		t.Error("a refused commit exited Inspect — a no-op should leave the highlight where it was")
	}
	if a.statusMsg == "" {
		t.Error("a refused commit said nothing — a silent no-op reads as a broken key")
	}
}

// TestEnterIsInertWhenNothingIsInspected: the commit binding is gated on
// Inspect being up, so Enter stays a free key on the orbit screen.
func TestEnterIsInertWhenNothingIsInspected(t *testing.T) {
	a := inspectApp(t)
	if a.orbitView.Inspecting() {
		t.Fatal("Inspect is up before any [j] press")
	}
	before := struct {
		active screenID
		target sim.Target
	}{a.active, a.world.Target}

	a.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if a.active != before.active {
		t.Errorf("enter with nothing inspected changed screen: %v -> %v", before.active, a.active)
	}
	if a.world.Target != before.target {
		t.Errorf("enter with nothing inspected moved the Target: %+v -> %+v", before.target, a.world.Target)
	}
}

// TestBodyClickStillMovesTheCursorAndAlsoInspects: Inspect is additive on
// the mouse. A body click keeps its existing meaning (move the body Cursor)
// AND now answers what it is — the two are the same act, not competing ones.
func TestBodyClickStillMovesTheCursorAndAlsoInspects(t *testing.T) {
	a := inspectApp(t)
	a.world.Focus = sim.Focus{Kind: sim.FocusSystem}
	a.View()

	col, row, bodyID, ok := findBodyCell(a)
	if !ok {
		t.Skip("no body disk on the canvas to click")
	}
	a.selectedBody = -1
	a.Update(tea.MouseMsg{X: col, Y: row, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})

	if a.selectedBody < 0 || a.world.System().Bodies[a.selectedBody].ID != bodyID {
		t.Errorf("clicking body %q did not move the body Cursor onto it (cursor at %d)", bodyID, a.selectedBody)
	}
	ref, _, inspecting := a.orbitView.InspectedRef()
	if !inspecting || ref.Kind != screens.InspectBody || ref.BodyID != bodyID {
		t.Errorf("clicking body %q did not inspect it (got %+v, inspecting=%v)", bodyID, ref, inspecting)
	}
}

// TestClickingAnotherVesselsOrbitLineInspectsInsteadOfPlanting is the
// behaviour ADR 0041 §3 adds to the mouse: an orbit-line pixel used to fall
// through to "stage a burn at the nearest point on YOUR orbit", which is
// both wrong and destructive when the line belongs to someone else. Now it
// answers whose line it is and stops there.
func TestClickingAnotherVesselsOrbitLineInspectsInsteadOfPlanting(t *testing.T) {
	a := inspectApp(t)
	active := a.world.ActiveCraft()
	if active == nil {
		t.Skip("no active vessel in the default world")
	}
	// A sister vessel on a clearly larger orbit, so its ellipse has pixels
	// nowhere near the active vessel's.
	sister := *active
	sister.Name = "Sister"
	sister.ID = 0
	sister.State.R = active.State.R.Scale(1.6)
	sister.State.V = active.State.V.Scale(1 / 1.6)
	a.world.Crafts = append(a.world.Crafts, &sister)
	a.world.EnsureCraftIDs()
	a.world.Focus = sim.Focus{Kind: sim.FocusCraft}
	a.orbitView.ZoomOut()
	a.orbitView.ZoomOut()
	a.View()

	want := screens.InspectRef{Kind: screens.InspectVessel, CraftID: sister.ID}
	col, row, ok := findOwnerCell(a, want.OwnerKey())
	if !ok {
		t.Skip("the sister vessel's orbit line did not land on the canvas")
	}

	nodesBefore := len(a.world.ActiveCraft().Nodes)
	orbit := a.active
	a.Update(tea.MouseMsg{X: col, Y: row, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})

	ref, _, inspecting := a.orbitView.InspectedRef()
	if !inspecting || ref != want {
		t.Errorf("clicking the sister's orbit line inspected %+v (inspecting=%v), want %+v", ref, inspecting, want)
	}
	if a.active != orbit {
		t.Errorf("clicking another vessel's orbit line left the map for %v — it should not open the planner", a.active)
	}
	if n := len(a.world.ActiveCraft().Nodes); n != nodesBefore {
		t.Errorf("clicking another vessel's orbit line planted a node (%d -> %d)", nodesBefore, n)
	}
}

// findOwnerCell scans the orbit canvas for a cell whose hit-test resolves
// to the given Inspect owner key, returning its SCREEN coordinates.
func findOwnerCell(a *App, owner string) (col, row int, ok bool) {
	for r := 2; r < a.height-1; r++ {
		for c := 1; c < a.width-1; c++ {
			hit := a.orbitView.HitAt(c, r)
			if hit.Owner == owner && hit.BodyID == "" && !hit.IsVessel && hit.NodeIdx == 0 {
				return c, r, true
			}
		}
	}
	return 0, 0, false
}

// findBodyCell scans the orbit canvas for a cell whose hit-test resolves to
// a body disk, returning its SCREEN coordinates (canvas content sits one
// col in and two rows down, behind the border and the title).
func findBodyCell(a *App) (col, row int, bodyID string, ok bool) {
	for r := 2; r < a.height-1; r++ {
		for c := 1; c < a.width-1; c++ {
			if hit := a.orbitView.HitAt(c, r); hit.BodyID != "" {
				return c, r, hit.BodyID, true
			}
		}
	}
	return 0, 0, "", false
}
