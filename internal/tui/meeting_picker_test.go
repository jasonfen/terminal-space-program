package tui

import (
	"math"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// ADR 0045 S6 (#399): K's modal decision, driven end-to-end through
// a.Update — mirrors app_rendezvous_success_test.go's own fixture shapes
// (rotateAboutAxis is defined there, same package).

// meetingPickerPhaseMismatchApp builds a fresh App with two craft on the
// SAME circular orbit (same altitude, same plane) but 90° apart in phase
// — far enough that K's trim-rung nudge exceeds the burn ceiling, while
// the coplanar, same-radius geometry gives the Meeting Planner a real
// ladder to solve. Mirrors internal/sim's rendezvousPhaseMismatchWorld.
func meetingPickerPhaseMismatchApp(t *testing.T) *App {
	t.Helper()
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := a.world.SpawnCraft(sim.SpawnSpec{AltitudeM: 600e3}); err != nil {
		t.Fatalf("SpawnCraft: %v", err)
	}
	active := a.world.Crafts[0]
	target := a.world.Crafts[1]
	h := active.State.R.Cross(active.State.V)
	axis := h.Unit()
	angle := 90 * math.Pi / 180
	target.State.R = rotateAboutAxis(active.State.R, axis, angle)
	target.State.V = rotateAboutAxis(active.State.V, axis, angle)
	target.Primary = active.Primary
	a.world.ActiveCraftIdx = 0
	a.world.SetTargetCraft(1)
	return a
}

// meetingPickerPlaneMismatchApp mirrors internal/sim's
// rendezvousPlaneMismatchWorld: same radius/speed, tilted 30° out of
// plane — a size-and-shape match, purely a plane difference.
func meetingPickerPlaneMismatchApp(t *testing.T) *App {
	t.Helper()
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := a.world.SpawnCraft(sim.SpawnSpec{AltitudeM: 600e3}); err != nil {
		t.Fatalf("SpawnCraft: %v", err)
	}
	active := a.world.Crafts[0]
	target := a.world.Crafts[1]
	axis := active.State.R.Unit()
	angle := 30 * math.Pi / 180
	target.State.R = rotateAboutAxis(active.State.R, axis, angle)
	target.State.V = rotateAboutAxis(active.State.V, axis, angle)
	target.Primary = active.Primary
	a.world.ActiveCraftIdx = 0
	a.world.SetTargetCraft(1)
	return a
}

// meetingPickerNearMatchedApp mirrors internal/sim's
// rendezvousSmallLagWorld — the fixture K's own direct-plant happy path
// test uses.
func meetingPickerNearMatchedApp(t *testing.T) *App {
	t.Helper()
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := a.world.SpawnCraft(sim.SpawnSpec{AltitudeM: 600e3}); err != nil {
		t.Fatalf("SpawnCraft: %v", err)
	}
	active := a.world.Crafts[0]
	target := a.world.Crafts[1]
	h := active.State.R.Cross(active.State.V)
	axis := h.Unit()
	angle := -0.5 * math.Pi / 180
	target.State.R = rotateAboutAxis(active.State.R, axis, angle)
	target.State.V = rotateAboutAxis(active.State.V, axis, angle)
	target.Primary = active.Primary
	a.world.ActiveCraftIdx = 0
	a.world.SetTargetCraft(1)
	return a
}

// TestPlanRendezvousKey_NearMatched_PlantsDirect_NoPicker — ADR 0045 S6
// acceptance: "K on a near-matched close pair still plants a nudge
// directly, with no picker."
func TestPlanRendezvousKey_NearMatched_PlantsDirect_NoPicker(t *testing.T) {
	a := meetingPickerNearMatchedApp(t)
	c := a.world.ActiveCraft()

	pressRune(a, 'K')

	if a.orbitView.MeetingPickerOpen() {
		t.Fatalf("picker opened on a near-matched close pair — should plant directly")
	}
	if len(c.Nodes) != 1 {
		t.Fatalf("expected 1 node planted directly, got %d (statusMsg=%q)", len(c.Nodes), a.statusMsg)
	}
	if strings.Contains(a.statusMsg, "meeting plan") {
		t.Errorf("statusMsg reads like a picker open, want a direct-plant message: %q", a.statusMsg)
	}
}

// TestPlanRendezvousKey_PhaseMismatch_OpensPicker — ADR 0045 S6
// acceptance: "K on a phase-mismatched pair opens the picker rather than
// returning a refusal."
func TestPlanRendezvousKey_PhaseMismatch_OpensPicker(t *testing.T) {
	a := meetingPickerPhaseMismatchApp(t)
	c := a.world.ActiveCraft()

	pressRune(a, 'K')

	if !a.orbitView.MeetingPickerOpen() {
		t.Fatalf("expected the picker to open on a phase-mismatched pair, statusMsg=%q", a.statusMsg)
	}
	if a.active != screenOrbit {
		t.Errorf("active screen = %v, want screenOrbit (the picker is a map chip)", a.active)
	}
	if len(c.Nodes) != 0 {
		t.Errorf("opening the picker must not plant anything, got %d nodes", len(c.Nodes))
	}
	if strings.Contains(a.statusMsg, "rendezvous: ") {
		t.Errorf("statusMsg reads like a bare refusal, want an open-the-picker message: %q", a.statusMsg)
	}
}

// TestPlanRendezvousKey_PlaneMismatch_NamesI_PlantsNothing — ADR 0045 S6
// acceptance: "K on a plane-mismatched pair names [I] and plants
// nothing."
func TestPlanRendezvousKey_PlaneMismatch_NamesI_PlantsNothing(t *testing.T) {
	a := meetingPickerPlaneMismatchApp(t)
	c := a.world.ActiveCraft()

	pressRune(a, 'K')

	if a.orbitView.MeetingPickerOpen() {
		t.Fatalf("picker opened on a plane-mismatched pair — must refuse instead")
	}
	if len(c.Nodes) != 0 {
		t.Fatalf("plane-mismatched K must plant nothing, got %d nodes", len(c.Nodes))
	}
	if !strings.Contains(a.statusMsg, "[I]") {
		t.Errorf("statusMsg = %q, want it to name [I]", a.statusMsg)
	}
}

// TestMeetingPickerLeftRight_WalksPlace and TestMeetingPickerUpDown_WalksRow
// pin the picker's own key contract (ADR 0045 §2 acceptance: "←/→ walks
// the Meeting Place ... ↑/↓ walks the Lap Ladder").
func TestMeetingPickerLeftRight_WalksPlace(t *testing.T) {
	a := meetingPickerPhaseMismatchApp(t)
	pressRune(a, 'K')
	if !a.orbitView.MeetingPickerOpen() {
		t.Fatalf("setup: picker did not open")
	}
	start := a.orbitView.MeetingPickerPlace()

	a.Update(tea.KeyMsg{Type: tea.KeyRight})
	afterRight := a.orbitView.MeetingPickerPlace()
	if afterRight == start {
		t.Errorf("right arrow did not change the Meeting Place from %v", start)
	}

	a.Update(tea.KeyMsg{Type: tea.KeyLeft})
	afterLeft := a.orbitView.MeetingPickerPlace()
	if afterLeft != start {
		t.Errorf("left arrow after right did not return to the start Place: got %v, want %v", afterLeft, start)
	}
}

func TestMeetingPickerUpDown_WalksRow(t *testing.T) {
	a := meetingPickerPhaseMismatchApp(t)
	pressRune(a, 'K')
	if !a.orbitView.MeetingPickerOpen() {
		t.Fatalf("setup: picker did not open")
	}
	startLaps, ok := a.orbitView.MeetingPickerSelectedLaps()
	if !ok {
		t.Fatalf("setup: no row selected after opening")
	}

	a.Update(tea.KeyMsg{Type: tea.KeyDown})
	downLaps, ok := a.orbitView.MeetingPickerSelectedLaps()
	if !ok {
		t.Fatalf("down arrow left no row selected")
	}
	if downLaps == startLaps {
		t.Errorf("down arrow did not change the selected row's laps (still %d)", startLaps)
	}

	a.Update(tea.KeyMsg{Type: tea.KeyUp})
	upLaps, _ := a.orbitView.MeetingPickerSelectedLaps()
	if upLaps != startLaps {
		t.Errorf("up arrow after down did not return to the start row: got %d laps, want %d", upLaps, startLaps)
	}
}

// TestMeetingPickerEnter_PlantsExactlyOneNode — ADR 0045 §2 acceptance:
// "Enter plants the single node."
func TestMeetingPickerEnter_PlantsExactlyOneNode(t *testing.T) {
	a := meetingPickerPhaseMismatchApp(t)
	c := a.world.ActiveCraft()
	pressRune(a, 'K')
	if !a.orbitView.MeetingPickerOpen() {
		t.Fatalf("setup: picker did not open")
	}
	if len(c.Nodes) != 0 {
		t.Fatalf("precondition: %d nodes before Enter, want 0", len(c.Nodes))
	}

	a.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if a.orbitView.MeetingPickerOpen() {
		t.Errorf("picker still open after Enter — should close on plant")
	}
	if len(c.Nodes) != 1 {
		t.Fatalf("expected exactly 1 node planted by Enter, got %d", len(c.Nodes))
	}
}

// TestMeetingPickerEsc_PlantsNothing — ADR 0045 §2 acceptance: "Esc
// closes without planting."
func TestMeetingPickerEsc_PlantsNothing(t *testing.T) {
	a := meetingPickerPhaseMismatchApp(t)
	c := a.world.ActiveCraft()
	pressRune(a, 'K')
	if !a.orbitView.MeetingPickerOpen() {
		t.Fatalf("setup: picker did not open")
	}

	a.Update(tea.KeyMsg{Type: tea.KeyEsc})

	if a.orbitView.MeetingPickerOpen() {
		t.Errorf("picker still open after Esc")
	}
	if len(c.Nodes) != 0 {
		t.Errorf("Esc must plant nothing, got %d nodes", len(c.Nodes))
	}
}

// TestMeetingPickerJoinsCapturingText — trap #1 (#399's own naming): a
// new interactive surface that forgets to join a.capturingText() lets the
// boss key fire mid-edit (ADR 0044's review). Pinned directly against the
// predicate, mirroring TestCapturingTextTrueWhileAltitudeBoxOpen's shape.
func TestMeetingPickerJoinsCapturingText(t *testing.T) {
	a := meetingPickerPhaseMismatchApp(t)
	if a.capturingText() {
		t.Fatalf("capturingText() = true before the picker opens")
	}

	pressRune(a, 'K')
	if !a.orbitView.MeetingPickerOpen() {
		t.Fatalf("setup: picker did not open")
	}
	if !a.capturingText() {
		t.Fatalf("capturingText() = false while the Meeting Planner picker is open")
	}

	a.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if a.capturingText() {
		t.Fatalf("capturingText() = true after the picker closed")
	}
}

// TestMeetingPickerBlocksBossKey is the end-to-end regression
// TestBossKeyInertWhileTypingAltitude mirrors for this surface: a
// backtick while the picker is open must not swap the screen to the boss
// shell.
func TestMeetingPickerBlocksBossKey(t *testing.T) {
	a := meetingPickerPhaseMismatchApp(t)
	pressRune(a, 'K')
	if !a.orbitView.MeetingPickerOpen() {
		t.Fatalf("setup: picker did not open")
	}

	pressRune(a, '`')

	if a.active == screenBoss {
		t.Fatalf("a backtick while the Meeting Planner picker was open opened the boss shell")
	}
	if a.active != screenOrbit {
		t.Errorf("active screen = %v after a backtick with the picker open, want screenOrbit", a.active)
	}
	if !a.orbitView.MeetingPickerOpen() {
		t.Errorf("the picker closed on an unrecognized key (backtick) — it should stay open")
	}
}

// TestMeetingPickerArrowKeysLeaveNoResidualPan is the camera-pan half of
// trap #2 (#399: "arrow keys here must not also pan the camera"). Since
// OrbitView.panOffset is unexported and this test lives in package tui,
// it asserts the same OBSERVABLE CONTRACT app_pan_test.go's own
// TestArrowKeysDispatchToOrbitViewPan does, via a single session: press
// Left exactly 3 times — one full lap of the 3-entry Place cycle (their
// orbit → the crossing → your orbit → their orbit), so the picker's OWN
// displayed Place returns to where it started — and compare the full
// render before and after. If an arrow press while the picker was open
// ALSO reached OrbitView.PanLeft, the camera would have moved by 3 steps
// (Pan has no notion of "a lap" to cancel itself out the way the Place
// cycle does) and the render would show a shifted map even though the
// chip's own text is unchanged.
//
// A window wide enough that the size gate (floor 104×24) never swallows
// the keys this test presses; the picker's own 80×24 render contract is
// pinned separately at the screens package level
// (TestMeetingPickerChip_Render80x24), where the size gate doesn't apply.
func TestMeetingPickerArrowKeysLeaveNoResidualPan(t *testing.T) {
	a := meetingPickerPhaseMismatchApp(t)
	a.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	a.View() // establish the canvas fit (Scale() > 0) — see OrbitView.pan's own guard
	pressRune(a, 'K')
	if !a.orbitView.MeetingPickerOpen() {
		t.Fatalf("setup: picker did not open")
	}
	startPlace := a.orbitView.MeetingPickerPlace()
	before := a.View()

	for i := 0; i < 3; i++ {
		a.Update(tea.KeyMsg{Type: tea.KeyLeft})
	}

	if got := a.orbitView.MeetingPickerPlace(); got != startPlace {
		t.Fatalf("test invariant broken: 3 lefts should complete a full Place lap (got %v, started %v) — cycle length changed?", got, startPlace)
	}
	after := a.View()
	if after != before {
		t.Errorf("view differs after a full lap of Left presses (residual camera pan?)\n--- before ---\n%s\n--- after ---\n%s", before, after)
	}
}
