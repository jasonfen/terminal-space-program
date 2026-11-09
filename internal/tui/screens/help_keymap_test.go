package screens_test

import (
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/key"

	"github.com/jasonfen/terminal-space-program/internal/keylayout"
	"github.com/jasonfen/terminal-space-program/internal/tui"
	"github.com/jasonfen/terminal-space-program/internal/tui/screens"
)

// rawKeyDisplay maps a bubbletea key string to the token a player reads on
// the F1 overlay (and sees on their keycaps). Only keys whose wire
// spelling differs from what the overlay prints need an entry.
var rawKeyDisplay = map[string]string{
	"left": "←", "right": "→", "up": "↑", "down": "↓",
	"shift+left": "shift+←", "shift+right": "shift+→",
	"shift+up": "shift+↑", "shift+down": "shift+↓",
	"alt+left": "alt+←", "alt+right": "alt+→", "alt+up": "alt+↑", "alt+down": "alt+↓",
	" ":  "space",
	"f1": "F1", "f2": "F2", "f3": "F3", "f4": "F4", "f5": "F5", "f9": "F9",
}

func displayKey(raw string) string {
	if d, ok := rawKeyDisplay[raw]; ok {
		return d
	}
	return raw
}

// unadvertised lists the bindings the F1 overlay deliberately does not
// name, keyed by Keymap field. Each needs a reason: the overlay is the
// project's source of truth for keybindings, so an omission is a decision,
// not an oversight.
var unadvertised = map[string]string{
	"BossKey": "deliberately hidden — advertising the panic key defeats it (see the BossKey comment in input.go)",
}

// unadvertisedAlias lists individual keys that are bound as a convenience
// alias of an advertised key and are intentionally left off the overlay,
// keyed "Field:key". Anything not listed here must appear in the overlay.
var unadvertisedAlias = map[string]string{
	"ZoomIn:=":  "unshifted alias of + on US layouts",
	"ZoomOut:_": "shifted alias of -",
	// Option+Z / Option+X (2026-10-05): the fine throttle matches the
	// one-modifier alt+arrow nose trims; the shifted forms still work and
	// the row's description names them.
	"ThrottleUpFine:alt+Z":   "shifted form of alt+z, named in the row text",
	"ThrottleDownFine:alt+X": "shifted form of alt+x, named in the row text",
	// #425: `?` is the reflex key everyone tries first, so it opens Help
	// too, but the overlay teaches F1 as the canonical key — the alias
	// stays undocumented on the row itself.
	"Help:?": "reflex alias of F1 (#425); the overlay advertises F1 as the canonical key",
	// CraftSlot binds the nine digits individually; the overlay
	// advertises them collectively on one "1-9" row.
	"CraftSlot:1": "covered by the 1-9 row", "CraftSlot:2": "covered by the 1-9 row",
	"CraftSlot:3": "covered by the 1-9 row", "CraftSlot:4": "covered by the 1-9 row",
	"CraftSlot:5": "covered by the 1-9 row", "CraftSlot:6": "covered by the 1-9 row",
	"CraftSlot:7": "covered by the 1-9 row", "CraftSlot:8": "covered by the 1-9 row",
	"CraftSlot:9": "covered by the 1-9 row",
}

// TestHelpOverlayCoversKeymap pins the project convention that the F1
// overlay is the source of truth for keybindings (#423): every binding in
// DefaultKeymap must appear in helpSections under a key token that is
// actually one of the keys it is bound to. Without this, the overlay drifts
// silently — it told players `q` quits long after `q` became radial+.
func TestHelpOverlayCoversKeymap(t *testing.T) {
	tokens := screens.HelpKeyTokens()
	km := reflect.ValueOf(tui.DefaultKeymap())
	kt := km.Type()

	for i := 0; i < km.NumField(); i++ {
		field := kt.Field(i).Name
		b, ok := km.Field(i).Interface().(key.Binding)
		if !ok {
			continue
		}
		keys := b.Keys()
		if len(keys) == 0 {
			continue // retired binding kept for compile stability
		}
		if why, skip := unadvertised[field]; skip {
			t.Logf("%s: not in the overlay by design (%s)", field, why)
			continue
		}

		token := b.Help().Key
		if token == "" {
			t.Errorf("%s: bound to %v but has no help token", field, keys)
			continue
		}
		if !tokens[token] {
			t.Errorf("%s: keymap advertises %q but the F1 overlay never names it (bound to %v)", field, token, keys)
		}

		// The advertised token must be one of the keys actually bound,
		// or the overlay is naming a key that does nothing.
		if !advertises(token, keys) {
			t.Errorf("%s: help token %q is not one of its bound keys %v", field, token, keys)
		}

		// Every other bound key is either also named, or listed as a
		// deliberate alias.
		for _, k := range keys {
			d := displayKey(k)
			if d == token || tokens[d] {
				continue
			}
			if _, ok := unadvertisedAlias[field+":"+k]; ok {
				continue
			}
			t.Errorf("%s: key %q (%q) is bound but never named in the overlay — document it or add it to unadvertisedAlias with a reason", field, k, d)
		}
	}
}

