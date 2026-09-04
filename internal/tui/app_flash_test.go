package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// TestFlashSetsOneLifetime pins the ADR 0048 contract: every Event Flash
// goes through the one flash helper, which sets exactly one lifetime
// (flashLifetime, 3s) regardless of caller. Pre-#427 call sites disagreed
// (1.5s/2s/3s/4s/6s) — this is the regression guard against that drift
// coming back.
func TestFlashSetsOneLifetime(t *testing.T) {
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	before := time.Now()
	a.flash("node 1 burned — 3054 m/s, 2 remaining")
	after := time.Now()

	if a.statusMsg != "node 1 burned — 3054 m/s, 2 remaining" {
		t.Errorf("statusMsg = %q", a.statusMsg)
	}
	wantMin := before.Add(flashLifetime)
	wantMax := after.Add(flashLifetime)
	if a.statusExpires.Before(wantMin) || a.statusExpires.After(wantMax) {
		t.Errorf("statusExpires = %v, want within [%v, %v] (flashLifetime = %v)",
			a.statusExpires, wantMin, wantMax, flashLifetime)
	}
	if flashLifetime != 3*time.Second {
		t.Errorf("flashLifetime = %v, want 3s (test above assumes this)", flashLifetime)
	}
}

// TestFlashOverwritesPriorMessage: a second flash replaces the first in
// full — text and expiry both — so an old Flash can never linger past a
// new one's shorter/longer life. (All lifetimes are equal today, but the
// overwrite behavior itself is the thing under test.)
func TestFlashOverwritesPriorMessage(t *testing.T) {
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	a.flash("first")
	a.flash("second")
	if a.statusMsg != "second" {
		t.Errorf("statusMsg = %q, want %q", a.statusMsg, "second")
	}
}

// TestRefuseIsAFlash: refuse (used by every guarded-key refusal, #282)
// shares flash's voice and lifetime rather than carrying its own copy.
func TestRefuseIsAFlash(t *testing.T) {
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	before := time.Now()
	a.refuse("end flight", "vessel is not wreckage")
	if a.statusMsg != "end flight: vessel is not wreckage" {
		t.Errorf("statusMsg = %q", a.statusMsg)
	}
	if a.statusExpires.Before(before.Add(flashLifetime)) {
		t.Errorf("refuse did not apply flashLifetime: statusExpires = %v", a.statusExpires)
	}
}

// TestStagingFlashNamesDroppedStageAndRemainingCount pins the #427 Event
// Flash text for a stage drop: "dropped <name> — <n> stages left" (the
// issue's own example, "dropped S-IC — 2 stages left"), replacing the
// old "staged: <name> jettisoned" wording that named the newly-spawned
// passive craft with no sense of how much rocket is left.
func TestStagingFlashNamesDroppedStageAndRemainingCount(t *testing.T) {
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	saturn := spacecraft.NewFromLoadout(spacecraft.LoadoutSaturnVID)
	if len(saturn.Stages) < 3 {
		t.Fatalf("Saturn V loadout has %d stages, want >= 3", len(saturn.Stages))
	}
	saturn.Primary = a.world.Crafts[0].Primary
	saturn.State = a.world.Crafts[0].State
	// Crew-tend the bottom stage so canCommand passes without a comms rig
	// (mirrors internal/sim's crewTendActive test helper).
	saturn.Stages[len(saturn.Stages)-1].CommandSource = spacecraft.CommandCrewed
	saturn.SyncFields()
	a.world.Crafts[0] = saturn
	a.world.ActiveCraftIdx = 0

	pressKey(a, ' ')

	const want = "dropped S-IC — 2 stages left"
	if a.statusMsg != want {
		t.Errorf("statusMsg = %q, want %q", a.statusMsg, want)
	}
	if !strings.Contains(a.statusMsg, "S-IC") {
		t.Errorf("flash %q doesn't name the dropped stage", a.statusMsg)
	}
}
