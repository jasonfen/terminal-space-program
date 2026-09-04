package tui

import (
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/key"

	"github.com/jasonfen/terminal-space-program/internal/tui/screens"
)

// rawKeyDisplay maps a bubbletea key string to the token a player reads on
// the F1 overlay (and sees on their keycaps). Only keys whose wire
// spelling differs from what the overlay prints need an entry.
var rawKeyDisplay = map[string]string{
	"left": "←", "right": "→", "up": "↑", "down": "↓",
	"shift+left": "shift+←", "shift+right": "shift+→",
	"shift+up": "shift+↑", "shift+down": "shift+↓",
	" ":  "space",
	"f1": "F1", "f2": "F2", "f4": "F4", "f5": "F5", "f9": "F9",
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
	km := reflect.ValueOf(DefaultKeymap())
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
