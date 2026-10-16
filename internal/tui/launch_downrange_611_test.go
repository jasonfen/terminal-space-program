package tui

// #611: the launch view shows no downrange for a vessel that never had a
// launch site. Cherry-picked from the red test on triage-587-588 (8d1f00b).

import (
	"regexp"
	"strconv"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// #588 lead 2's `downrange 17635.4 km`: the launch view measures
// downrange from (LaunchLatDeg, LaunchLonDeg), which is (0, 0) for a
// vessel that spawned in orbit. The seed vessel has never launched, yet
// at T+ 00:00:00 the launch view reads `downrange: 8607 km`. A vessel
// that never left a pad has no downrange to report: a dash, no row, or
// zero at T+0 are all fine; thousands of km from the Gulf of Guinea are
// not.
func TestLaunchViewDownrangeForAnOrbitSpawnIsNotFromLatLonZero(t *testing.T) {
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	a.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	if c := a.world.ActiveCraft(); c.Landed || c.OnPad {
		t.Fatal("setup: the seed vessel should be in orbit, not on a pad")
	}
	pressKey(a, 'V')
	if a.world.ViewMode != sim.ViewLaunch {
		t.Fatalf("setup: V did not open the launch view (ViewMode %v)", a.world.ViewMode)
	}
	frame := a.View()
	m := regexp.MustCompile(`downrange: ([0-9.]+) (m|km|Mm)`).FindStringSubmatch(frame)
	if m == nil {
		return // no downrange shown: acceptable
	}
	v, _ := strconv.ParseFloat(m[1], 64)
	switch m[2] {
	case "km":
		v *= 1e3
	case "Mm":
		v *= 1e6
	}
	if v >= 1000 {
		line := ""
		for _, l := range strings.Split(frame, "\n") {
			if strings.Contains(l, "downrange:") {
				line = strings.TrimSpace(l)
			}
		}
		t.Errorf("never-launched orbital vessel shows downrange %s %s at session start: %q", m[1], m[2], line)
	}
}
