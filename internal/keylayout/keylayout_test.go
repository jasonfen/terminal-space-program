package keylayout

import "testing"

// TestMapsInjective is the load-bearing invariant: a layout map must be a
// bijection over the keys it names, or two physical keys would normalize
// onto one QWERTY binding (a silent collision). Guards every present and
// future layout table.
func TestMapsInjective(t *testing.T) {
	for l, m := range toQWERTY {
		seen := make(map[rune]rune, len(m))
		for from, to := range m {
			if prev, ok := seen[to]; ok {
				t.Errorf("layout %q: %q and %q both map to %q (not injective)",
					l, string(prev), string(from), string(to))
			}
			seen[to] = from
		}
	}
}

// TestRoundTrip asserts Display is the exact inverse of Normalize for every
// remapped rune — the property that lets a player press a key and see it
// labelled with their own keycap.
func TestRoundTrip(t *testing.T) {
	for l, m := range toQWERTY {
		for layoutRune, qwertyRune := range m {
			if got := Normalize(l, layoutRune); got != qwertyRune {
				t.Errorf("Normalize(%q, %q) = %q, want %q", l, string(layoutRune), string(got), string(qwertyRune))
			}
			if got := Display(l, qwertyRune); got != layoutRune {
				t.Errorf("Display(%q, %q) = %q, want %q", l, string(qwertyRune), string(got), string(layoutRune))
			}
		}
	}
}

func TestQWERTYIsIdentity(t *testing.T) {
	for r := rune(0x20); r < 0x7f; r++ {
		if got := Normalize(QWERTY, r); got != r {
			t.Errorf("Normalize(QWERTY, %q) = %q, want identity", string(r), string(got))
		}
		if got := Display(QWERTY, r); got != r {
			t.Errorf("Display(QWERTY, %q) = %q, want identity", string(r), string(got))
		}
	}
	if got := DisplayToken(QWERTY, "z / x"); got != "z / x" {
		t.Errorf("DisplayToken(QWERTY) altered token: %q", got)
	}
}

func TestQWERTZSwap(t *testing.T) {
	cases := []struct{ in, want rune }{
		{'z', 'y'}, {'y', 'z'}, {'Z', 'Y'}, {'Y', 'Z'},
		{'x', 'x'}, {'w', 'w'}, {'m', 'm'}, {'.', '.'},
	}
	for _, c := range cases {
		if got := Normalize(QWERTZ, c.in); got != c.want {
			t.Errorf("Normalize(QWERTZ, %q) = %q, want %q", string(c.in), string(got), string(c.want))
		}
	}
}

// TestDisplayTokenThrottle is the concrete help-overlay behaviour: a QWERTZ
// player's throttle row reads "y / x" (their physical keycaps), while a
// description containing 'z' would be mangled — so only tokens get translated.
func TestDisplayTokenThrottle(t *testing.T) {
	if got := DisplayToken(QWERTZ, "z / x"); got != "y / x" {
		t.Errorf("DisplayToken(QWERTZ, %q) = %q, want %q", "z / x", got, "y / x")
	}
	if got := DisplayToken(QWERTZ, "Z / X"); got != "Y / X" {
		t.Errorf("DisplayToken(QWERTZ, %q) = %q, want %q", "Z / X", got, "Y / X")
	}
	// Tokens without y/z are untouched.
	if got := DisplayToken(QWERTZ, "shift+↑ / ↓"); got != "shift+↑ / ↓" {
		t.Errorf("DisplayToken(QWERTZ) altered an unrelated token: %q", got)
	}
}

func TestResolve(t *testing.T) {
	cases := map[string]Layout{
		"":          QWERTY, // absent field
		"qwerty":    QWERTY,
		"qwertz":    QWERTZ,
		"dvorak":    QWERTY, // unknown → safe default
		"AZERTY":    QWERTY, // case-sensitive; unknown → default
	}
	for in, want := range cases {
		if got := Resolve(in); got != want {
			t.Errorf("Resolve(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNextCycles(t *testing.T) {
	if got := Next(QWERTY); got != QWERTZ {
		t.Errorf("Next(QWERTY) = %q, want QWERTZ", got)
	}
	if got := Next(QWERTZ); got != QWERTY {
		t.Errorf("Next(QWERTZ) = %q, want QWERTY (wrap)", got)
	}
	if got := Next("bogus"); got != QWERTY {
		t.Errorf("Next(unknown) = %q, want first layout", got)
	}
}
