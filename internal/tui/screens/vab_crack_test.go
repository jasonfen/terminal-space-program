package screens

import (
	"strings"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// S2 / ADR 0032 §6 — crack-open: enter on an atomic catalog stage header
// converts it in place to its authored seed components, flags riding along,
// with an honest Δv-delta flash. These use the real catalog (seeds reference
// real component IDs).

// TestVABCrackOpenRoundTrip — cracking a seeded catalog stage replaces it with
// its seed components and flashes the delta.
func TestVABCrackOpenRoundTrip(t *testing.T) {
	v := NewVAB(Theme{})
	v.Reset(spacecraft.Components)
	v.stages = []vabStage{{catalogPartID: "s-ivb"}}
	v.stackCursor = 0 // the (only) stage header
	v.crackOpen()

	if v.stages[0].isCatalog() {
		t.Fatalf("stage still atomic after crack: %+v", v.stages[0])
	}
	seed := spacecraft.StageCatalog["s-ivb"].VabSeed
	if strings.Join(v.stages[0].components, ",") != strings.Join(seed, ",") {
		t.Errorf("cracked components = %v, want the seed %v", v.stages[0].components, seed)
	}
	if !strings.Contains(v.flash, "cracked") || !strings.Contains(v.flash, "Δv") {
		t.Errorf("flash = %q, want a cracked Δv-delta message", v.flash)
	}
	// The cracked stage must resolve to a real, non-zero stage.
	if v.Stats().TotalDV <= 0 {
		t.Error("cracked stack has no Δv — seed did not resolve")
	}
}

// TestVABCrackFlagsRideAlong — the seam / decouple flags survive the crack
// (ADR 0032 §6): staging intent set on the atomic part is not lost.
func TestVABCrackFlagsRideAlong(t *testing.T) {
	v := NewVAB(Theme{})
	v.Reset(spacecraft.Components)
	v.stages = []vabStage{
		{components: []string{"merlin-sustainer", "kero-tank-4k"}},
		{catalogPartID: "s-ivb", dockSeamBelow: true, decoupleFused: true},
	}
	v.stackCursor = v.headerRowIndex(1) // the s-ivb header (top stage)
	v.crackOpen()
	if v.stages[1].isCatalog() {
		t.Fatal("top stage not cracked")
	}
	if !v.stages[1].dockSeamBelow || !v.stages[1].decoupleFused {
		t.Errorf("flags lost on crack: seam=%v fused=%v, want both true",
			v.stages[1].dockSeamBelow, v.stages[1].decoupleFused)
	}
}

// TestVABCrackNoSeed — a catalog part with no authored seed flashes and stays
// atomic (ADR 0032 §6).
func TestVABCrackNoSeed(t *testing.T) {
	v := NewVAB(Theme{})
	v.Reset(spacecraft.Components)
	v.stages = []vabStage{{catalogPartID: "service-module"}} // in catalog, no vab_seed
	v.stackCursor = 0
	v.crackOpen()
	if !v.stages[0].isCatalog() {
		t.Error("seedless part was cracked, want it left atomic")
	}
	if !strings.Contains(v.flash, "no decomposition") {
		t.Errorf("flash = %q, want a no-decomposition notice", v.flash)
	}
}

// TestVABCrackNonHeaderNoOp — enter on a composed stage (nothing to crack) and
// on a component row is a no-op (ADR 0032 §6).
func TestVABCrackNonHeaderNoOp(t *testing.T) {
	v := NewVAB(Theme{})
	v.Reset(spacecraft.Components)
	v.stages = []vabStage{{components: []string{"merlin-sustainer", "kero-tank-4k"}}}
	before := strings.Join(v.stages[0].components, ",")

	v.stackCursor = v.headerRowIndex(0) // composed stage header — no catalog block
	v.crackOpen()
	if strings.Join(v.stages[0].components, ",") != before {
		t.Errorf("crack mutated a composed stage: %v", v.stages[0].components)
	}

	v.stackCursor = v.headerRowIndex(0) + 1 // a component row
	v.crackOpen()
	if strings.Join(v.stages[0].components, ",") != before {
		t.Errorf("crack on a component row mutated the stage: %v", v.stages[0].components)
	}
}

// TestVABCrackViaEnterKey — the enter key routes to crackOpen through the
// build keymap (ADR 0032 §6).
func TestVABCrackViaEnterKey(t *testing.T) {
	v := NewVAB(Theme{})
	v.Reset(spacecraft.Components)
	v.stages = []vabStage{{catalogPartID: "icps"}}
	v.focus = focusStack
	v.stackCursor = 0
	if act := v.HandleKey("enter"); act != VABActionNone {
		t.Fatalf("enter returned action %v, want none", act)
	}
	if v.stages[0].isCatalog() {
		t.Error("enter did not crack the catalog stage")
	}
}
