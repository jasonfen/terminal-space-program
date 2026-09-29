package tui

import (
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/settings"
)

// cycleEmptyReadings walks Tidy (default) -> Compact -> Full -> Tidy and
// each step is persisted to settings.json and read back by a fresh Load.
func TestCycleEmptyReadingsPersists(t *testing.T) {
	testStateDirs(t)
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if got := a.orbitView.Settings().EmptyReadingsMode(); got != settings.EmptyTidy {
		t.Fatalf("default = %q, want tidy", got)
	}
	for _, want := range []settings.EmptyReadingsMode{settings.EmptyCompact, settings.EmptyFull, settings.EmptyTidy} {
		a.cycleEmptyReadings()
		if got := a.orbitView.Settings().EmptyReadingsMode(); got != want {
			t.Fatalf("in-memory after cycle = %q, want %q", got, want)
		}
		disk, _ := settings.Load()
		if got := disk.EmptyReadingsMode(); got != want {
			t.Fatalf("settings.json after cycle = %q, want %q", got, want)
		}
	}
}
