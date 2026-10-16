package screens

import (
	"strings"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// ADR 0056 decision 7: Docking Port is a `structure` component in the
// palette; a stage that holds one reads `· port` on its header and
// `port: nose` in INSPECT; a stage without one reads neither.
func TestVABDockingPortComponentShowsPortOnHeaderAndInspector(t *testing.T) {
	v := NewVAB(Theme{})
	v.Reset(spacecraft.Components)

	inPalette := false
	for _, it := range v.palette {
		if it.isComponent && it.id == "docking-port" {
			inPalette = true
			if kind, label := v.paletteItemLabel(it); kind != spacecraft.ComponentStructure || label != "Docking Port" {
				t.Errorf("palette entry = %q / %q, want structure / Docking Port", kind, label)
			}
		}
	}
	if !inPalette {
		t.Fatal("Docking Port is not in the palette")
	}

	v.addComponentToCurrent("probe-core")
	headerOf := func() string {
		return strings.Join(v.renderVehicleColumn(70, 20), "\n")
	}
	inspectorOnHeader := func() string {
		v.focus = focusStack
		for i, r := range v.stackRows() {
			if r.isHeader() && r.stageIdx == 0 {
				v.stackCursor = i
			}
		}
		return strings.Join(v.renderInspector(50), "\n")
	}
	if h := stripANSI(headerOf()); strings.Contains(h, "· port") {
		t.Errorf("a stage without the component reads ported:\n%s", h)
	}
	if in := stripANSI(inspectorOnHeader()); strings.Contains(in, "port: nose") {
		t.Errorf("INSPECT of a port-less stage reads a port:\n%s", in)
	}

	v.addComponentToCurrent("docking-port")
	if h := stripANSI(headerOf()); !strings.Contains(h, "· port") {
		t.Errorf("stage header lacks `· port` after adding Docking Port:\n%s", h)
	}
	if in := stripANSI(inspectorOnHeader()); !strings.Contains(in, "port: nose") {
		t.Errorf("INSPECT lacks `port: nose` after adding Docking Port:\n%s", in)
	}
}
