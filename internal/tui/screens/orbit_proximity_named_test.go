package screens

import (
	"math"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// ADR 0055 decision 2: align: and off-axis: are gone, misalign: and lateral:
// carry the same numbers, and hold: sits after closing: and before trim:.
// Old labels (the sabotage) fail the absent checks; no hold: row fails the
// order check.
func TestProximityChipNamedRowsAndOrder(t *testing.T) {
	w := portedPairWorld(t, []string{"rcs-tug"}, []string{"nudge-tug"}, orbital.Vec3{X: 1.1, Y: 0.6, Z: 0.2}, 0)
	v := NewOrbitView(chipTestTheme())
	v.Resize(DesignWidth, DesignHeight)
	chip := v.buildProximityChip(w)
	for _, old := range []string{"align:", "off-axis:"} {
		if chipLine(chip, old) != "" {
			t.Errorf("old label %q still on the chip", old)
		}
	}
	idx := func(label string) int {
		for i, l := range chip {
			if strings.HasPrefix(ansi.Strip(l), "  "+label) {
				return i
			}
		}
		return -1
	}
	closing, hold, trim := idx("closing:"), idx("hold:"), idx("trim:")
	if closing < 0 || hold != closing+1 || trim != hold+1 {
		t.Errorf("order closing=%d hold=%d trim=%d, want hold right after closing and right before trim", closing, hold, trim)
	}
	if got := chipLine(chip, "hold:"); !strings.HasSuffix(got, "Prograde (ORBIT)") {
		t.Errorf("default hold row = %q, want Prograde (ORBIT)", got)
	}
	// Numbers unchanged by the rename.
	if got := chipLine(chip, "lateral:"); !strings.Contains(got, "0.6 m ↓") || !strings.Contains(got, "0.2 m ◂") {
		t.Errorf("lateral row = %q", got)
	}
}

// hold: reads the same label GUIDANCE's hold: row builds, for every mode,
// and the value starts at the shared value column.
func TestProximityHoldRowReadsTheGuidanceLabel(t *testing.T) {
	w := portedPairWorld(t, []string{"rcs-tug"}, []string{"nudge-tug"}, orbital.Vec3{X: 5}, 0)
	v := NewOrbitView(chipTestTheme())
	v.Resize(DesignWidth, DesignHeight)
	c := w.ActiveCraft()
	for m := spacecraft.BurnMode(0); m < 40; m++ {
		if m.String() == "" {
			continue
		}
		c.AttitudeMode = m
		want := attitudeHoldLabel(w, m)
		row := chipLine(v.buildProximityChip(w), "hold:")
		if row != "  hold:"+strings.Repeat(" ", chipValueCol-2-len("hold:"))+want {
			t.Errorf("mode %v: hold row %q, want value %q at col %d", m, row, want, chipValueCol)
		}
	}
	c.AttitudeMode = spacecraft.BurnPortIn
	if got := chipLine(v.buildProximityChip(w), "hold:"); !strings.HasSuffix(got, "Port+ (PORT)") {
		t.Errorf("Port+ hold row = %q", got)
	}
}

// The chip grows by one row (hold:) and still fits 41x(14 boxed) at 140x40
// with the tallest content, and no row, hold included, outgrows the inner
// width for any hold the vessel can carry.
func TestProximityChipHeightAndWidthWithHold(t *testing.T) {
	w := portedPairWorld(t, []string{"rcs-tug"}, []string{"heartbeat-keeper"}, orbital.Vec3{X: 3.2, Y: 0.6, Z: 0.2}, 20)
	w.Crafts[0].Name = "A Very Long Vessel Name-12"
	w.Crafts[1].Name = "Another Very Long Vessel Name-7"
	v := NewOrbitView(chipTestTheme())
	v.Resize(DesignWidth, DesignHeight)
	c := w.ActiveCraft()
	c.PitchTrim = 0.1
	for m := spacecraft.BurnMode(0); m < 40; m++ {
		if m.String() == "" {
			continue
		}
		c.AttitudeMode = m
		chip := v.buildProximityChip(w)
		if len(chip) != 12 {
			t.Fatalf("mode %v: chip has %d lines, want 12 (11 + hold:)", m, len(chip))
		}
		for _, l := range chip {
			if got := lipgloss.Width(l); got > proximityChipInnerWidth {
				t.Errorf("mode %v: row %d cells > %d: %q", m, got, proximityChipInnerWidth, ansi.Strip(l))
			}
		}
	}
	// Placed: 14 rows boxed, 41 wide, measured off the render.
	c.AttitudeMode = spacecraft.BurnSurfaceRetrograde
	v.Render(w, 0, DesignWidth, DesignHeight)
	found := false
	for _, r := range v.chipRects {
		if r.id != "" {
			continue
		}
		found = true
		if h := r.rowEnd - r.rowStart + 1; h != 14 {
			t.Errorf("placed PROXIMITY chip is %d rows, want 14", h)
		}
		if wd := r.colEnd - r.colStart + 1; wd != 41 {
			t.Errorf("placed PROXIMITY chip is %d cells wide, want 41", wd)
		}
	}
	if !found {
		t.Error("no PROXIMITY chip rect")
	}
}

// Compact Form: hold: survives only in PORT with a hold other than Port+.
func TestProximityCompactHoldRule(t *testing.T) {
	w := portedPairWorld(t, []string{"rcs-tug"}, []string{"nudge-tug"}, orbital.Vec3{X: 120}, 0)
	v := newProximityTestView(t, 80, 24)
	c := w.ActiveCraft()
	cases := []struct {
		nav  sim.NavMode
		mode spacecraft.BurnMode
		want bool
	}{
		{sim.NavOrbit, spacecraft.BurnPrograde, false},
		{sim.NavOrbit, spacecraft.BurnPortIn, false},
		{sim.NavPort, spacecraft.BurnPortIn, false},
		{sim.NavPort, spacecraft.BurnPrograde, true},
		{sim.NavPort, spacecraft.BurnPortOut, true},
	}
	for _, k := range cases {
		w.NavMode, c.AttitudeMode = k.nav, k.mode
		got := chipLine(v.buildProximityChipCompact(w), "hold:") != ""
		if got != k.want {
			t.Errorf("nav %v hold %v: compact hold row = %v, want %v", k.nav, k.mode, got, k.want)
		}
	}
}

// ADR 0055 decision 3: the navball row in PORT reads pitch / yaw to the
// port axis; elsewhere it is the surface pitch / hdg as before.
func TestNavballReadoutInPortIsPitchYawToThePortAxis(t *testing.T) {
	w := portedPairWorld(t, []string{"rcs-tug"}, []string{"nudge-tug"}, orbital.Vec3{X: 20}, 0)
	st, _ := w.ProximityState()
	up, right, _ := sim.PortFaceBasis(st.Target.Axis, st.Frame)
	lean := func(upDeg, rightDeg float64) {
		u, r := upDeg*math.Pi/180, rightDeg*math.Pi/180
		w.ActiveCraft().CurrentAttitudeDir = st.Target.Axis.Scale(-1).Add(up.Scale(math.Tan(u))).Add(right.Scale(math.Tan(r))).Unit()
	}
	w.NavMode = sim.NavPort
	for _, k := range []struct {
		up, right float64
		want      string
	}{{5, -2, "pitch +5°  yaw -2°"}, {0, 0, "pitch +0°  yaw +0°"}, {-3, 4, "pitch -3°  yaw +4°"}} {
		lean(k.up, k.right)
		if got := navballReadoutLabel(w); got != k.want {
			t.Errorf("lean (%v up, %v right): %q, want %q", k.up, k.right, got, k.want)
		}
	}
	w.NavMode = sim.NavOrbit
	if got := navballReadoutLabel(w); !strings.Contains(got, "hdg") || strings.Contains(got, "yaw") {
		t.Errorf("outside PORT the row = %q, want pitch / hdg", got)
	}
}
