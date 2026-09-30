package screens

import (
	"strings"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// R4 #6: the F1 `v` row names the views exactly as the screen does
// (ViewMode.Label(), title-cased since #521), in cycle order.
func TestHelpViewRowUsesScreenNames(t *testing.T) {
	var names []string
	for _, m := range sim.ProjectionViewModes {
		names = append(names, m.Label())
	}
	want := "cycle view (" + strings.Join(names, " / ") + ")"
	for _, s := range helpSections {
		for _, r := range s.rows {
			if r[0] == "v" {
				if !strings.Contains(r[1], want) {
					t.Errorf("help `v` row %q lacks %q", r[1], want)
				}
				return
			}
		}
	}
	t.Fatal("no `v` row in helpSections")
}
