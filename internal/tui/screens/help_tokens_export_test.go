package screens

import "strings"

// Test-only seam for the external screens_test package (help_keymap_test.go):
// helpSections is unexported, and the tokenizer below has no production
// caller, so it lives in a _test.go file instead of help.go.

// helpTokenConnectors are the words-between-keys used in a row's left
// column ("z / x", "esc → b"). HelpKeyTokens splits on these rather than
// on bare whitespace so a connector is never mistaken for a key, while the
// pan row ("↑ ↓ ← →") still yields four separate arrow tokens.
var helpTokenConnectors = []string{" / ", " → "}

// HelpKeyTokens returns every key token the overlay names in its left
// column, split out of the compound rows ("z / x" → "z", "x"; "↑ ↓ ← →" →
// four arrows). The keymap-coverage test (screens_test) reads it to prove
// the overlay still names every binding in DefaultKeymap, so helpSections
// itself can stay unexported. Tokens are the QWERTY spellings authored in
// helpSections, before any keylayout.DisplayToken translation.
func HelpKeyTokens() map[string]bool {
	tokens := map[string]bool{}
	for _, s := range helpSections {
		for _, r := range s.rows {
			for _, tok := range splitHelpToken(r[0]) {
				tokens[tok] = true
			}
		}
	}
	return tokens
}

// splitHelpToken breaks one left-column cell into the individual keys it
// names. A cell that is itself a key ("/") is returned whole.
func splitHelpToken(cell string) []string {
	cell = strings.TrimSpace(cell)
	if cell == "" {
		return nil
	}
	pieces := []string{cell}
	for _, sep := range helpTokenConnectors {
		var next []string
		for _, p := range pieces {
			next = append(next, strings.Split(p, sep)...)
		}
		pieces = next
	}
	var out []string
	for _, p := range pieces {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		// A space-separated run of short glyphs is a key list ("↑ ↓ ← →");
		// anything with a word in it is one label ("click body").
		if fields := strings.Fields(p); len(fields) > 1 && allShort(fields) {
			out = append(out, fields...)
			continue
		}
		out = append(out, p)
	}
	return out
}

func allShort(fields []string) bool {
	for _, f := range fields {
		if len([]rune(f)) > 3 {
			return false
		}
	}
	return true
}
