package screens

import (
	"strconv"
	"strings"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
)

// ADR 0044 S4: the spawn form's ALTITUDE field is typed (not a ladder), with
// an explicit edit-box state machine, arrow-stepping over sim.OrbitStops,
// and clamp/no-orbit feedback rendered verbatim from sim.ClampToOrbitBand.

// loadRealSystems loads the embedded catalog once — used by the tests that
// need a real no-orbit body (Phobos) rather than a bare fixture.
func loadRealSystems(t *testing.T) []bodies.System {
	t.Helper()
	systems, warnings, err := bodies.LoadAllWithWarnings()
	if err != nil {
		t.Fatalf("LoadAllWithWarnings: %v", err)
	}
	for _, w := range warnings {
		t.Fatalf("unexpected catalog load warning: %v", w)
	}
	return systems
}

func findRealBody(t *testing.T, systems []bodies.System, sysName, id string) bodies.System {
	t.Helper()
	for _, sys := range systems {
		if sys.Name != sysName {
			continue
		}
		for _, b := range sys.Bodies {
			if b.ID == id {
				return sys
			}
		}
	}
	t.Fatalf("body %q not found in system %q", id, sysName)
	return bodies.System{}
}

// enterAltEdit focuses ALTITUDE and opens the edit box, asserting the open
// press never confirms/cancels the form.
func enterAltEdit(t *testing.T, s *SpawnCraft) {
	t.Helper()
	// Disarm any "Enter now launches" left over from a previous commit — a
	// player returns to the field by moving focus, which is what clears it.
	s.HandleKey("tab")
	s.fieldIdx = 3
	if got := s.HandleKey("enter"); got != SpawnActionNone {
		t.Fatalf("opening the altitude box returned %v, want SpawnActionNone", got)
	}
	if !s.altEditing {
		t.Fatalf("HandleKey(enter) on a focused, non-empty-band ALTITUDE field did not open the edit box")
	}
}

// typeDigits sends each rune of km as a separate keystroke, the way a
// player types.
func typeDigits(s *SpawnCraft, km int) {
	for _, d := range strconv.Itoa(km) {
		s.HandleKey(string(d))
	}
}

// TestAltitudeEnterAfterCommitLaunches walks the ADR's three mockup frames
// end to end: the quiet field opens the box on Enter, the box keeps the
// number on Enter, and the very next Enter LAUNCHES rather than reopening.
// That last frame is the whole "Enter now LAUNCHES" line in the mockup, and
// it is what makes the natural gesture Enter-digits-Enter-Enter.
func TestAltitudeEnterAfterCommitLaunches(t *testing.T) {
	s := NewSpawnCraft(Theme{})
	s.Reset(bandTestBodies(), "earth", nil, "", nil)

	enterAltEdit(t, s) // frame 1 → 2
	typeDigits(s, 4400)
	if got := s.HandleKey("enter"); got != SpawnActionNone { // frame 2 → 3
		t.Fatalf("committing the box returned %v, want SpawnActionNone", got)
	}
	if s.altM != 4_400_000 {
		t.Fatalf("altM = %v after commit, want 4400000", s.altM)
	}
	if got := s.HandleKey("enter"); got != SpawnActionConfirm { // frame 3 launches
		t.Fatalf("Enter after leaving the box returned %v, want SpawnActionConfirm", got)
	}
	if s.altEditing {
		t.Error("the launching Enter reopened the edit box")
	}
}

// TestAltitudeArmedLaunchDisarmsOnAnyOtherKey guards the armed state from
// outliving the player's attention: after stepping back out of the box, any
// key that is not Enter must return the field to "Enter to edit", so a
// player who changes their mind cannot launch on an Enter they had queued
// up for the box.
func TestAltitudeArmedLaunchDisarmsOnAnyOtherKey(t *testing.T) {
	for _, key := range []string{"tab", "shift+tab", "left", "right"} {
		t.Run(key, func(t *testing.T) {
			s := NewSpawnCraft(Theme{})
			s.Reset(bandTestBodies(), "earth", nil, "", nil)
			enterAltEdit(t, s)
			typeDigits(s, 900)
			s.HandleKey("enter") // leave the box — armed
			s.HandleKey(key)     // ...and change our mind
			s.fieldIdx = 3       // come back to ALTITUDE however we got away
			if got := s.HandleKey("enter"); got != SpawnActionNone {
				t.Fatalf("after %q, Enter returned %v, want the box to reopen (SpawnActionNone)", key, got)
			}
			if !s.altEditing {
				t.Errorf("after %q, Enter did not reopen the edit box", key)
			}
		})
	}
}

