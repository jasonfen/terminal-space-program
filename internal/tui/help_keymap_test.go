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

// TestControlsMdHasRecordingWindowRow (ADR 0062 decision 9): docs/controls.md
// carries the Settings Recording window row. New function: no existing
// test pins Settings rows of controls.md by name.
func TestControlsMdHasRecordingWindowRow(t *testing.T) {
	doc, err := os.ReadFile("../../docs/controls.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"| Settings → **Recording window** | How much of a vessel's Flight Recording is kept: sim-days, default 7; older rows are dropped from the file as it is written |"} {
		if !strings.Contains(string(doc), want) {
			t.Errorf("docs/controls.md lacks %q", want)
		}
	}
}

// ADR 0063 §1: docs/controls.md carries the `A` Inertial row and the
// Port-hold fallback sentence. New function: each controls.md row here is
// pinned by its own test.
func TestControlsDocNamesInertialHold(t *testing.T) {
	doc, err := os.ReadFile("../../docs/controls.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"| `A` | Hold the nose where it points now (`hold: Inertial (ORBIT)`)", "the arrows then turn the held nose instead of trimming", "a Port hold goes to Inertial with it"} {
		if !strings.Contains(string(doc), want) {
			t.Errorf("docs/controls.md lacks %q", want)
		}
	}
}
