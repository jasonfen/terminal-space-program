package screens

import (
	"strings"
	"testing"
)

// R4 #4: a one-component stage reads "1 component", not "1 components".
func TestVABStageLabelSingular(t *testing.T) {
	v := NewVAB(Theme{})
	v.Reset(testVABComps())
	v.addComponentToCurrent("eng")
	if got := v.stageLabel(v.stages[0]); got != "1 component" {
		t.Errorf("stageLabel = %q, want %q", got, "1 component")
	}
	v.addComponentToCurrent("tank")
	if got := v.stageLabel(v.stages[0]); got != "2 components" {
		t.Errorf("stageLabel = %q, want %q", got, "2 components")
	}
}

// R4 #5: when the vehicle column scrolls, the first visible content row is
// always a stage header (never an orphaned component row), and the cursor
// stays visible. 12 stages, the 140x40 column height.
func TestVABWindowStartsOnStageHeader(t *testing.T) {
	v := NewVAB(Theme{})
	v.Reset(testVABComps())
	v.addComponentToCurrent("eng")
	v.addComponentToCurrent("tank")
	for i := 1; i < 12; i++ {
		v.newStage()
		v.addComponentToCurrent("eng2")
		v.addComponentToCurrent("tank2")
	}
	rows := v.stackRows()
	isStageLine := func(s string) bool {
		s = strings.TrimLeft(s, " →")
		return strings.HasPrefix(s, "S") && len(s) > 1 && s[1] >= '0' && s[1] <= '9'
	}
	for _, h := range []int{18, 22, 27} {
		for cur := range rows {
			v.stackCursor = cur
			v.focus = focusStack
			out := v.renderVehicleColumn(60, h)
			body := -1
			for i, l := range out {
				if strings.Contains(l, "↑") && strings.Contains(l, "more") {
					body = i + 1
				}
			}
			if body < 0 {
				continue // not scrolled from the top: nothing clipped above
			}
			if !isStageLine(out[body]) {
				t.Errorf("h=%d cursor=%d: first visible row %q is not a stage header", h, cur, out[body])
			}
			seen := false
			for _, l := range out {
				if strings.Contains(l, "→ ") && !strings.Contains(l, "→ top") && !strings.Contains(l, "bottom") {
					seen = true
				}
			}
			if !seen {
				t.Errorf("h=%d cursor=%d: cursor row not visible", h, cur)
			}
		}
	}
}
