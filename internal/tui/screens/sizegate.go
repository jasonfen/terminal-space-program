package screens

import (
	"fmt"
	"strings"
)

// Playable terminal floor (v0.27 S2, ADR 0034). Width is calibrated
// against the widest fixed element the orbit screen composes (the HUD
// row measures 104 cells); below it one line wraps and every frame
// scrolls the terminal. Height 24 keeps the canvas + HUD readable and
// matches the classic terminal minimum. Shared by local play and ssh
// sessions — the gate in App.View replaces rendering below this floor.
//
// This is the **Playable Floor** (CONTEXT.md "HUD & overlays" / ADR
// 0046): the hard render gate, unchanged by #422. It is distinct from
// the **Design Size** below, the size every screen is actually laid out
// and golden-tested at — a screen looking wrong AT OR ABOVE the Design
// Size is a bug, while between the two the contract is Graceful Shrink
// (see orbit_chips.go's layoutChipsBySide).
const (
	MinTerminalWidth  = 104
	MinTerminalHeight = 24
)

// DesignWidth / DesignHeight are the **Design Size** (ADR 0046, #422):
// the size every screen is laid out, reviewed, and golden-tested at, and
// the size README.md / docs/controls.md and the too-small screen below
// tell players to use. Anything wrong at or above this size is a bug;
// below it down to the Playable Floor, chips are expected to shrink
// (Compact Form) and, at worst, drop behind a Hidden Stub rather than
// overlap — see CONTEXT.md's "Graceful Shrink" / "Compact Form" entries.
const (
	DesignWidth  = 140
	DesignHeight = 40
)

// RenderSizeGate is the blocking too-small screen. Deliberately
// unstyled: a terminal in a broken state should get the most robust
// output we can produce. Safe at any size — lines are truncated to w
// and the block is centered in w×h.
func RenderSizeGate(w, h int) string {
	content := []string{
		"TERMINAL TOO SMALL",
		"",
		fmt.Sprintf("now %d×%d — need at least %d×%d", w, h, MinTerminalWidth, MinTerminalHeight),
		fmt.Sprintf("designed for %d×%d — chips compact gracefully below it", DesignWidth, DesignHeight),
		"",
		"resize the window to keep flying",
	}
	pad := (h - len(content)) / 2
	if pad < 0 {
		pad = 0
	}
	lines := make([]string, 0, h)
	for i := 0; i < pad; i++ {
		lines = append(lines, "")
	}
	for _, c := range content {
		if len(lines) >= h && h > 0 {
			break
		}
		r := []rune(c)
		if len(r) > w && w > 0 {
			r = r[:w]
		}
		left := (w - len(r)) / 2
		if left < 0 {
			left = 0
		}
		lines = append(lines, strings.Repeat(" ", left)+string(r))
	}
	return strings.Join(lines, "\n")
}
