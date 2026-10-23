package screens

import (
	"strings"
	"testing"
)

// helpRow returns the text of the row in the section whose key cell is key.
func helpRowText(t *testing.T, header, key string) string {
	t.Helper()
	for _, s := range helpSections {
		if s.header != header {
			continue
		}
		for _, r := range s.rows {
			if r[0] == key {
				return r[1]
			}
		}
	}
	return ""
}

// ADR 0058 decision 14 / guard 15.[13]: `c` is a throttle row on MANUAL
// FLIGHT. TestHelpOverlayCoversKeymap alone is vacuous for it, since the
// token "c" already appears on the VAB and planner rows.
func TestHelpManualFlightNamesThrottleTarget(t *testing.T) {
	row := helpRowText(t, "MANUAL FLIGHT", "c")
	for _, want := range []string{"throttle target", "all / core / boosters", "one throttle"} {
		if !strings.Contains(row, want) {
			t.Errorf("MANUAL FLIGHT c row %q lacks %q", row, want)
		}
	}
	if !strings.Contains(helpRowText(t, "MANUAL FLIGHT", "C"), "circularize") {
		t.Error("MANUAL FLIGHT lacks the C circularize sentence (ADR conflict 1)")
	}
	for _, k := range []string{"z / x", "Z / X", "alt+z / alt+x"} {
		if !strings.Contains(helpRowText(t, "MANUAL FLIGHT", k), "selected throttle") {
			t.Errorf("MANUAL FLIGHT %q row does not say it moves the selected throttle", k)
		}
	}
	if !strings.Contains(helpRowText(t, "MANUAL FLIGHT", "b"), "liquid") {
		t.Error("MANUAL FLIGHT b row does not say it cuts every liquid engine")
	}
	if !strings.Contains(helpRowText(t, "PLAN BURNS", "c"), "throttle target") {
		t.Error("PLAN BURNS c row does not point at the throttle target")
	}
}
