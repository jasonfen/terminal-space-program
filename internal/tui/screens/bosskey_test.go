package screens

import (
	"strings"
	"testing"
)

// typeLine feeds each rune of s through HandleKey as a single-rune key,
// mirroring how bubbletea delivers typed characters.
func typeLine(b *BossShell, s string) BossAction {
	var last BossAction
	for _, r := range s {
		last = b.HandleKey(string(r))
	}
	return last
}

func TestBossShellTypingBuildsLine(t *testing.T) {
	b := NewBossShell(Theme{})
	typeLine(b, "whoami")
	if b.input != "whoami" {
		t.Fatalf("input = %q, want %q", b.input, "whoami")
	}
	if act := b.HandleKey("enter"); act != BossActionNone {
		t.Fatalf("enter action = %v, want BossActionNone", act)
	}
	if b.input != "" {
		t.Fatalf("input not cleared after enter: %q", b.input)
	}
	last := b.scroll[len(b.scroll)-1]
	if last != "user" {
		t.Fatalf("whoami output = %q, want %q", last, "user")
	}
}

func TestBossShellBackspaceIsRuneSafe(t *testing.T) {
	b := NewBossShell(Theme{})
	typeLine(b, "héllo")
	b.HandleKey("backspace")
	if b.input != "héll" {
		t.Fatalf("after backspace input = %q, want %q", b.input, "héll")
	}
}

func TestBossShellNamedKeysIgnored(t *testing.T) {
	b := NewBossShell(Theme{})
	for _, k := range []string{"up", "tab", "ctrl+x", "f1", "left"} {
		if act := b.HandleKey(k); act != BossActionNone {
			t.Fatalf("key %q action = %v, want BossActionNone", k, act)
		}
	}
	if b.input != "" {
		t.Fatalf("named keys polluted input: %q", b.input)
	}
}

func TestBossShellExitVariants(t *testing.T) {
	for _, word := range []string{"exit", "logout"} {
		b := NewBossShell(Theme{})
		typeLine(b, word)
		if act := b.HandleKey("enter"); act != BossActionExit {
			t.Fatalf("%q action = %v, want BossActionExit", word, act)
		}
	}
	// ctrl+d logs out regardless of buffer contents.
	b := NewBossShell(Theme{})
	if act := b.HandleKey("ctrl+d"); act != BossActionExit {
		t.Fatalf("ctrl+d action = %v, want BossActionExit", act)
	}
}

func TestBossShellClearWipesScrollback(t *testing.T) {
	b := NewBossShell(Theme{})
	if len(b.scroll) == 0 {
		t.Fatal("expected lived-in initial scrollback")
	}
	typeLine(b, "clear")
	b.HandleKey("enter")
	if len(b.scroll) != 0 {
		t.Fatalf("clear left %d rows, want 0", len(b.scroll))
	}
}

func TestBossShellUnknownCommand(t *testing.T) {
	b := NewBossShell(Theme{})
	typeLine(b, "frobnicate")
	b.HandleKey("enter")
	last := b.scroll[len(b.scroll)-1]
	if last != "frobnicate: command not found" {
		t.Fatalf("unknown cmd output = %q", last)
	}
}

func TestBossShellResetRestoresBanner(t *testing.T) {
	b := NewBossShell(Theme{})
	typeLine(b, "clear")
	b.HandleKey("enter")
	b.Reset()
	if len(b.scroll) == 0 {
		t.Fatal("Reset did not restore lived-in scrollback")
	}
	if b.input != "" {
		t.Fatalf("Reset did not clear input: %q", b.input)
	}
}

func TestBossShellRenderFillsHeight(t *testing.T) {
	b := NewBossShell(Theme{})
	out := b.Render(80, 24)
	if got := strings.Count(out, "\n") + 1; got != 24 {
		t.Fatalf("render produced %d rows, want 24", got)
	}
	if !strings.Contains(out, "█") {
		t.Fatal("render missing block cursor")
	}
}
