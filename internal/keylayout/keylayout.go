// Package keylayout maps keypresses between the player's physical keyboard
// layout and the QWERTY positions every binding is authored against
// (ADR 0022). Terminals deliver already-resolved characters, not physical
// scan codes, so all handling is character→character.
//
// The model is "ingest-normalize + display-translate": a keypress is
// Normalize-d from the active layout back to its QWERTY-position rune before
// any binding match (so internal/tui/input.go's Keymap and every raw-string
// screen handler stay authored in QWERTY and need no changes), and key
// labels are Display-translated the other way at render time so the help
// overlay shows the player's actual keycaps.
//
// Slice 1 ships QWERTY (identity) and QWERTZ (a single y↔z letter swap).
// The mechanism is general — adding a layout is a new injective rune map
// plus a test — but AZERTY, Dvorak, and punctuation position-mapping are
// deliberately out of scope (see ADR 0022). The package is a pure leaf: no
// imports, no upward dependency on settings or tui.
package keylayout

// Layout identifies a physical keyboard layout. The string value is the
// stable key persisted in settings.json (settings.Settings.KeyboardLayout).
type Layout string

const (
	QWERTY Layout = "qwerty"
	QWERTZ Layout = "qwertz"
)

// toQWERTY maps a layout-resolved rune to the rune QWERTY produces at the
// same physical key. Identity runes are omitted, so QWERTY needs no entry.
// Each map MUST be injective (a bijection over the keys it names) or two
// layout keys would collide onto one QWERTY binding — TestMapsInjective
// guards this.
//
// QWERTZ differs from QWERTY only in the swapped Y and Z keys; every other
// letter (including M) sits at its QWERTY position on the German layout, so
// the whole layout is four entries.
var toQWERTY = map[Layout]map[rune]rune{
	QWERTZ: {'y': 'z', 'z': 'y', 'Y': 'Z', 'Z': 'Y'},
}

// fromQWERTY is the inverse of toQWERTY, derived once at init for the
// display direction (a QWERTY-authored label rune → the rune on the
// player's keyboard). Built by inversion so the two directions can never
// drift apart.
var fromQWERTY = func() map[Layout]map[rune]rune {
	out := make(map[Layout]map[rune]rune, len(toQWERTY))
	for l, m := range toQWERTY {
		inv := make(map[rune]rune, len(m))
		for k, v := range m {
			inv[v] = k
		}
		out[l] = inv
	}
	return out
}()

// ordered is the cycle order presented in the Controls screen. Append-only:
// the order is part of the UI contract.
var ordered = []Layout{QWERTY, QWERTZ}

var labels = map[Layout]string{
	QWERTY: "QWERTY",
	QWERTZ: "QWERTZ (y↔z)",
}

// Valid reports whether l is a known layout.
func Valid(l Layout) bool {
	if l == QWERTY {
		return true
	}
	_, ok := toQWERTY[l]
	return ok
}

// Resolve coerces a persisted string to a known Layout, defaulting to
// QWERTY for the empty string (an absent settings.json field) or any
// unknown value (a newer build's layout this binary doesn't understand).
func Resolve(s string) Layout {
	l := Layout(s)
	if Valid(l) {
		return l
	}
	return QWERTY
}

// Label returns the human-readable name shown in the Controls screen.
func Label(l Layout) string {
	if s, ok := labels[l]; ok {
		return s
	}
	return string(l)
}

// All returns the layouts in cycle order. The caller must not mutate it.
func All() []Layout { return ordered }

// Next returns the layout after l in cycle order, wrapping around. An
// unknown l restarts the cycle at the first layout.
func Next(l Layout) Layout {
	for i, x := range ordered {
		if x == l {
			return ordered[(i+1)%len(ordered)]
		}
	}
	return ordered[0]
}

// Normalize maps a rune typed on layout l back to its QWERTY-position rune.
// Runes the layout doesn't remap (and every rune under QWERTY) pass through
// unchanged.
func Normalize(l Layout, r rune) rune {
	if m, ok := toQWERTY[l]; ok {
		if q, ok := m[r]; ok {
			return q
		}
	}
	return r
}

// Display maps a QWERTY-authored rune to the rune that sits at the same
// physical key on layout l — the inverse of Normalize, used to translate
// key labels for rendering.
func Display(l Layout, r rune) rune {
	if m, ok := fromQWERTY[l]; ok {
		if d, ok := m[r]; ok {
			return d
		}
	}
	return r
}

// DisplayToken Display-translates every rune of a key-label token (e.g.
// "z / x" → "y / x" under QWERTZ). Apply only to the key-token field of a
// help row, never to its description — a description like "zoom in" must not
// have its letters swapped.
func DisplayToken(l Layout, token string) string {
	if l == QWERTY {
		return token
	}
	out := []rune(token)
	for i, r := range out {
		out[i] = Display(l, r)
	}
	return string(out)
}
