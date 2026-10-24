package tui

import (
	"os"
	"strings"
	"testing"
)

// TestControlsDocNamesThrottleTarget (ADR 0058 decision 14 / guard 15.[13]):
// docs/controls.md carries the `c` throttle-target row and the "selected
// throttle" wording on the throttle keys, mirroring F1.
func TestControlsDocNamesThrottleTarget(t *testing.T) {
	doc, err := os.ReadFile("../../docs/controls.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"| `c` | Throttle target", "selected throttle", "never a lit solid"} {
		if !strings.Contains(string(doc), want) {
			t.Errorf("docs/controls.md lacks %q", want)
		}
	}
}
