package screens

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// VAB visual legend (ADR 0030 §6). Glyph AND color both encode the component
// kind, identical in the palette and the vehicle column, so the player learns
// exactly one legend. A component may override either via its catalog
// Glyph / Color fields. Glyphs are deliberately SINGLE-WIDTH Unicode — a
// double-width emoji would desync the two-column lipgloss.Width math the
// layout depends on, so none are used here.
var kindGlyph = map[string]string{
	spacecraft.ComponentEngine:      "➤",
	spacecraft.ComponentTank:        "▮",
	spacecraft.ComponentCommandCore: "◈",
	spacecraft.ComponentAntenna:     "Ψ",
	spacecraft.ComponentStructure:   "▭",
}

var kindColor = map[string]string{
	spacecraft.ComponentEngine:      "#FFAF00", // amber
	spacecraft.ComponentTank:        "#5FD7FF", // cyan
	spacecraft.ComponentCommandCore: "#5FD75F", // green
	spacecraft.ComponentAntenna:     "#AF87FF", // violet
	spacecraft.ComponentStructure:   "#9E9E9E", // gray
}

// catalogColor tints an opaque atomic catalog block in the vehicle column
// (matches the yellow of spacecraft.VesselGlyph) so it reads distinctly from
// the kind-colored composed components.
const catalogColor = "#FFD93D"

func glyphForKind(kind string) string {
	if g, ok := kindGlyph[kind]; ok {
		return g
	}
	return "•"
}

func styleForKind(kind string) lipgloss.Style {
	if c, ok := kindColor[kind]; ok {
		return lipgloss.NewStyle().Foreground(lipgloss.Color(c))
	}
	return lipgloss.NewStyle()
}

// componentGlyph resolves a component's display glyph, honoring a
// per-component override (ADR 0030 §6) and falling back to the kind default.
func (v *VAB) componentGlyph(id string) string {
	if c, ok := v.comps[id]; ok {
		if c.Glyph != "" {
			return c.Glyph
		}
		return glyphForKind(c.Kind)
	}
	return "?"
}

// componentStyle resolves a component's display color the same way.
func (v *VAB) componentStyle(id string) lipgloss.Style {
	if c, ok := v.comps[id]; ok {
		if c.Color != "" {
			return lipgloss.NewStyle().Foreground(lipgloss.Color(c.Color))
		}
		return styleForKind(c.Kind)
	}
	return lipgloss.NewStyle()
}

// kindRank orders a kind by vabKindOrder for canonical fold-all display
// (ADR 0030 §4); unknown kinds sort last.
func kindRank(kind string) int {
	for i, k := range vabKindOrder {
		if k == kind {
			return i
		}
	}
	return len(vabKindOrder)
}

// clampI clamps i into [lo, hi].
func clampI(i, lo, hi int) int {
	if i < lo {
		return lo
	}
	if i > hi {
		return hi
	}
	return i
}
