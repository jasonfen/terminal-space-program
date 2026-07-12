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
const (
	MinTerminalWidth  = 104
	MinTerminalHeight = 24
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
