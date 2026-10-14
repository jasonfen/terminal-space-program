package missions

import (
	"strings"
	"testing"
)

// ADR 0053 "Settled here" item 5: the dock rungs no longer describe a proximity
// click. chal-dock's second objective and tut-dock's last name the port (and
// chal-dock the PORT frame key), and neither still promises a bare "and dock".
func TestDockRungTextNamesThePort(t *testing.T) {
	cat, err := LoadAll()
	if err != nil {
		t.Fatal(err)
	}
	last := func(id string) string {
		for _, m := range cat.Missions {
			if m.ID == id {
				return m.Objectives[len(m.Objectives)-1].Description
			}
		}
		t.Fatalf("mission %q missing", id)
		return ""
	}
	chal := last("chal-dock")
	for _, want := range []string{"port", "[;]", "half a metre", "0.1 m/s"} {
		if !strings.Contains(chal, want) {
			t.Errorf("chal-dock objective %q does not mention %q", chal, want)
		}
	}
	tut := last("tut-dock")
	for _, want := range []string{"port", "half a metre", "0.1 m/s"} {
		if !strings.Contains(tut, want) {
			t.Errorf("tut-dock objective %q does not mention %q", tut, want)
		}
	}
}