// advertises reports whether token names one of keys, allowing the "1-9"
// range shorthand for a binding that takes every digit it spans.
func advertises(token string, keys []string) bool {
	for _, k := range keys {
		if displayKey(k) == token {
			return true
		}
	}
	if lo, hi, ok := strings.Cut(token, "-"); ok && len(lo) == 1 && len(hi) == 1 {
		want := map[string]bool{}
		for c := lo[0]; c <= hi[0]; c++ {
			want[string(c)] = true
		}
		for _, k := range keys {
			delete(want, k)
		}
		return len(want) == 0
	}
	return false
}

// TestHelpKeyTokensSplitsCompoundRows guards the accessor the coverage test
// leans on: an empty or over-eager split would make the check above pass
// vacuously.
func TestHelpKeyTokensSplitsCompoundRows(t *testing.T) {
	tokens := screens.HelpKeyTokens()
	for _, want := range []string{"z", "x", "↑", "↓", "←", "→", "/", "space", "1-9", "ctrl+c"} {
		if !tokens[want] {
			t.Errorf("HelpKeyTokens missing %q — the row splitter dropped a key", want)
		}
	}
	if tokens[""] {
		t.Error("HelpKeyTokens produced an empty token")
	}
	var got []string
	for k := range tokens {
		got = append(got, k)
	}
	sort.Strings(got)
	t.Logf("%d tokens: %s", len(got), strings.Join(got, " "))
}

// TestHelpGlossaryCoversReadoutCodes (ADR 0049 stage A3a): the six codes
// that survive the readout contract, fpa, Q, TCA, TWR, Ap/Pe, Δv/Δincl,
// must each have a line in the F1 glossary block, so a player who hits
// one on a HUD panel can look it up without leaving the game.
func TestHelpGlossaryCoversReadoutCodes(t *testing.T) {
	tokens := screens.HelpKeyTokens()
	for _, code := range []string{"fpa", "Q", "TCA", "TWR", "Ap", "Pe", "Δv", "Δincl"} {
		if !tokens[code] {
			t.Errorf("F1 glossary missing a line naming %q (ADR 0049 readout contract)", code)
		}
	}
}

// TestHelpGlossaryNamesCargoAndHoldRow (ADR 0057 S4): PROPELLANT's cargo:
// and hold: cells are new words on the HUD, so the F1 glossary names both
// and docs/controls.md carries the same row.
func TestHelpGlossaryNamesCargoAndHoldRow(t *testing.T) {
	tokens := screens.HelpKeyTokens()
	for _, code := range []string{"cargo", "hold"} {
		if !tokens[code] {
			t.Errorf("F1 glossary missing a line naming %q (ADR 0057 5.3)", code)
		}
	}
	doc, err := os.ReadFile("../../../docs/controls.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(doc), "| `cargo / hold` |") {
		t.Error("docs/controls.md glossary lacks the `cargo / hold` row")
	}
}

// TestHelpGlossaryNamesBoosterReadouts (ADR 0058 decision 14, guard [13]):
// ENGINE's boosters / core badge and row label are new words on the HUD, so
// F1's READOUT GLOSSARY and docs/controls.md each carry the row, and the
// STAGES pips row says a booster group is lit together.
func TestHelpGlossaryNamesBoosterReadouts(t *testing.T) {
	tokens := screens.HelpKeyTokens()
	for _, code := range []string{"boosters", "core", "ground", "miss", "lpd"} {
		if !tokens[code] {
			t.Errorf("F1 glossary missing a line naming %q (ADR 0058 decision 14)", code)
		}
	}
	doc, err := os.ReadFile("../../../docs/controls.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"| `COVERAGE` |", "no gap this lap", "| `boosters / core` |", "| `ground / miss / lpd` |", "| `rsv` |", "`rsv 10%`", "`+` is long, past the site; `\u2212` is short of it", "DRY", "booster group", "PROPELLANT's fuel row"} {
		if !strings.Contains(string(doc), want) {
			t.Errorf("docs/controls.md glossary lacks %q", want)
		}
	}
}