// TestAltitudeArmedHintTellsThePlayerEnterLaunches pins the mockup's own
// hint text: the frame that launches must say so, or the state is invisible.
func TestAltitudeArmedHintTellsThePlayerEnterLaunches(t *testing.T) {
	s := NewSpawnCraft(Theme{})
	s.Reset(bandTestBodies(), "earth", nil, "", nil)
	s.fieldIdx = 3
	if out := s.Render(80); !strings.Contains(out, "Enter to edit") {
		t.Fatalf("quiet focused ALTITUDE does not offer %q:\n%s", "Enter to edit", out)
	}
	enterAltEdit(t, s)
	typeDigits(s, 900)
	s.HandleKey("enter")
	out := s.Render(80)
	if !strings.Contains(out, "Enter now launches") {
		t.Errorf("after leaving the box the hint does not say Enter launches:\n%s", out)
	}
	if strings.Contains(out, "Enter to edit") {
		t.Errorf("after leaving the box the hint still offers to edit:\n%s", out)
	}
}

// TestAltitudeNeverLaunchesHalfTyped is the first ADR-mandated invariant:
// with the edit box open, mid-type, no key (including "enter" being routed
// elsewhere or a stray key) can produce SpawnActionConfirm.
func TestAltitudeNeverLaunchesHalfTyped(t *testing.T) {
	s := NewSpawnCraft(Theme{})
	s.Reset(bandTestBodies(), "earth", nil, "", nil)
	enterAltEdit(t, s)

	for _, k := range []string{"4", "4", "0", "0"} {
		if got := s.HandleKey(k); got != SpawnActionNone {
			t.Fatalf("digit key %q returned %v while editing, want SpawnActionNone", k, got)
		}
	}
	if s.altInput != "4400" {
		t.Fatalf("altInput = %q, want \"4400\"", s.altInput)
	}
	// The committed altitude must NOT have moved yet — only Enter commits.
	if s.altM != 500_000 {
		t.Errorf("altM changed to %v before commit, want unchanged 500000 (500km default)", s.altM)
	}
}

// TestAltitudeEscRevertsNotCancelsForm is the second ADR-mandated invariant:
// Esc while editing discards the typed buffer and reopens nothing — it must
// never cancel the whole form (SpawnActionCancel).
func TestAltitudeEscRevertsNotCancelsForm(t *testing.T) {
	s := NewSpawnCraft(Theme{})
	s.Reset(bandTestBodies(), "earth", nil, "", nil)
	enterAltEdit(t, s)
	typeDigits(s, 9999)

	if got := s.HandleKey("esc"); got != SpawnActionNone {
		t.Fatalf("Esc while editing returned %v, want SpawnActionNone (must not cancel the form)", got)
	}
	if s.altEditing {
		t.Error("Esc did not close the edit box")
	}
	if s.altInput != "" {
		t.Errorf("Esc left a stale input buffer %q", s.altInput)
	}
	if s.altM != 500_000 {
		t.Errorf("Esc changed the committed altitude to %v, want unchanged 500000", s.altM)
	}

	// Plain Esc, NOT editing, still cancels the whole form as normal.
	if got := s.HandleKey("esc"); got != SpawnActionCancel {
		t.Errorf("Esc outside the edit box = %v, want SpawnActionCancel", got)
	}
}

