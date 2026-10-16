package screens

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// GK Q1 (#589): the keys that ask [y/n] before acting get their own amber row
// in the roster's KEYS box, off the grey rows. The row names exactly the keys
// this seat can press that arm an ask.

func askRow(out string) string {
	for _, ln := range strings.Split(out, "\n") {
		if strings.Contains(ln, "asks first:") {
			return ln
		}
	}
	return ""
}

func TestRosterAskingKeysAreMarked(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(termenv.Ascii) })
	amber := lipgloss.NewStyle().Foreground(lipgloss.Color("#FFAF00"))
	grey := lipgloss.NewStyle().Foreground(lipgloss.Color("#5F5F5F"))
	th := sessionTheme()
	th.Warning, th.Footer = amber, grey
	// Positive control: the instrument can tell the two styles apart.
	if amber.Render("x") == grey.Render("x") {
		t.Fatal("setup: amber and grey render identically, colour is off")
	}

	t.Run("host", func(t *testing.T) {
		out := NewSessionScreen(th).Render(sessionWorld(t, true), 140, 0)
		row := askRow(out)
		if row == "" {
			t.Fatalf("no 'asks first:' row on the host roster:\n%s", out)
		}
		for _, want := range []string{"[d] disconnect", "[x] kick (new invite to return)", "[F4] restart server", "[h] stop hosting"} {
			if !strings.Contains(row, want) {
				t.Errorf("ask row missing %q: %q", want, row)
			}
		}
		if got := strings.TrimSpace(strings.Trim(strings.TrimSpace(ansi.Strip(row)), "│")); got != "asks first:  [d] disconnect  [x] kick (new invite to return)  [F4] restart server  [h] stop hosting" {
			t.Errorf("host ask row = %q", got)
		}
		if !strings.Contains(row, strings.SplitN(amber.Render("Q"), "Q", 2)[0]+"asks") {
			t.Errorf("ask row is not amber: %q", row)
		}
		// The three keys left the grey rows.
		for _, ln := range strings.Split(out, "\n") {
			if ln == row {
				continue
			}
			for _, k := range []string{"[d] disconnect", "[x] kick (new invite to return)", "[F4] restart server", "[h] stop hosting"} {
				if strings.Contains(ln, k) {
					t.Errorf("%q still on a grey row: %q", k, ln)
				}
			}
		}
		if !strings.Contains(out, "[t] target vessel") || !strings.Contains(out, "[tab] section") {
			t.Errorf("the other keys must stay on the grey rows:\n%s", out)
		}
	})

	t.Run("admin sees x and F4 but not h", func(t *testing.T) {
		w := sessionWorld(t, false)
		w.Session.CanAdminister = true
		row := askRow(NewSessionScreen(th).Render(w, 140, 0))
		if got := strings.TrimSpace(strings.Trim(strings.TrimSpace(ansi.Strip(row)), "│")); got != "asks first:  [d] disconnect  [x] kick (new invite to return)  [F4] restart server" {
			t.Errorf("admin ask row = %q", got)
		}
		if strings.Contains(row, "stop hosting") {
			t.Errorf("admin ask row offers stop hosting: %q", row)
		}
	})

	t.Run("guest sees no ask row", func(t *testing.T) {
		out := NewSessionScreen(th).Render(sessionWorld(t, false), 140, 0)
		if row := askRow(out); row != "" {
			t.Errorf("guest sees an ask row: %q", row)
		}
	})

	t.Run("solo start hosting is not an ask", func(t *testing.T) {
		w, err := sim.NewWorld()
		if err != nil {
			t.Fatal(err)
		}
		if row := askRow(NewSessionScreen(th).Render(w, 140, 0)); row != "" {
			t.Errorf("solo roster shows an ask row: %q", row)
		}
	})
}
