package serve

import (
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jasonfen/terminal-space-program/internal/tui"
	"github.com/jasonfen/terminal-space-program/internal/tui/screens"
)

// Audit 15: a store write that fails must not be reported as done, and must
// leave a line in the server log. The failure is injected by making the
// directory the write lands in read-only (a stand-in for a full disk or a
// permissions mix-up after a restore).

type syncBuf struct {
	mu sync.Mutex
	b  strings.Builder
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// newServerIn is newOfflineServer with the session dir exposed.
func newServerIn(t *testing.T) (*Server, string) {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	dir := t.TempDir()
	srv, err := New(Config{Addr: "127.0.0.1:0", HostKeyPath: filepath.Join(t.TempDir(), "hostkey"), SessionDir: dir})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = srv.ln.Close() })
	return srv, dir
}

// captureLog redirects the standard logger into a buffer for the test. The
// positive control proves the capture sees a line before any test trusts an
// empty result.
func captureLog(t *testing.T) *syncBuf {
	t.Helper()
	buf := &syncBuf{}
	old := log.Writer()
	log.SetOutput(buf)
	t.Cleanup(func() { log.SetOutput(old) })
	log.Printf("capture-control")
	if !strings.Contains(buf.String(), "capture-control") {
		t.Fatal("log capture cannot see a line it just wrote")
	}
	return buf
}

// breakDir makes dir unwritable until the test ends. Skipped as root, which
// ignores the mode bits.
func breakDir(t *testing.T, dir string) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory modes")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	// Prove the injection bites before trusting what it breaks.
	if f, err := os.CreateTemp(dir, "probe"); err == nil {
		f.Close()
		t.Skip("directory is still writable; cannot inject a write failure here")
	}
}

// footer is what the host reads: the orbit screen's footer flash.
func footer(t *testing.T, m tea.Model) string {
	t.Helper()
	m, _ = m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	lines := strings.Split(stripANSI(m.View()), "\n")
	if len(lines) > 4 {
		lines = lines[len(lines)-4:]
	}
	return strings.Join(lines, "\n")
}

// The positive control for footer: a toast the host app raises is seen.
func TestFooterSeesAToast(t *testing.T) {
	f := startDropFixture(t)
	f.app.Toast("footer-control")
	if out := footer(t, f.host); !strings.Contains(out, "footer-control") {
		t.Fatalf("footer cannot see a toast; got:\n%s", out)
	}
}

type hostFixture struct {
	srv  *Server
	dir  string
	host tea.Model
	gfp  string
}

// newAdminFixture is an offline hosted server with one enrolled guest and the
// host's own wrapped model.
func newAdminFixture(t *testing.T) *hostFixture {
	t.Helper()
	srv, dir := newServerIn(t)
	enrollDirect(t, srv, "SHA256:pilot2", "pilot2")
	app, err := tui.New(nil)
	if err != nil {
		t.Fatalf("tui.New: %v", err)
	}
	return &hostFixture{srv: srv, dir: dir, host: srv.HostModel(app), gfp: "SHA256:pilot2"}
}

func (f *hostFixture) admin(cmd screens.SessionCommand) {
	f.host, _ = f.host.Update(screens.SessionAdminMsg{Cmd: cmd})
}

// breakMeta makes session.json unwritable (roster and invite edits).
func (f *hostFixture) breakMeta(t *testing.T) { breakDir(t, f.dir) }

func TestRevokeFailureIsToldToTheHostAndLogged(t *testing.T) {
	f := newAdminFixture(t)
	inv, err := f.srv.store.MintInvite("newbie")
	if err != nil {
		t.Fatal(err)
	}
	logs := captureLog(t)
	f.breakMeta(t)
	f.admin(screens.SessionCommand{Kind: screens.SessionCmdRevoke, Code: inv.Code})
	if out := footer(t, f.host); !strings.Contains(out, "revoke failed") {
		t.Errorf("host saw no revoke failure; footer:\n%s", out)
	}
	if !strings.Contains(logs.String(), "revoke") {
		t.Errorf("server log has no revoke line: %q", logs.String())
	}
}

func TestPromoteAndDemoteFailureAreToldToTheHostAndLogged(t *testing.T) {
	for _, tc := range []struct {
		kind screens.SessionCommandKind
		word string
	}{
		{screens.SessionCmdPromote, "promote"},
		{screens.SessionCmdDemote, "demote"},
	} {
		t.Run(tc.word, func(t *testing.T) {
			f := newAdminFixture(t)
			if tc.word == "demote" {
				if err := f.srv.store.PromoteAdmin(f.gfp); err != nil {
					t.Fatal(err)
				}
			}
			logs := captureLog(t)
			f.breakMeta(t)
			f.admin(screens.SessionCommand{Kind: tc.kind, Fingerprint: f.gfp, Handle: "pilot2"})
			if out := footer(t, f.host); !strings.Contains(out, tc.word+" pilot2 failed") {
				t.Errorf("host saw no %s failure; footer:\n%s", tc.word, out)
			}
			if !strings.Contains(logs.String(), tc.word) {
				t.Errorf("server log has no %s line: %q", tc.word, logs.String())
			}
		})
	}
}