// TestAltitudeCommitClamps — a typed value below the floor is raised on
// commit, and the note is sim.ClampToOrbitBand's sentence verbatim.
func TestAltitudeCommitClamps(t *testing.T) {
	s := NewSpawnCraft(Theme{})
	systems := loadRealSystems(t)
	sys := findRealBody(t, systems, "Sol", "earth")
	s.Reset(sys.Bodies, "earth", nil, "", nil)

	enterAltEdit(t, s)
	typeDigits(s, 60) // 60km — below Earth's 175km floor
	if got := s.HandleKey("enter"); got != SpawnActionNone {
		t.Fatalf("committing returned %v, want SpawnActionNone", got)
	}
	if s.altEditing {
		t.Error("commit did not close the edit box")
	}
	if s.altM != 175_000 {
		t.Errorf("altM = %v after commit, want 175000 (clamped to Earth's floor)", s.altM)
	}
	if !strings.Contains(s.altNote, "raised from 60") {
		t.Errorf("altNote = %q, missing the raised-from-60 clamp sentence", s.altNote)
	}
	out := s.Render(80)
	if !strings.Contains(out, "↳") || !strings.Contains(out, s.altNote) {
		t.Errorf("rendered form does not show the clamp note verbatim:\n%s", out)
	}
}

// TestAltitudeEmptyCommitReverts — committing an empty buffer reverts to the
// prior value rather than clearing/erroring (there is no "no altitude"
// state), mirroring vab.go's setTarget treating empty as its own accepted
// case.
func TestAltitudeEmptyCommitReverts(t *testing.T) {
	s := NewSpawnCraft(Theme{})
	s.Reset(bandTestBodies(), "earth", nil, "", nil)
	enterAltEdit(t, s)
	typeDigits(s, 4400)
	s.HandleKey("backspace")
	s.HandleKey("backspace")
	s.HandleKey("backspace")
	s.HandleKey("backspace")
	if s.altInput != "" {
		t.Fatalf("setup: altInput = %q, want empty after 4 backspaces", s.altInput)
	}
	if got := s.HandleKey("enter"); got != SpawnActionNone {
		t.Fatalf("committing empty returned %v, want SpawnActionNone", got)
	}
	if s.altM != 500_000 {
		t.Errorf("empty commit changed altM to %v, want unchanged 500000 (prior value)", s.altM)
	}
	if s.altEditing {
		t.Error("empty commit did not close the box")
	}
}

// TestAltitudeOnlyDigitsAndBackspaceEditBuffer — letters, arrows, tab are
// ignored while editing; they neither mutate the buffer nor escape the box.
func TestAltitudeOnlyDigitsAndBackspaceEditBuffer(t *testing.T) {
	s := NewSpawnCraft(Theme{})
	s.Reset(bandTestBodies(), "earth", nil, "", nil)
	enterAltEdit(t, s)
	typeDigits(s, 42)

	for _, k := range []string{"a", "x", "left", "right", "tab", "shift+tab", "."} {
		s.HandleKey(k)
	}
	if s.altInput != "42" {
		t.Errorf("altInput = %q after non-digit keys, want unchanged \"42\"", s.altInput)
	}
	if !s.altEditing {
		t.Error("a non-digit/non-enter/non-esc key closed the edit box")
	}
	if s.fieldIdx != 3 {
		t.Error("tab moved focus while the edit box was open")
	}
}

// TestAltitudeEnterFromOtherFieldsStillLaunches — Enter's field-3 takeover
// (open the box / stay parked) is local to ALTITUDE; from every other field
// Enter still confirms the form as before.
func TestAltitudeEnterFromOtherFieldsStillLaunches(t *testing.T) {
	s := NewSpawnCraft(Theme{})
	s.Reset(bandTestBodies(), "earth", nil, "", nil)
	for _, idx := range []int{0, 1, 2, 4} {
		s.fieldIdx = idx
		if got := s.HandleKey("enter"); got != SpawnActionConfirm {
			t.Errorf("fieldIdx=%d: Enter = %v, want SpawnActionConfirm", idx, got)
		}
	}
}

