package serve

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/sessiondir"
)

// #274: a legacy roster collision repaired at load (sessiondir's
// dedupeRosterHandles, pinned at that layer's own tests) must actually
// reach the affected players, not just rewrite session.json quietly.
// Both the renamed player and the collision counterpart who kept
// their name get a toast on their next connect.
func TestLegacyHandleRenameNoteReachesConnectingPlayers(t *testing.T) {
	sessionDir := t.TempDir()
	// Seed a pre-#274 session.json: two guest rows sharing "gern",
	// exactly the shape a roster written before this fix would have.
	seed := sessiondir.Meta{
		Version:         sessiondir.MetaVersion,
		BodyCatalogHash: "irrelevant-to-this-test",
		Roster: []sessiondir.Player{
			{Fingerprint: sessiondir.HostFingerprint, Handle: "jason", Role: sessiondir.RoleHost},
			{Fingerprint: "SHA256:first", Handle: "gern", Role: sessiondir.RoleGuest},
			{Fingerprint: "SHA256:second", Handle: "gern", Role: sessiondir.RoleGuest},
		},
	}
	data, err := json.MarshalIndent(seed, "", "  ")
	if err != nil {
		t.Fatalf("marshal seed: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(sessionDir, "players"), 0o755); err != nil {
		t.Fatalf("mkdir players: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sessionDir, "session.json"), data, 0o644); err != nil {
		t.Fatalf("seed session.json: %v", err)
	}

	t.Setenv("XDG_STATE_HOME", t.TempDir())
	srv, err := New(Config{
		Addr:        "127.0.0.1:0",
		HostKeyPath: filepath.Join(t.TempDir(), "hostkey"),
		SessionDir:  sessionDir,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = srv.ln.Close() })

	// The repair ran during New (via sessiondir.Open): the later
	// enrollee is renamed.
	p, err := srv.store.FindPlayer("SHA256:second")
	if err != nil || p.Handle != "gern2" {
		t.Fatalf("legacy roster not repaired by New/Open: %+v, %v", p, err)
	}

	renamedApp, err := srv.newGuestApp("SHA256:second")
	if err != nil {
		t.Fatalf("newGuestApp (renamed): %v", err)
	}
	renamed := tick(srv.withReporting(renamedApp, "SHA256:second"))
	out := stripANSI(renamed.View())
	if !strings.Contains(out, "gern2") {
		t.Errorf("renamed player's connect toast missing the new handle: %q", out)
	}

	keptApp, err := srv.newGuestApp("SHA256:first")
	if err != nil {
		t.Fatalf("newGuestApp (kept name): %v", err)
	}
	kept := tick(srv.withReporting(keptApp, "SHA256:first"))
	koutt := stripANSI(kept.View())
	if !strings.Contains(koutt, "gern2") {
		t.Errorf("collision counterpart's connect toast missing the new handle: %q", koutt)
	}
}
