package sessiondir

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"strings"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/save"
	"github.com/jasonfen/terminal-space-program/internal/sim"
)

func openStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return s
}

// Open initialises a versioned session.json stamped with the current
// catalog hash; a reopen reads it back unchanged.
func TestMetaRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	m, err := s.Meta()
	if err != nil {
		t.Fatalf("Meta: %v", err)
	}
	if m.Version != MetaVersion {
		t.Errorf("Version = %d, want %d", m.Version, MetaVersion)
	}
	if m.BodyCatalogHash == "" {
		t.Error("catalog hash not stamped at init")
	}

	if _, err := s.EnsureHost("jason"); err != nil {
		t.Fatalf("EnsureHost: %v", err)
	}
	inv, err := s.MintInvite("dave")
	if err != nil {
		t.Fatalf("MintInvite: %v", err)
	}

	s2, err := Open(dir) // reopen — everything persisted
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	m2, err := s2.Meta()
	if err != nil {
		t.Fatalf("Meta after reopen: %v", err)
	}
	if len(m2.Roster) != 1 || m2.Roster[0].Role != RoleHost || m2.Roster[0].Handle != "jason" {
		t.Errorf("host entry not persisted: %+v", m2.Roster)
	}
	if len(m2.Invites) != 1 || m2.Invites[0].Code != inv.Code || m2.Invites[0].Handle != "dave" {
		t.Errorf("invite not persisted: %+v", m2.Invites)
	}
}

// EnsureHost is idempotent and keeps the host as roster entry #1.
func TestEnsureHostIdempotent(t *testing.T) {
	s := openStore(t)
	h1, err := s.EnsureHost("jason")
	if err != nil {
		t.Fatalf("EnsureHost: %v", err)
	}
	h2, err := s.EnsureHost("someone-else")
	if err != nil {
		t.Fatalf("EnsureHost again: %v", err)
	}
	if h2.Handle != h1.Handle {
		t.Errorf("second EnsureHost replaced the host: %q → %q", h1.Handle, h2.Handle)
	}
	m, _ := s.Meta()
	if len(m.Roster) != 1 {
		t.Errorf("roster grew on idempotent EnsureHost: %d entries", len(m.Roster))
	}
}

// Invite codes are one-time: enroll consumes; a second enroll (or a
// bogus code) is rejected. Peek validates without consuming, and the
// handle is editable at enroll.
func TestInviteLifecycle(t *testing.T) {
	s := openStore(t)
	inv, err := s.MintInvite("dave")
	if err != nil {
		t.Fatalf("MintInvite: %v", err)
	}
	if !strings.Contains(inv.Code, "-") || len(inv.Code) != 9 {
		t.Errorf("unexpected code shape %q", inv.Code)
	}

	if _, err := s.Peek(inv.Code); err != nil {
		t.Errorf("Peek minted code: %v", err)
	}
	if _, err := s.Peek(inv.Code); err != nil {
		t.Errorf("Peek must not consume: %v", err)
	}
	if _, err := s.Peek("XXXX-XXXX"); !errors.Is(err, ErrUnknownInvite) {
		t.Errorf("Peek bogus code: err = %v, want ErrUnknownInvite", err)
	}

	// Case/dash-insensitive entry, handle edited at enroll.
	typed := strings.ToLower(strings.ReplaceAll(inv.Code, "-", ""))
	p, err := s.Enroll(typed, "SHA256:abc", "david")
	if err != nil {
		t.Fatalf("Enroll: %v", err)
	}
	if p.Handle != "david" || p.Role != RoleGuest || !p.Calibrated {
		t.Errorf("enrolled player = %+v", p)
	}

	if _, err := s.Enroll(inv.Code, "SHA256:other", "mallory"); !errors.Is(err, ErrUnknownInvite) {
		t.Errorf("second enroll of a spent code: err = %v, want ErrUnknownInvite", err)
	}

	got, err := s.FindPlayer("SHA256:abc")
	if err != nil || got.Handle != "david" {
		t.Errorf("FindPlayer = %+v, %v", got, err)
	}
	if _, err := s.FindPlayer("SHA256:stranger"); !errors.Is(err, ErrNotEnrolled) {
		t.Errorf("FindPlayer stranger: err = %v, want ErrNotEnrolled", err)
	}
}

