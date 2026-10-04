package screens

import (
	"strings"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/settings"
)

// Every box's description is read in full at the Design Size and wider:
// at 140x40 the SAVES and DISPLAY descriptions used to end in "…" mid-word
// ("quit-autosave on…", "trailing dash ro…"). Descriptions wrap instead,
// and the value rows under them still answer to clicks.
func TestSettingsDescriptionsAreNeverCutOff(t *testing.T) {
	wantWords := []string{
		"Default visibility of each orbit-screen chip.",
		"Challenge ladder is opt-in.",
		"Off keeps quit-autosave only.",
		"Compact: no trailing dash rows.",
	}
	for _, sz := range [][2]int{{DesignWidth, DesignHeight - 1}, {181, 48}} {
		s := NewSettingsScreen(Theme{})
		out := s.Render(settings.Default(), sz[0], sz[1])
		if strings.Contains(out, "…") {
			t.Errorf("%dx%d: a settings line is cut off with …:\n%s", sz[0], sz[1], out)
		}
		// A wrapped description continues on the next row of its own box,
		// so read the screen box by box: the text between the frame's
		// "│" edges, per column, top to bottom.
		cols := map[int][]string{}
		for _, ln := range strings.Split(out, "\n") {
			for i, seg := range strings.Split(ln, "│") {
				if t := strings.TrimSpace(seg); t != "" {
					cols[i] = append(cols[i], t)
				}
			}
		}
		var boxes []string
		for _, c := range cols {
			boxes = append(boxes, strings.Join(c, " "))
		}
		for _, w := range wantWords {
			found := false
			for _, b := range boxes {
				if strings.Contains(b, w) {
					found = true
				}
			}
			if !found {
				t.Errorf("%dx%d: description %q not shown in full", sz[0], sz[1], w)
			}
		}
		lines := strings.Split(out, "\n")
		for label, want := range map[string]SettingsAction{
			"Autosave interval": SettingsActionCycleAutosave,
		} {
			for i, ln := range lines {
				if strings.Contains(ln, label) {
					if a, _ := s.HandleClick(settingsLabelCol(ln, label), i); a != want {
						t.Errorf("%dx%d: click %q row = %v, want %v", sz[0], sz[1], label, a, want)
					}
				}
			}
		}
	}
}