// TestAltitudeArrowsStepOrbitStops — outside the box, arrows walk
// sim.OrbitStops rather than any hardcoded ladder, moving to the next real
// stop even when the current value sits between two stops.
func TestAltitudeArrowsStepOrbitStops(t *testing.T) {
	s := NewSpawnCraft(Theme{})
	systems := loadRealSystems(t)
	sys := findRealBody(t, systems, "Sol", "earth")
	s.Reset(sys.Bodies, "earth", nil, "", nil)
	s.fieldIdx = 3

	// Land squarely on a value between two stops (the default 500km is
	// already there for Earth in practice, but force it to be sure).
	enterAltEdit(t, s)
	typeDigits(s, 4400)
	s.HandleKey("enter")
	before := s.altM

	s.HandleKey("right")
	if s.altM <= before {
		t.Fatalf("right arrow did not move to a HIGHER stop: before=%v after=%v", before, s.altM)
	}
	afterUp := s.altM

	s.HandleKey("left")
	s.HandleKey("left")
	if s.altM >= afterUp {
		t.Fatalf("left arrow did not move to a LOWER stop: afterUp=%v after=%v", afterUp, s.altM)
	}
}

// TestAltitudeArrowsClampAtEnds — stepping past either end of the Orbit
// Stops holds at the end rather than wrapping (S4 design choice — see the
// implementation report).
func TestAltitudeArrowsClampAtEnds(t *testing.T) {
	s := NewSpawnCraft(Theme{})
	systems := loadRealSystems(t)
	sys := findRealBody(t, systems, "Sol", "earth")
	s.Reset(sys.Bodies, "earth", nil, "", nil)
	s.fieldIdx = 3

	for i := 0; i < 20; i++ {
		s.HandleKey("left")
	}
	floor := s.altM
	if s.HandleKey("left"); s.altM != floor {
		t.Errorf("left arrow past the floor moved altM to %v, want holding at floor %v", s.altM, floor)
	}

	for i := 0; i < 20; i++ {
		s.HandleKey("right")
	}
	ceiling := s.altM
	if s.HandleKey("right"); s.altM != ceiling {
		t.Errorf("right arrow past the ceiling moved altM to %v, want holding at ceiling %v", s.altM, ceiling)
	}
}

// TestAltitudeFollowsParentAcrossChangeWhenLegal — a typed value survives a
// PARENT BODY change untouched when the new body can hold it.
func TestAltitudeFollowsParentAcrossChangeWhenLegal(t *testing.T) {
	s := NewSpawnCraft(Theme{})
	systems := loadRealSystems(t)
	sys := findRealBody(t, systems, "Sol", "earth")
	// Also needs Mars in the parent list to cursor onto.
	var marsIdx int = -1
	for i, b := range sys.Bodies {
		if b.ID == "mars" {
			marsIdx = i
		}
	}
	if marsIdx < 0 {
		t.Fatal("setup: mars not found in Sol system")
	}
	s.Reset(sys.Bodies, "earth", nil, "", nil)
	enterAltEdit(t, s)
	typeDigits(s, 300) // 300km — legal at Earth (floor 175) and at Mars (floor 125)
	s.HandleKey("enter")
	if s.altM != 300_000 {
		t.Fatalf("setup: altM = %v, want 300000", s.altM)
	}

	s.fieldIdx = 2
	for s.SelectedParentID() != "mars" {
		s.HandleKey("right")
	}
	if s.altM != 300_000 {
		t.Errorf("altM changed to %v across a parent change where 300km stays legal", s.altM)
	}
	if s.altNote != "" {
		t.Errorf("altNote = %q, want empty (no clamp happened)", s.altNote)
	}
}

// TestAltitudeReclampsOnParentChangeWhenIllegal — cursoring to a body that
// can't hold the current altitude re-clamps it and reports why (ADR 0044 §4
// "type 300 at Earth, cursor to a small moon, land on that moon's ceiling").
func TestAltitudeReclampsOnParentChangeWhenIllegal(t *testing.T) {
	s := NewSpawnCraft(Theme{})
	systems := loadRealSystems(t)
	sys := findRealBody(t, systems, "Sol", "earth")
	s.Reset(sys.Bodies, "earth", nil, "", nil)
	enterAltEdit(t, s)
	typeDigits(s, 90000) // 90,000km — way above the Moon's ceiling
	s.HandleKey("enter")

	s.fieldIdx = 2
	for s.SelectedParentID() != "moon" {
		s.HandleKey("right")
	}
	if s.altM >= 90_000_000 {
		t.Fatalf("altM = %v, expected a clamp down at the Moon", s.altM)
	}
	if s.altNote == "" {
		t.Error("altNote empty after a cross-parent clamp — the move must be reported")
	}
	if !strings.Contains(s.altNote, "lowered from 90,000") && !strings.Contains(s.altNote, "lowered from 90000") {
		t.Errorf("altNote = %q, missing the lowered-from-90000 wording", s.altNote)
	}
}

