package serve

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jasonfen/terminal-space-program/internal/sessiondir"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/tui"
	"github.com/jasonfen/terminal-space-program/internal/tui/screens"
)

// ADR 0035 S2: chat intent travels App → serve wrapper as a message
// (SessionAdminMsg precedent) — the screen never touches shared state.

func newHostModel(t *testing.T, srv *Server) (tea.Model, *tui.App) {
	t.Helper()
	hostApp, err := tui.New(nil)
	if err != nil {
		t.Fatalf("tui.New: %v", err)
	}
	return srv.HostModel(hostApp), hostApp
}

func TestChatSendPostsToRing(t *testing.T) {
	srv := newOfflineServer(t)
	m, _ := newHostModel(t, srv)

	m, _ = m.Update(tui.ChatSendMsg{Text: "burning in 30"})
	_ = m

	lines := srv.chat.linesFor("SHA256:other")
	if len(lines) != 1 || lines[0].Text != "burning in 30" {
		t.Fatalf("broadcast must land in the ring; got %+v", lines)
	}
	if lines[0].Owner != sessiondir.HostFingerprint {
		t.Fatalf("owner must be the sender's fingerprint; got %q", lines[0].Owner)
	}
	// The handle renders — it must be the roster handle, not the raw
	// fingerprint.
	if lines[0].Handle == "" || lines[0].Handle == sessiondir.HostFingerprint {
		t.Fatalf("handle must resolve through the roster; got %q", lines[0].Handle)
	}
}

func TestChatSendDMToOfflineHandleRefused(t *testing.T) {
	srv := newOfflineServer(t)
	enrollDirect(t, srv, "SHA256:gern", "gern") // enrolled but NOT online
	m, _ := newHostModel(t, srv)

	m, _ = m.Update(tui.ChatSendMsg{Text: "you there?", To: "SHA256:gern", ToHandle: "gern"})
	_ = m

	if lines := srv.chat.linesFor("SHA256:gern"); len(lines) != 0 {
		t.Fatalf("a DM to an offline player must be refused at the handler (defense in depth); got %+v", lines)
	}
}

func TestChatSendDMToOnlinePlayerLands(t *testing.T) {
	srv := newOfflineServer(t)
	enrollDirect(t, srv, "SHA256:gern", "gern")
	srv.presence.markOnline("SHA256:gern")
	m, _ := newHostModel(t, srv)

	m, _ = m.Update(tui.ChatSendMsg{Text: "hold your burn", To: "SHA256:gern", ToHandle: "gern"})
	_ = m

	if lines := srv.chat.linesFor("SHA256:gern"); len(lines) != 1 {
		t.Fatalf("a DM to an online player must land; got %+v", lines)
	}
	if lines := srv.chat.linesFor("SHA256:third"); len(lines) != 0 {
		t.Fatalf("a DM must stay private; got %+v", lines)
	}
}

func TestChatSendEmptyTextDropped(t *testing.T) {
	srv := newOfflineServer(t)
	m, _ := newHostModel(t, srv)

	m, _ = m.Update(tui.ChatSendMsg{Text: "   "})
	_ = m

	if lines := srv.chat.linesFor("SHA256:other"); len(lines) != 0 {
		t.Fatalf("blank messages must not post; got %+v", lines)
	}
}

func TestChatSendSoloInert(t *testing.T) {
	// Without a server the wrapper is inert — chat is unavailable in
	// single-player like the rest of SessionInfo. Must not panic.
	app, err := tui.New(nil)
	if err != nil {
		t.Fatalf("tui.New: %v", err)
	}
	var m tea.Model = reportingModel{inner: app, app: app}
	m, _ = m.Update(tui.ChatSendMsg{Text: "anyone?"})
	_ = m
}

func TestChatLinesReachWorldSlate(t *testing.T) {
	srv := newOfflineServer(t)
	enrollDirect(t, srv, "SHA256:gern", "gern")
	m, hostApp := newHostModel(t, srv)
	m = tick(m)

	srv.chat.post("SHA256:gern", "gern", "", "", "node is 200m off")
	m, _ = m.Update(sim.TickMsg(time.Now()))
	_ = m

	w := hostApp.World()
	if len(w.ChatLines) != 1 || w.ChatLines[0].Text != "node is 200m off" {
		t.Fatalf("tick must feed the world's chat slate; got %+v", w.ChatLines)
	}
}

func TestStopHostingClearsChatSlate(t *testing.T) {
	srv := newOfflineServer(t)
	m, hostApp := newHostModel(t, srv)
	m = tick(m)

	srv.chat.post("SHA256:gern", "gern", "", "", "bye")
	m, _ = m.Update(sim.TickMsg(time.Now()))

	m, _ = m.Update(screens.SessionHostMsg{Start: false})
	_ = m
	if hostApp.World().ChatLines != nil {
		t.Fatalf("back to solo: the chat slate must clear with the session slates")
	}
}
