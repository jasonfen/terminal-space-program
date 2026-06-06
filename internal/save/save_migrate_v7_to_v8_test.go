package save

import (
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
)

// systemIdxByNameForTest finds a loaded System's index by name.
func systemIdxByNameForTest(t *testing.T, systems []bodies.System, name string) int {
	t.Helper()
	for i := range systems {
		if systems[i].Name == name {
			return i
		}
	}
	t.Fatalf("system %q not loaded", name)
	return -1
}

// TestMigrateV7PayloadToV8 — ADR 0015. A pre-v8 payload has no per-Vessel
// SystemIdx. The migration derives each craft's binding from which loaded
// System owns its PrimaryID: Sol craft → Sol/0, a craft a buggy interim
// Lumen build left on a Lumen body → the Lumen index, and an unknown
// PrimaryID falls back to Sol/0.
func TestMigrateV7PayloadToV8(t *testing.T) {
	systems, err := bodies.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	solIdx := systemIdxByNameForTest(t, systems, "Sol")
	lumenIdx := systemIdxByNameForTest(t, systems, "Lumen")

	p := &Payload{
		Crafts: []Craft{
			{PrimaryID: "Earth"},    // Sol body
			{PrimaryID: "kern"},     // Lumen planet
			{PrimaryID: "cursor"},   // Lumen moon
			{PrimaryID: "nonesuch"}, // unknown → Sol fallback
		},
	}
	migrateV7PayloadToV8(p, systems)

	want := []int{solIdx, lumenIdx, lumenIdx, solIdx}
	for i, w := range want {
		if got := p.Crafts[i].SystemIdx; got != w {
			t.Errorf("craft[%d] (%q) SystemIdx = %d, want %d", i, p.Crafts[i].PrimaryID, got, w)
		}
	}
}

// TestMigrateV7PayloadToV8SingularCraft — the pre-v5 singular Craft pointer
// must also get its SystemIdx derived, since worldFromPayload promotes it
// into the slate.
func TestMigrateV7PayloadToV8SingularCraft(t *testing.T) {
	systems, err := bodies.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	lumenIdx := systemIdxByNameForTest(t, systems, "Lumen")

	p := &Payload{Craft: &Craft{PrimaryID: "kern"}}
	migrateV7PayloadToV8(p, systems)
	if p.Craft.SystemIdx != lumenIdx {
		t.Errorf("singular Craft SystemIdx = %d, want %d (Lumen)", p.Craft.SystemIdx, lumenIdx)
	}
}