// TestSpawnFormArrowsRowNamesFrozenEllipse (ADR 0065 decision 8): the SPAWN
// FORM `← / →` row names the frozen-ellipse stop in F1 and in controls.md.
func TestSpawnFormArrowsRowNamesFrozenEllipse(t *testing.T) {
	h := screens.NewHelp(screens.Theme{})
	h.OpenFor("SPAWN FORM")
	f1 := strings.Join(strings.Fields(h.Render(140, 40, keylayout.QWERTY)), " ")
	doc, err := os.ReadFile("../../../docs/controls.md")
	if err != nil {
		t.Fatal(err)
	}
	// Its own row, not a tail on the first: F1 truncates a row to the width,
	// and at the 140x40 Design Size the tail would be cut off.
	const want = "frozen ellipse (1,748 x 17,409 km, 57°, 30 h); spawns slot a fifth of a lap apart"
	if !strings.Contains(f1, "← / → at the Moon one step past the ceiling is the "+want) {
		t.Error("F1 SPAWN FORM page lacks the Moon row")
	}
	if !strings.Contains(string(doc), "| `←` / `→` at the Moon | One step past the ceiling is the "+want+" |") {
		t.Errorf("docs/controls.md lacks the Moon row")
	}
}

// TestHelpNamesTheSiteLandingAutopilot (ADR 0067 decision 12): the `;` row
// reaches Site, `q` in SITE engages Site+, `Z / X` hand over a descent rate
// and `z / x` take the raw throttle, in F1 and in controls.md row for row.
func TestHelpNamesTheSiteLandingAutopilot(t *testing.T) {
	h := screens.NewHelp(screens.Theme{})
	h.OpenFor("MANUAL FLIGHT")
	f1 := strings.Join(strings.Fields(h.Render(140, 40, keylayout.QWERTY)), " ")
	doc, err := os.ReadFile("../../../docs/controls.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Surface → Orbit → Target → Port → Site",
		"q (SITE) in SITE: nose per landing guidance (Site+ brake), straight up in final; throttle stays yours; N flies it all",
		"e (SITE) in SITE: hold straight up (Site−), no guidance; under N, e sinks",
		"e / q (Site+ auto) under N, approach and final: e sinks 1 m/s slower, q faster; z x Z X take the throttle",
	} {
		if !strings.Contains(f1, want) {
			t.Errorf("F1 MANUAL FLIGHT page lacks %q", want)
		}
	}
	for _, want := range []string{
		"Surface → Orbit → Target → Port → Site",
		"| `q` in Site | The guidance nose hold (`hold: Site+ brake (SITE)`)",
		"final: straight up, held, no lean",
		"| `e` in Site | Holds straight up (`hold: Site− (SITE)`)",
		"It never lights, cuts or throttles the engine, never caps warp and holds no rate of descent",
		"| `e` / `q` under `Site+ auto` | Under `N`'s autopilot, in approach and final, set the rate of descent:",
		"Under the `q` nose hold `e` is a plain hold key",
		"| `B` | Plant the deorbit burn only: the same one retro node, footer line and refusals as `N`",
		"`Z` / `X` / `z` / `x` take the throttle and keep the steering",
		"the descent rate `e` / `q` set",
		"(`alt+e` / `alt+q` 0.3 m/s)",
		"| `Site+` / `Site+ auto` / `ROD` | `q` in SITE holds the guidance nose",
		"`N` flies everything (`Site+ auto wait` in amber until the brake lights",
		"It also puts the navball on SITE and engages the full landing autopilot (`hold: Site+ auto wait`, amber)",
		"engages it from the current state with no new burn (footer `Site+ auto: landing from here`)",
		"`hold:` reads `Site+ wait` until the engine is lit",
		"`b` (surface retrograde, the engine yours: footer `Site+ off: engine lit by hand`) switches it off",
	} {
		if !strings.Contains(string(doc), want) {
			t.Errorf("docs/controls.md lacks %q", want)
		}
	}
	tokens := screens.HelpKeyTokens()
	if !tokens["Site+"] || !tokens["ROD"] {
		t.Error("F1 glossary does not name Site+ and ROD")
	}
}

// ADR 0067 decision 8: the `o` row names the landing view, the glossary names
// its rates and the aim cell, and the map markers name the stop mark, in
// docs/controls.md row for row (F1's wording is pinned in help_rows_test.go).
func TestControlsDocNamesTheLandingView(t *testing.T) {
	doc, err := os.ReadFile("../../../docs/controls.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"opens the **landing view** instead",
		"`range:`, `alt:`, `alt rate:`, `stop:`, `fwd vel:`, `lat vel:`, `lpd:`, `miss:`, `cross:`, `aim:`, `hold:`, `throttle:`, `rcs:`, `monoprop:`",
		"| `alt rate` | LANDING chip",
		"| `fwd vel` / `lat vel` | LANDING chip",
		"| `aim` | LANDING chip",
		"| `⊙` | the landing view: where the powered stop ends",
	} {
		if !strings.Contains(string(doc), want) {
			t.Errorf("docs/controls.md lacks %q", want)
		}
	}
}
