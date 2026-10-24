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

// TestControlsDocNamesBatchRow (ADR 0059 decision 9): docs/controls.md mirrors
// F1's VESSEL `batch` row word for word and says what `t` does with a batch.
func TestControlsDocNamesBatchRow(t *testing.T) {
	doc, err := os.ReadFile("../../docs/controls.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"| `batch` | U releases a stack of payloads at once; [t] steps over it as one stop. About 30 vessels per player keeps the map quick |",
		"a released batch is one stop (its nearest member); `j` then `enter` picks a particular one",
	} {
		if !strings.Contains(string(doc), want) {
			t.Errorf("docs/controls.md lacks %q", want)
		}
	}
}