// TestAltitudeNoOrbitBodyKeepsEnterDead — a body with an Empty Orbit Band
// (Phobos) stays selectable as PARENT BODY, but Enter never confirms the
// form while it's selected and POSITION is orbit — the sim would refuse the
// spawn anyway.
func TestAltitudeNoOrbitBodyKeepsEnterDead(t *testing.T) {
	s := NewSpawnCraft(Theme{})
	systems := loadRealSystems(t)
	sys := findRealBody(t, systems, "Sol", "phobos")
	s.Reset(sys.Bodies, "phobos", nil, "", nil)

	if !s.altBandEmpty {
		t.Fatal("setup: Phobos should report an Empty Orbit Band")
	}
	if got := s.SelectedParentID(); got != "phobos" {
		t.Fatalf("setup: parent = %q, want phobos", got)
	}

	for _, idx := range []int{0, 1, 2, 3, 4} {
		s.fieldIdx = idx
		if got := s.HandleKey("enter"); got == SpawnActionConfirm {
			t.Errorf("fieldIdx=%d: Enter confirmed the form over an Empty Orbit Band", idx)
		}
	}
	if s.altEditing {
		t.Error("Enter opened the edit box over an Empty Orbit Band — there is nothing to edit")
	}

	out := s.Render(80)
	if !strings.Contains(out, "✕") {
		t.Errorf("rendered form missing the ✕ no-orbit marker:\n%s", out)
	}
	if !strings.Contains(out, "Mars owns everything outside Phobos's surface") {
		t.Errorf("rendered form does not show sim.ClampToOrbitBand's no-orbit sentence verbatim:\n%s", out)
	}
	// PARENT BODY still shows Phobos — it must not be delisted.
	if !strings.Contains(out, "Phobos") {
		t.Error("Phobos must remain selectable in PARENT BODY even with no legal orbit")
	}
}

// TestSelectedAltitudeMSignatureUnchanged — app.go:753 calls this with no
// arguments; a signature change would be a silent break there.
func TestSelectedAltitudeMSignatureUnchanged(t *testing.T) {
	s := NewSpawnCraft(Theme{})
	s.Reset(bandTestBodies(), "earth", nil, "", nil)
	var _ func() float64 = s.SelectedAltitudeM
	if s.SelectedAltitudeM() != 500_000 {
		t.Errorf("default SelectedAltitudeM = %v, want 500000 (500km)", s.SelectedAltitudeM())
	}
}

// TestAltitudeCommitSamplesCommsOnceNotPerKeystroke — #221/ADR0044: sampling
// costs ~400 connectivity solves, so it must run on commit, never per
// keystroke while typing.
func TestAltitudeCommitSamplesCommsOnceNotPerKeystroke(t *testing.T) {
	s := NewSpawnCraft(Theme{})
	calls := 0
	s.Reset(bandTestBodies(), "earth", nil, "", func(bodyID string, altM, antennaRangeM float64) (float64, bool) {
		calls++
		return 1.0, true
	})

	enterAltEdit(t, s)
	typeDigits(s, 4400)
	// Render repeatedly while editing — must never sample.
	for i := 0; i < 5; i++ {
		_ = s.Render(80)
	}
	if calls != 0 {
		t.Fatalf("sampler called %d times while the edit box was open (mid-type), want 0", calls)
	}

	s.HandleKey("enter") // commit
	_ = s.Render(80)
	_ = s.Render(80)
	_ = s.Render(80)
	if calls != 1 {
		t.Errorf("sampler called %d times across commit + repeated renders at the same altitude, want exactly 1 (memoized)", calls)
	}
}

