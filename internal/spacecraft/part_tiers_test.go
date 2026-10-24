package spacecraft

import "testing"

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
