package spacecraft

import "testing"

// ADR 0056 S2: the VAB palette groups catalog parts by tier, so every part in
// the real catalog needs one of the five tiers.
func TestEveryStageCatalogPartHasATier(t *testing.T) {
	ok := map[string]bool{"booster": true, "sustainer": true, "transfer": true, "payload": true, "tug": true}
	if len(StageCatalog) < 50 {
		t.Fatalf("catalog has %d parts, the real one did not load", len(StageCatalog))
	}
	for id, m := range StageCatalog {
		if !ok[m.Tier] {
			t.Errorf("part %q has tier %q, want one of booster/sustainer/transfer/payload/tug", id, m.Tier)
		}
	}
}

// The fleet line: a part only the stripped-back loadouts reference is a Lumen
// part; a part the full-size loadouts reference is not.
func TestPartFleet(t *testing.T) {
	if got := PartFleet("vector-1"); got != "Lumen" {
		t.Errorf("PartFleet(vector-1) = %q, want Lumen", got)
	}
	if got := PartFleet("s-ic"); got != "" {
		t.Errorf("PartFleet(s-ic) = %q, want empty", got)
	}
	if got := PartFleet("no-such-part"); got != "" {
		t.Errorf("PartFleet(no-such-part) = %q, want empty", got)
	}
}
