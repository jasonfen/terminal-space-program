package screens

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// Ghost styling (v0.27 S5): a populated w.Ghosts renders the ghost's
// glyph and handle on the orbit canvas; an empty slate renders none.
func TestOrbitViewRendersGhosts(t *testing.T) {
	th := Theme{
		Primary: lipgloss.NewStyle(),
		Warning: lipgloss.NewStyle(),
		Alert:   lipgloss.NewStyle(),
		Dim:     lipgloss.NewStyle(),
		HUDBox:  lipgloss.NewStyle().Border(lipgloss.RoundedBorder()),
		Footer:  lipgloss.NewStyle(),
		Title:   lipgloss.NewStyle(),
	}
	v := NewOrbitView(th)
	v.Resize(120, 40)

	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	before := v.Render(w, 0, 120, 40)
	if strings.Contains(before, "gern") {
		t.Fatal("handle rendered with no ghosts set")
	}

	// Park the ghost on the active craft's orbit, opposite side — on
	// screen at the default zoom, clear of the vessel's own cell.
	c := w.ActiveCraft()
	ghostPos := w.BodyPosition(c.Primary).Add(c.State.R.Scale(-1))
	w.Ghosts = []sim.Ghost{{
		Owner: "SHA256:x", Handle: "gern", Name: "gern's ship",
		Glyph: "◆", PrimaryID: c.Primary.ID, Pos: ghostPos,
	}}
	out := v.Render(w, 0, 120, 40)
	if !strings.Contains(out, "gern") {
		t.Error("ghost handle not rendered")
	}
	if !strings.Contains(out, "◆") {
		t.Error("ghost glyph not rendered")
	}
}
