package screens

import (
	"strings"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// ADR 0057 decision 5.1: Cargo Hold is a `structure` component in the palette;
// a stage that holds one reads `· hold` on its header and `hold: yes` in
// INSPECT, and the palette entry carries the one-line description; a stage
// without one reads neither. A port and a hold sit on one header together.
func TestVABCargoHoldComponentShowsHoldOnHeaderAndInspector(t *testing.T) {
	v := NewVAB(Theme{})
	v.Reset(spacecraft.Components)

	inPalette := false
	for _, it := range v.palette {
		if it.isComponent && it.id == "cargo-hold" {
			inPalette = true
			if kind, label := v.paletteItemLabel(it); kind != spacecraft.ComponentStructure || label != "Cargo Hold" {
				t.Errorf("palette entry = %q / %q, want structure / Cargo Hold", kind, label)
			}
		}
	}
	if !inPalette {
		t.Fatal("Cargo Hold is not in the palette")
	}

	v.addComponentToCurrent("probe-core")
	header := func() string { return stripANSI(strings.Join(v.renderVehicleColumn(70, 20), "\n")) }
	inspect := func() string {
		v.focus = focusStack
		for i, r := range v.stackRows() {
			if r.isHeader() && r.stageIdx == 0 {
				v.stackCursor = i
			}
		}
		return stripANSI(strings.Join(v.renderInspector(50), "\n"))
	}
	if h := header(); strings.Contains(h, "· hold") {
		t.Errorf("a stage without the component reads as a hold:\n%s", h)
	}
	if in := inspect(); strings.Contains(in, "hold: yes") {
		t.Errorf("INSPECT of a hold-less stage reads a hold:\n%s", in)
	}

	v.addComponentToCurrent("cargo-hold")
	if h := header(); !strings.Contains(h, "· hold") {
		t.Errorf("stage header lacks `· hold` after adding Cargo Hold:\n%s", h)
	}
	if in := inspect(); !strings.Contains(in, "hold: yes") {
		t.Errorf("INSPECT lacks `hold: yes` after adding Cargo Hold:\n%s", in)
	}

	v.addComponentToCurrent("docking-port")
	if h := header(); !strings.Contains(h, "· port") || !strings.Contains(h, "· hold") {
		t.Errorf("a stage with both reads %q:\n%s", "· port · hold", h)
	}
}