// A kick whose roster edit fails must not read as a kick: the host is told it
// failed, the log has the error, and the player is neither dropped nor left
// with the kicked line while their row stays.
func TestKickRosterFailureIsNotReportedAsKicked(t *testing.T) {
	f := startDropFixture(t)
	logs := captureLog(t)
	breakDir(t, f.dir)
	f.admin(screens.SessionCommand{Kind: screens.SessionCmdKick, Fingerprint: f.gfp, Handle: "pilot2"})
	out := footer(t, f.host)
	if !strings.Contains(out, "kick of pilot2 failed") {
		t.Errorf("host saw no kick failure; footer:\n%s", out)
	}
	if strings.Contains(out, "kicked pilot2") {
		t.Errorf("host was told pilot2 was kicked; footer:\n%s", out)
	}
	if !strings.Contains(logs.String(), "kick") {
		t.Errorf("server log has no kick line: %q", logs.String())
	}
	select {
	case <-f.guest.exited:
		t.Error("the guest was dropped although the kick failed")
	case <-time.After(300 * time.Millisecond):
	}
	if _, err := f.srv.store.FindPlayer(f.gfp); err != nil {
		t.Errorf("roster row should be untouched: %v", err)
	}
}

// The final write at disconnect: a failure is logged by name with the error,
// and the line the dropped player is left with does not claim a save.
func TestDisconnectSaveFailureIsLoggedAndNotClaimedSaved(t *testing.T) {
	f := startDropFixture(t)
	logs := captureLog(t)
	breakDir(t, filepath.Join(f.dir, "players"))
	f.admin(screens.SessionCommand{Kind: screens.SessionCmdDisconnect, Fingerprint: f.gfp, Handle: "pilot2"})
	f.waitGone(t, 2*time.Second, "disconnect")
	if got := logs.String(); !strings.Contains(got, "pilot2") || !strings.Contains(got, "not saved") {
		t.Errorf("server log lacks the disconnect-save failure: %q", got)
	}
	if got := f.guest.stderr(); strings.Contains(got, "progress is saved") {
		t.Errorf("player was told their progress is saved although the write failed: %q", got)
	}
	if got := f.guest.stderr(); !strings.Contains(got, "not saved") {
		t.Errorf("player was not told the save failed: %q", got)
	}
}

// The guest sink (every guest autosave, and the quit-path write) logs a
// failure server-side; the error text, which names host paths, is not sent to
// the guest.
func TestGuestSinkFailureIsLogged(t *testing.T) {
	srv, dir := newServerIn(t)
	enrollDirect(t, srv, "SHA256:pilot2", "pilot2")
	app, err := srv.newGuestApp("SHA256:pilot2")
	if err != nil {
		t.Fatal(err)
	}
	logs := captureLog(t)
	breakDir(t, filepath.Join(dir, "players"))
	if err := app.PersistNow(); err == nil {
		t.Fatal("PersistNow succeeded into a read-only players/")
	}
	if got := logs.String(); !strings.Contains(got, "pilot2") || !strings.Contains(got, "not saved") {
		t.Errorf("server log lacks the guest save failure: %q", got)
	}
}

// Kick and delete vessels: the roster edit fails, so nothing is kicked and
// nothing is deleted. The session was already flagged as a purge (no final
// payload); it must end as a plain disconnect that still saves.
func TestPurgeKickRosterFailureDeletesNothingAndStillSaves(t *testing.T) {
	f := startDropFixture(t)
	_ = os.Remove(f.payloadFile())
	logs := captureLog(t)
	// session.json is unwritable but players/ stays writable.
	breakDir(t, f.dir)
	if err := os.Chmod(filepath.Join(f.dir, "players"), 0o700); err != nil {
		t.Fatal(err)
	}
	f.admin(screens.SessionCommand{Kind: screens.SessionCmdKick, Fingerprint: f.gfp, Handle: "pilot2", Purge: true})
	f.waitGone(t, 2*time.Second, "failed purge kick")
	out := footer(t, f.host)
	if !strings.Contains(out, "kick of pilot2 failed") || strings.Contains(out, "kicked pilot2") {
		t.Errorf("host footer:\n%s", out)
	}
	if !strings.Contains(logs.String(), "not removed from the roster") {
		t.Errorf("server log has no purge-kick line: %q", logs.String())
	}
	if !f.srv.store.HasPayload(f.gfp) {
		t.Error("the failed purge left the player without a saved program")
	}
	if got := f.guest.stderr(); strings.Contains(got, "kicked") {
		t.Errorf("player was told they were kicked: %q", got)
	}
}
