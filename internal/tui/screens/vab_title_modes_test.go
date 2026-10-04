package screens

import (
	"strings"
	"testing"
)

// TestVABModesNameThemselvesOnTheTitleRow pins review #555 L10: the VAB's
// Save design, Load design and Σ Δv target modes were never reached
// through the keys that open them, so nothing tied TitleScreen (the Title
// Row's screen name) to the modal actually drawn. Each mode is entered with
// the real build-screen key, at both Design Sizes the frame must keep its
// exact height, name itself in the box and legend, and esc must give the
// build title back.
func TestVABModesNameThemselvesOnTheTitleRow(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	const build = "Vehicle Assembly (VAB)"
	cases := []struct{ key, title, box, legend string }{
		{"s", "Save design", "SAVE DESIGN", "[enter] save"},
		{"o", "Load design", "LOAD DESIGN", "[enter] load"},
		{"t", "Σ Δv target", "Σ Δv TARGET", "[enter] set"},
	}
	for _, sz := range [][2]int{{140, 40}, {181, 49}} {
		for _, c := range cases {
			v := NewVAB(Theme{})
			v.Reset(testVABComps())
			v.addComponentToCurrent("eng")
			if got := v.TitleScreen(); got != build {
				t.Fatalf("build TitleScreen = %q", got)
			}
			v.HandleKey(c.key)
			if got := v.TitleScreen(); got != c.title {
				t.Errorf("key %q: TitleScreen = %q, want %q", c.key, got, c.title)
			}
			out := stripANSI(v.Render(sz[0], sz[1]))
			if n := len(strings.Split(out, "\n")); n != sz[1] {
				t.Errorf("%dx%d %s: rendered %d rows, want %d", sz[0], sz[1], c.title, n, sz[1])
			}
			for _, want := range []string{c.box, c.legend, "[esc]"} {
				if !strings.Contains(out, want) {
					t.Errorf("%dx%d %s: missing %q:\n%s", sz[0], sz[1], c.title, want, out)
				}
			}
			v.HandleKey("esc")
			if got := v.TitleScreen(); got != build {
				t.Errorf("key %q then esc: TitleScreen = %q, want %q", c.key, got, build)
			}
		}
	}
}