// Review follow-up: a fingerprint that is already enrolled gets its
// existing identity back — no duplicate roster rows, and the second
// code is not spent.
func TestEnrollDedupesFingerprint(t *testing.T) {
	s := openStore(t)
	inv1, _ := s.MintInvite("dave")
	inv2, _ := s.MintInvite("dave-again")
	p1, err := s.Enroll(inv1.Code, "SHA256:dave", "dave")
	if err != nil {
		t.Fatalf("first Enroll: %v", err)
	}
	p2, err := s.Enroll(inv2.Code, "SHA256:dave", "someone-new")
	if err != nil {
		t.Fatalf("second Enroll: %v", err)
	}
	if p2.Handle != p1.Handle {
		t.Errorf("second enroll changed identity: %q -> %q", p1.Handle, p2.Handle)
	}
	m, _ := s.Meta()
	if len(m.Roster) != 1 {
		t.Errorf("roster rows = %d, want 1 (no duplicates per key)", len(m.Roster))
	}
	if _, err := s.Peek(inv2.Code); err != nil {
		t.Error("second code was spent by an idempotent re-enroll")
	}
}

// Revoke deletes an unredeemed code; removing a player drops the
// roster entry but keeps their payload (a re-invite resumes it). The
// host can't be removed.
func TestRevokeAndRemove(t *testing.T) {
	s := openStore(t)
	inv, err := s.MintInvite("dave")
	if err != nil {
		t.Fatalf("MintInvite: %v", err)
	}
	if err := s.RevokeInvite(inv.Code); err != nil {
		t.Fatalf("RevokeInvite: %v", err)
	}
	if _, err := s.Peek(inv.Code); !errors.Is(err, ErrUnknownInvite) {
		t.Error("revoked code still peekable")
	}
	if err := s.RevokeInvite(inv.Code); !errors.Is(err, ErrUnknownInvite) {
		t.Errorf("double revoke: %v", err)
	}

	inv2, _ := s.MintInvite("dave")
	if _, err := s.Enroll(inv2.Code, "SHA256:dave", "dave"); err != nil {
		t.Fatalf("Enroll: %v", err)
	}
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	if err := s.SavePlayer("SHA256:dave", w); err != nil {
		t.Fatalf("SavePlayer: %v", err)
	}
	if err := s.RemovePlayer("SHA256:dave"); err != nil {
		t.Fatalf("RemovePlayer: %v", err)
	}
	if _, err := s.FindPlayer("SHA256:dave"); !errors.Is(err, ErrNotEnrolled) {
		t.Error("removed player still enrolled")
	}
	if !s.HasPayload("SHA256:dave") {
		t.Error("removal deleted the payload; re-invites should resume")
	}
	if _, err := s.EnsureHost("jason"); err != nil {
		t.Fatal(err)
	}
	if err := s.RemovePlayer(HostFingerprint); err == nil {
		t.Error("host removal allowed")
	}
}

// Player payloads round-trip through the save envelope:
// the restored world carries the same craft and sim clock.
func TestPlayerPayloadRoundTrip(t *testing.T) {
	s := openStore(t)
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	const fp = "SHA256:abc"
	if s.HasPayload(fp) {
		t.Fatal("payload exists before save")
	}
	if err := s.SavePlayer(fp, w); err != nil {
		t.Fatalf("SavePlayer: %v", err)
	}
	if !s.HasPayload(fp) {
		t.Fatal("HasPayload false after save")
	}
	got, err := s.LoadPlayer(fp)
	if err != nil {
		t.Fatalf("LoadPlayer: %v", err)
	}
	if len(got.Crafts) != len(w.Crafts) {
		t.Errorf("craft count %d, want %d", len(got.Crafts), len(w.Crafts))
	}
	if !got.Clock.SimTime.Equal(w.Clock.SimTime) {
		t.Errorf("sim clock %v, want %v", got.Clock.SimTime, w.Clock.SimTime)
	}
	if len(got.Crafts) > 0 && got.Crafts[0].ID != w.Crafts[0].ID {
		t.Errorf("craft ID %d, want %d", got.Crafts[0].ID, w.Crafts[0].ID)
	}
}

// A missing payload surfaces fs.ErrNotExist (first session), and a
// payload written under a different body catalog is rejected at load
// — the connect path must refuse, never corrupt.
func TestLoadPlayerErrors(t *testing.T) {
	s := openStore(t)
	if _, err := s.LoadPlayer("SHA256:none"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("missing payload: err = %v, want fs.ErrNotExist", err)
	}

	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	const fp = "SHA256:stale"
	if err := s.SavePlayer(fp, w); err != nil {
		t.Fatalf("SavePlayer: %v", err)
	}
	// Corrupt the stored catalog hash to simulate a game upgraded
	// between sessions.
	path := s.payloadPath(fp)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read payload: %v", err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("parse payload: %v", err)
	}
	raw["body_catalog_hash"], _ = json.Marshal("not-the-real-hash")
	data, _ = json.Marshal(raw)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("rewrite payload: %v", err)
	}

	if _, err := s.LoadPlayer(fp); !errors.Is(err, save.ErrCatalogMismatch) {
		t.Errorf("stale catalog: err = %v, want save.ErrCatalogMismatch", err)
	}
}
