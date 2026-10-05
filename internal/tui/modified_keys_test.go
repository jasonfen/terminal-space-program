package tui

import (
	"bytes"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// parsedMsgs runs raw terminal bytes through Bubble Tea's own input parser
// and returns what it hands the program: the exact messages App.Update sees
// from a real terminal.
func parsedMsgs(t *testing.T, raw string) []tea.Msg {
	t.Helper()
	var got []tea.Msg
	var m capture
	m.out = &got
	p := tea.NewProgram(m, tea.WithInput(bytes.NewBufferString(raw)), tea.WithOutput(&bytes.Buffer{}))
	if _, err := p.Run(); err != nil {
		t.Fatalf("parse %q: %v", raw, err)
	}
	return got
}

type capture struct{ out *[]tea.Msg }

func (c capture) Init() tea.Cmd {
	return tea.Tick(200*time.Millisecond, func(time.Time) tea.Msg { return captureDone{} })
}
func (c capture) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg.(type) {
	case captureDone:
		return c, tea.Quit
	case tea.WindowSizeMsg:
		return c, nil
	}
	*c.out = append(*c.out, msg)
	return c, nil
}
func (c capture) View() string { return "" }

type captureDone struct{}

// TestShiftedKeysWorkInEveryTerminalKeyMode: a terminal in xterm
// modifyOtherKeys mode (Ghostty after another program switched it on)
// sends shift+\ as ESC[27;2;124~, and a kitty-protocol terminal as
// ESC[124;2u or ESC[92;2u. Bubble Tea v1 drops all of these as unknown
// sequences, so | (reset trims), Z/X (throttle 10%) did nothing at all
// (Jason's playtest 2026-10-04). The App decodes them into the key the
// player pressed.
func TestShiftedKeysWorkInEveryTerminalKeyMode(t *testing.T) {
	for _, raw := range []string{"\x1b[27;2;124~", "\x1b[124;2u", "\x1b[92;2u"} {
		a, c := padApp(t)
		a.Update(keyType(tea.KeyRight))
		a.Update(keyType(tea.KeyUp))
		if c.PitchTrim == 0 || c.HeadingTrim == 0 {
			t.Fatalf("setup: arrows did not trim")
		}
		for _, msg := range parsedMsgs(t, raw) {
			a.Update(msg)
		}
		if c.PitchTrim != 0 || c.HeadingTrim != 0 {
			t.Errorf("%q (shift+\\): trims not reset: PitchTrim=%v HeadingTrim=%v", raw, c.PitchTrim, c.HeadingTrim)
		}
	}
	for _, tc := range []struct {
		raw  string
		want float64
	}{
		{"\x1b[27;2;90~", 0.6},  // shift+z (modifyOtherKeys): Z, +10%
		{"\x1b[122;2u", 0.6},    // shift+z (kitty): Z
		{"\x1b[27;2;88~", 0.4},  // shift+x: X, -10%
		{"\x1b[27;4;90~", 0.51}, // shift+alt+z: alt+Z, the fine +1% (modifier 4 = 1 + shift 1 + alt 2)
	} {
		a, _ := padApp(t)
		a.world.SetThrottle(0.5)
		for _, msg := range parsedMsgs(t, tc.raw) {
			a.Update(msg)
		}
		if got := a.world.ActiveCraft().Throttle; got < tc.want-1e-9 || got > tc.want+1e-9 {
			t.Errorf("%q: throttle %.2f, want %.2f", tc.raw, got, tc.want)
		}
	}
}

// TestDecodeModifiedKeyTable pins the decoder on the forms a player meets:
// what each sequence must read as once it reaches the keymap.
func TestDecodeModifiedKeyTable(t *testing.T) {
	for raw, want := range map[string]string{
		"\x1b[27;2;124~": "|",
		"\x1b[92;2u":     "|",
		"\x1b[124;2u":    "|",
		"\x1b[122;2u":    "Z",
		"\x1b[27;2;63~":  "?",
		"\x1b[47;2u":     "?",
		"\x1b[27;4;90~":  "alt+Z",
		"\x1b[27;5;99~":  "ctrl+c",
		"\x1b[9;2u":      "shift+tab",
		"\x1b[13;2u":     "enter",
		"\x1b[27;2;60~":  "<",
	} {
		msgs := parsedMsgs(t, raw)
		if len(msgs) != 1 {
			t.Fatalf("%q: parser gave %d messages, want 1", raw, len(msgs))
		}
		k, ok := decodeModifiedKey(msgs[0])
		if !ok || k.String() != want {
			t.Errorf("%q: decoded %q (ok=%v), want %q", raw, k.String(), ok, want)
		}
	}
	// A kitty key release must not act as a second press.
	if _, ok := decodeModifiedKey(parsedMsgs(t, "\x1b[124;2:3u")[0]); ok {
		t.Error("a key release decoded as a key press")
	}
	// Ordinary keys pass straight through untouched.
	if _, ok := decodeModifiedKey(keyRunes("|")); ok {
		t.Error("a plain KeyMsg was treated as a modified sequence")
	}
}