// TestAltKmLabelUsesCommaGrouping — review finding #7: the ALTITUDE value
// line and the clamp notes beneath it must use the SAME number format.
// sim.ClampToOrbitBand's notes comma-group ("32,097,122 km" via
// sim.CommaKm); altKmLabel must match rather than showing the bare
// "32097122 km" a %.0f format produces.
func TestAltKmLabelUsesCommaGrouping(t *testing.T) {
	got := altKmLabel(32_097_122_000) // Jupiter's Orbit Ceiling, in metres
	want := "32,097,122 km"
	if got != want {
		t.Errorf("altKmLabel(32097122000) = %q, want %q", got, want)
	}
	// The common case must stay unchanged (no comma below 1000).
	if got := altKmLabel(500_000); got != "500 km" {
		t.Errorf("altKmLabel(500000) = %q, want %q", got, "500 km")
	}
}

// TestAltitudeInputBufferCapped — review finding #6. A held digit key must
// not grow altInput without bound (the "[%s_] km" line would blow past the
// modal's width at 80 columns, and strconv.Atoi's overflow was silently
// swallowed by commitAltInput). Past maxAltInputDigits, further digits are
// ignored exactly like a non-digit key already is.
func TestAltitudeInputBufferCapped(t *testing.T) {
	s := NewSpawnCraft(Theme{})
	s.Reset(bandTestBodies(), "earth", nil, "", nil)
	enterAltEdit(t, s)

	for i := 0; i < maxAltInputDigits+5; i++ {
		s.HandleKey("9")
	}
	if len(s.altInput) != maxAltInputDigits {
		t.Fatalf("altInput = %q (len %d) after %d digit presses, want capped at %d digits",
			s.altInput, len(s.altInput), maxAltInputDigits+5, maxAltInputDigits)
	}

	// The capped buffer must still parse and commit cleanly — no silent
	// strconv.Atoi overflow swallowed on Enter.
	s.HandleKey("enter")
	wantKm, _ := strconv.Atoi(strings.Repeat("9", maxAltInputDigits))
	if s.altM == 0 {
		t.Fatalf("altM = 0 after committing a capped all-9s buffer, want a real (clamped) altitude")
	}
	_ = wantKm // the exact clamped value depends on Earth's ceiling; altM != 0 is the load-bearing check
}

// TestAltitudeRetypedDisplayedValueCountsAsOnStop — review finding #9. At
// Lumen's Mote the synchronous Orbit Stop sits at 42.1387km but the whole-km
// display rounds it to "42 km"; a player who reads that and retypes 42
// lands at exactly 42.000km, 139m short of the real stop. Before the
// epsilon fix, `→` from there crept forward to the real 42.1387km value —
// still displayed as "42 km", so the keypress looked like a no-op. After
// the fix the retyped value counts as being ON the stop, so `→` moves to
// the NEXT stop out (Mote's stops are 25 / 42.139(sync) / 50 / 75.416km).
func TestAltitudeRetypedDisplayedValueCountsAsOnStop(t *testing.T) {
	s := NewSpawnCraft(Theme{})
	systems := loadRealSystems(t)
	sys := findRealBody(t, systems, "Lumen", "mote")
	s.Reset(sys.Bodies, "mote", nil, "", nil)

	enterAltEdit(t, s)
	typeDigits(s, 42) // the displayed rounding of the 42.1387km sync stop
	s.HandleKey("enter")
	if s.altM != 42_000 {
		t.Fatalf("setup: altM = %v, want exactly 42000 (retyped, not the raw sync altitude)", s.altM)
	}

	s.fieldIdx = 3
	s.HandleKey("right")
	if s.altM < 49_000 {
		t.Errorf("altM after -> from a retyped displayed-stop value = %v, want a real move to the next stop (~50000), not an invisible creep to the raw 42139 sync altitude", s.altM)
	}
}

// TestAltitudeFieldRendersAt80Columns is a render smoke test at the repo's
// mandated real terminal width (not a wide terminal) for the quiet state.
func TestAltitudeFieldRendersAt80Columns(t *testing.T) {
	s := NewSpawnCraft(Theme{})
	s.Reset(bandTestBodies(), "earth", nil, "", nil)
	out := s.Render(80)
	if !strings.Contains(out, "ALTITUDE") {
		t.Error("ALTITUDE header missing from an 80-column render")
	}
	if !strings.Contains(out, "500 km") {
		t.Errorf("default 500km value not rendered at 80 columns:\n%s", out)
	}
}
