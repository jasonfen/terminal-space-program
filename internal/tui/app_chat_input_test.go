package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// ADR 0035 S3: the ~ chat input overlay. The sim keeps running while
// typing; every key is text or an editing action — nothing leaks to
// flight controls; @handle DMs tab-complete against the online roster.

func newChatApp(t *testing.T) *App {
	t.Helper()
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	a.world.Session = &sim.SessionInfo{
		Self: "SHA256:me",
		Players: []sim.SessionPlayer{
			{Fingerprint: "SHA256:me", Handle: "me", Online: true},
			{Fingerprint: "SHA256:gern", Handle: "gern", Online: true},
			{Fingerprint: "SHA256:gale", Handle: "gale", Online: true},
			{Fingerprint: "SHA256:zed", Handle: "zed", Online: false},
		},
	}
	return a
}

func chatPress(a *App, msg tea.KeyMsg) tea.Cmd {
	_, cmd := a.Update(msg)
	return cmd
}

func typeRunes(a *App, s string) {
	for _, r := range s {
		if r == ' ' {
			chatPress(a, tea.KeyMsg{Type: tea.KeySpace})
			continue
		}
		chatPress(a, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
}

func openChat(t *testing.T, a *App) {
	t.Helper()
	chatPress(a, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'~'}})
	if !a.capturingText() {
		t.Fatalf("~ must open the chat capture")
	}
}

// sendMsg runs the Cmd a send produced and returns the ChatSendMsg, or
// nil if no send happened.
func sendMsg(cmd tea.Cmd) *ChatSendMsg {
	if cmd == nil {
		return nil
	}
	if m, ok := cmd().(ChatSendMsg); ok {
		return &m
	}
	return nil
}

func TestChatTildeOpensAndCaptures(t *testing.T) {
	a := newChatApp(t)
	openChat(t, a)
	// Flight keys are text now — accepted cost (ADR 0035 §5). A typed
	// backtick must be a literal, not the boss shell (the v0.26 scar).
	chatPress(a, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'`'}})
	if a.active != screenOrbit {
		t.Fatalf("backtick while chatting must not open the boss shell")
	}
	typeRunes(a, "hi")
	if got := string(a.chatInput); got != "`hi" {
		t.Fatalf("input = %q, want literal backtick + hi", got)
	}
}

func TestChatSoloToastsInsteadOfOpening(t *testing.T) {
	a := newChatApp(t)
	a.world.Session = nil // solo
	chatPress(a, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'~'}})
	if a.capturingText() {
		t.Fatalf("chat must be unavailable in single-player")
	}
	if a.statusMsg == "" {
		t.Fatalf("a solo ~ must explain itself, not silently no-op (v0.30 lesson)")
	}
}

func TestChatEnterBroadcasts(t *testing.T) {
	a := newChatApp(t)
	openChat(t, a)
	typeRunes(a, "burning in 30")
	cmd := chatPress(a, tea.KeyMsg{Type: tea.KeyEnter})
	m := sendMsg(cmd)
	if m == nil || m.Text != "burning in 30" || m.To != "" {
		t.Fatalf("enter must emit a broadcast ChatSendMsg; got %+v", m)
	}
	if a.capturingText() {
		t.Fatalf("a sent line closes the overlay — back to flying")
	}
	if len(a.chatInput) != 0 {
		t.Fatalf("input must clear after send")
	}
}

func TestChatDMResolvesCaseInsensitive(t *testing.T) {
	a := newChatApp(t)
	openChat(t, a)
	typeRunes(a, "@GERN hold your burn")
	m := sendMsg(chatPress(a, tea.KeyMsg{Type: tea.KeyEnter}))
	if m == nil || m.To != "SHA256:gern" || m.ToHandle != "gern" {
		t.Fatalf("@GERN must resolve to gern's fingerprint; got %+v", m)
	}
	if m.Text != "hold your burn" {
		t.Fatalf("DM text must drop the @handle prefix; got %q", m.Text)
	}
}

func TestChatDMUnmatchedRefusedTextIntact(t *testing.T) {
	a := newChatApp(t)
	openChat(t, a)
	typeRunes(a, "@nobody you there")
	cmd := chatPress(a, tea.KeyMsg{Type: tea.KeyEnter})
	if sendMsg(cmd) != nil {
		t.Fatalf("an unmatched handle must refuse to send — never fall back to broadcast")
	}
	if !a.capturingText() || string(a.chatInput) != "@nobody you there" {
		t.Fatalf("refusal must leave the typed text intact to be fixed; input=%q open=%v",
			string(a.chatInput), a.capturingText())
	}
	if a.statusMsg == "" {
		t.Fatalf("refusal must toast")
	}
}

func TestChatDMOfflineRefused(t *testing.T) {
	a := newChatApp(t)
	openChat(t, a)
	typeRunes(a, "@zed hi")
	if sendMsg(chatPress(a, tea.KeyMsg{Type: tea.KeyEnter})) != nil {
		t.Fatalf("a DM to an offline player must refuse to send")
	}
}

func TestChatEscBailsAndClears(t *testing.T) {
	a := newChatApp(t)
	openChat(t, a)
	typeRunes(a, "half a thou")
	chatPress(a, tea.KeyMsg{Type: tea.KeyEscape})
	if a.capturingText() {
		t.Fatalf("esc must bail out")
	}
	openChat(t, a)
	if len(a.chatInput) != 0 {
		t.Fatalf("a bailed draft must not survive reopen; got %q", string(a.chatInput))
	}
}

func TestChatTabCompletesAndCycles(t *testing.T) {
	a := newChatApp(t)
	openChat(t, a)
	typeRunes(a, "@g")
	chatPress(a, tea.KeyMsg{Type: tea.KeyTab})
	first := string(a.chatInput)
	if first != "@gern" && first != "@gale" {
		t.Fatalf("tab must complete @g against the online roster; got %q", first)
	}
	chatPress(a, tea.KeyMsg{Type: tea.KeyTab})
	second := string(a.chatInput)
	if second == first || (second != "@gern" && second != "@gale") {
		t.Fatalf("repeated tab must cycle matches; got %q then %q", first, second)
	}
	chatPress(a, tea.KeyMsg{Type: tea.KeyTab})
	if got := string(a.chatInput); got != first {
		t.Fatalf("cycling must wrap; got %q, want %q", got, first)
	}
}

func TestChatTabIgnoresOfflineAndSelf(t *testing.T) {
	a := newChatApp(t)
	openChat(t, a)
	typeRunes(a, "@")
	seen := map[string]bool{}
	for i := 0; i < 6; i++ {
		chatPress(a, tea.KeyMsg{Type: tea.KeyTab})
		seen[string(a.chatInput)] = true
	}
	if seen["@zed"] {
		t.Fatalf("tab must not offer offline players")
	}
	if seen["@me"] {
		t.Fatalf("tab must not offer the sender themself")
	}
}

func TestChatInputRuneCap(t *testing.T) {
	a := newChatApp(t)
	openChat(t, a)
	typeRunes(a, strings.Repeat("x", 200))
	if n := len(a.chatInput); n != chatInputRuneCap {
		t.Fatalf("input must cap at %d runes; got %d", chatInputRuneCap, n)
	}
}
