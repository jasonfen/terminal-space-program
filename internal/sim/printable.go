package sim

import (
	"strings"
	"unicode"
)

// PrintableOnly drops every rune that is not printable (ESC, \n, \r, \t and
// the rest of the control set). Text one player types and another player's
// terminal renders passes through it where it enters shared state, so a
// pasted escape sequence can never reach a peer's screen.
func PrintableOnly(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsPrint(r) {
			return r
		}
		return -1
	}, s)
}
