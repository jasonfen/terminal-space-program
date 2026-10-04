package tui

import (
	"io"
	"math"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jasonfen/terminal-space-program/internal/sim"
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

// TestPlanInclinationOnThePadRefusesInPlainWords (#548 review line 88): `I`
// on the pad with the Moon targeted flashed
// `inclination: planinclination: source already at target inclination`
// while TARGET read a 47 degree gap. A vessel on the ground has no orbit to
// tilt; say so, point at the plan: row, and plant nothing. Through the real
// key handler.
func TestPlanInclinationOnThePadRefusesInPlainWords(t *testing.T) {
	a, c := padApp(t)
	for i, b := range a.world.System().Bodies {
		if b.ID == "moon" {
			a.world.SetTargetBody(i)
		}
	}
	pressKey(a, 'I')
	got := a.statusMsg
	if strings.Contains(got, "planinclination") || strings.Contains(got, "already at target") {
		t.Errorf("pad refusal still reads as a no-op: %q", got)
	}
	if !strings.HasPrefix(got, "inclination: ") || !strings.Contains(got, "pad") || !strings.Contains(got, "plan:") {
		t.Errorf("pad refusal %q should say the vessel is on the pad and point at the plan: row", got)
	}
	if n := len(c.Nodes); n != 0 {
		t.Errorf("a refused I planted %d nodes", n)
	}
}

// TestPlanInclinationNoOpFlashHasNoPackagePrefix: the planner's own error
// strings carry a "planinclination: " package prefix that the flash must
// not repeat after "inclination: ". Orbiting vessel, no target, already
// equatorial: the real no-op.
func TestPlanInclinationNoOpFlashHasNoPackagePrefix(t *testing.T) {
	a, _ := padApp(t)
	pad := a.world.ActiveCraft()
	idx := len(a.world.Crafts)
	if _, err := a.world.SpawnCraft(sim.SpawnSpec{AltitudeM: 400e3, Inclination: 0.0}); err != nil {
		t.Fatal(err)
	}
	_ = pad
	a.world.ActiveCraftIdx = idx
	pressKey(a, 'I')
	if strings.Contains(a.statusMsg, "planinclination") {
		t.Errorf("flash repeats the package prefix: %q", a.statusMsg)
	}
	if !strings.HasPrefix(a.statusMsg, "inclination: ") {
		t.Errorf("flash %q does not start with the key label", a.statusMsg)
	}
}
