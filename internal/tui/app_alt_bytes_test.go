package tui

import (
	"io"
	"math"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// keyRecorder is a throwaway tea.Model that records every KeyMsg Bubble Tea's
// own byte parser produces, then quits once it has seen n of them.
type keyRecorder struct {
	n    int
	keys []tea.KeyMsg
}

func (k *keyRecorder) Init() tea.Cmd { return nil }
func (k *keyRecorder) Update(m tea.Msg) (tea.Model, tea.Cmd) {
	if km, ok := m.(tea.KeyMsg); ok {
		k.keys = append(k.keys, km)
		if len(k.keys) >= k.n {
			return k, tea.Quit
		}
	}
	return k, nil
}
func (k *keyRecorder) View() string { return "" }

// parseTerminalBytes pushes raw terminal bytes through Bubble Tea's real input
// parser and returns the KeyMsgs it emits.
func parseTerminalBytes(t *testing.T, raw string, n int) []tea.KeyMsg {
	t.Helper()
	rec := &keyRecorder{n: n}
	p := tea.NewProgram(rec, tea.WithInput(strings.NewReader(raw)), tea.WithOutput(io.Discard), tea.WithoutSignals())
	done := make(chan error, 1)
	go func() { _, err := p.Run(); done <- err }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		p.Kill()
		t.Fatal("bubbletea never delivered the keys")
	}
	return rec.keys
}

// TestMacTerminalFinePitchBytes (#548 review line 83): Terminal.app and
// Ghostty send Option+Left as ESC b and Option+Right as ESC f, whatever the
// Option-as-Meta setting. Feed those exact bytes through Bubble Tea's parser
// and the real App.Update: they must be the fine pitch trims. The CSI 1;3D/C
// form (iTerm2 style) stays working.
func TestMacTerminalFinePitchBytes(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want float64 // PitchTrim in degrees after one press
	}{
		{"ESC f (Option+Right on Terminal.app/Ghostty)", "\x1bf", 1},
		{"ESC b (Option+Left on Terminal.app/Ghostty)", "\x1bb", -1},
		{"CSI 1;3C (alt+right)", "\x1b[1;3C", 1},
		{"CSI 1;3D (alt+left)", "\x1b[1;3D", -1},
	}
	for _, tc := range cases {
		a, c := padApp(t)
		for _, km := range parseTerminalBytes(t, tc.raw, 1) {
			a.Update(km)
		}
		if got := c.PitchTrim * 180 / math.Pi; math.Abs(got-tc.want) > 1e-6 {
			t.Errorf("%s: PitchTrim %.4f deg, want %v", tc.name, got, tc.want)
		}
	}
}

// TestBareBAndFKeepTheirMeaning: the aliases are alt+b / alt+f only; plain b
// still lights the engine and plain f still cycles focus (no pitch trim).
func TestBareBAndFKeepTheirMeaning(t *testing.T) {
	a, c := padApp(t)
	c.Throttle = 0.5
	a.Update(keyRunes("f"))
	if c.PitchTrim != 0 {
		t.Errorf("plain f moved the pitch trim: %v", c.PitchTrim)
	}
	a.Update(keyRunes("b"))
	if c.PitchTrim != 0 {
		t.Errorf("plain b moved the pitch trim: %v", c.PitchTrim)
	}
}
