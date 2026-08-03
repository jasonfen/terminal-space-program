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

// MayAdminister is the capability predicate the serve handler gates on
// (v0.30 S1, #222): true for the host, false for guests and for
// fingerprints not in the roster.
func TestMayAdminister(t *testing.T) {
	s := openStore(t)
	if _, err := s.EnsureHost("jason"); err != nil {
		t.Fatalf("EnsureHost: %v", err)
	}
	inv, err := s.MintInvite("dave")
	if err != nil {
		t.Fatalf("MintInvite: %v", err)
	}
	if _, err := s.Enroll(inv.Code, "SHA256:guest", "dave"); err != nil {
		t.Fatalf("Enroll: %v", err)
	}

	if !s.MayAdminister(HostFingerprint) {
		t.Error("host may not administer")
	}
	if s.MayAdminister("SHA256:guest") {
		t.Error("guest may administer")
	}
	if s.MayAdminister("SHA256:stranger") {
		t.Error("unenrolled fingerprint may administer")
	}
}

// PromoteAdmin/DemoteAdmin round-trip a guest through the admin role
// (v0.30 S2): the role persists, MayAdminister flips with it, delegation
// stays host-only, and the host's role is immutable.
func TestPromoteDemoteRole(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := s.EnsureHost("jason"); err != nil {
		t.Fatalf("EnsureHost: %v", err)
	}
	inv, _ := s.MintInvite("dave")
	if _, err := s.Enroll(inv.Code, "SHA256:dave", "dave"); err != nil {
		t.Fatalf("Enroll: %v", err)
	}

	// Promote → admin, may administer but may not delegate.
	if err := s.PromoteAdmin("SHA256:dave"); err != nil {
		t.Fatalf("PromoteAdmin: %v", err)
	}
	if !s.MayAdminister("SHA256:dave") {
		t.Error("promoted admin may not administer")
	}
	if s.MayDelegate("SHA256:dave") {
		t.Error("admin may delegate — escalation not single-rooted")
	}
	if !s.MayDelegate(HostFingerprint) {
		t.Error("host may not delegate")
	}
	// Idempotent grant.
	if err := s.PromoteAdmin("SHA256:dave"); err != nil {
		t.Errorf("re-promote not idempotent: %v", err)
	}

	// Persists across a reopen (role rides the roster string).
	s2, err := Open(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if p, _ := s2.FindPlayer("SHA256:dave"); p.Role != RoleAdmin {
		t.Errorf("admin role not persisted: role = %q", p.Role)
	}
	if m2, _ := s2.Meta(); m2.Version != MetaVersion {
		t.Errorf("promote bumped MetaVersion to %d (want additive, %d)", m2.Version, MetaVersion)
	}

	// Demote → guest.
	if err := s2.DemoteAdmin("SHA256:dave"); err != nil {
		t.Fatalf("DemoteAdmin: %v", err)
	}
	if s2.MayAdminister("SHA256:dave") {
		t.Error("demoted player still administers")
	}

	// Guards: host immutable, unknown fingerprint rejected.
	if err := s2.PromoteAdmin(HostFingerprint); err == nil {
		t.Error("promoting the host was allowed")
	}
	if err := s2.PromoteAdmin("SHA256:nobody"); !errors.Is(err, ErrNotEnrolled) {
		t.Errorf("promote unknown fingerprint: err = %v, want ErrNotEnrolled", err)
	}
}

// MayRemove encodes the actor×target guardrail matrix (v0.30 S3, #224):
// host removes guests and admins; an admin removes only guests; nobody
// removes the host or themselves; guests remove no one.
func TestMayRemoveGuardrails(t *testing.T) {
	s := openStore(t)
	if _, err := s.EnsureHost("jason"); err != nil {
		t.Fatalf("EnsureHost: %v", err)
	}
	// adminA, adminB (promoted), guestA, guestB.
	for _, e := range []struct{ fp, handle string }{
		{"SHA256:adminA", "adminA"}, {"SHA256:adminB", "adminB"},
		{"SHA256:guestA", "guestA"}, {"SHA256:guestB", "guestB"},
	} {
		inv, _ := s.MintInvite(e.handle)
		if _, err := s.Enroll(inv.Code, e.fp, e.handle); err != nil {
			t.Fatalf("Enroll %s: %v", e.handle, err)
		}
	}
	if err := s.PromoteAdmin("SHA256:adminA"); err != nil {
		t.Fatalf("promote adminA: %v", err)
	}
	if err := s.PromoteAdmin("SHA256:adminB"); err != nil {
		t.Fatalf("promote adminB: %v", err)
	}

	cases := []struct {
		name          string
		actor, target string
		want          bool
	}{
		{"host removes guest", HostFingerprint, "SHA256:guestA", true},
		{"host removes admin", HostFingerprint, "SHA256:adminA", true},
		{"host removes self", HostFingerprint, HostFingerprint, false},
		{"admin removes guest", "SHA256:adminA", "SHA256:guestA", true},
		{"admin removes another admin", "SHA256:adminA", "SHA256:adminB", false},
		{"admin removes self", "SHA256:adminA", "SHA256:adminA", false},
		{"admin removes host", "SHA256:adminA", HostFingerprint, false},
		{"guest removes guest", "SHA256:guestA", "SHA256:guestB", false},
		{"guest removes admin", "SHA256:guestA", "SHA256:adminA", false},
		{"actor not enrolled", "SHA256:nobody", "SHA256:guestA", false},
		{"target not enrolled", HostFingerprint, "SHA256:nobody", false},
	}
	for _, c := range cases {
		if got := s.MayRemove(c.actor, c.target); got != c.want {
			t.Errorf("%s: MayRemove(%s, %s) = %v, want %v", c.name, c.actor, c.target, got, c.want)
		}
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

// TestMigrateV1SessionForward — a live v0.27 session.json (schema v1:
// roster + invites, no docks) must survive into v0.28. Writing a raw v1
// file and reading it back migrates to the current MetaVersion with the
// roster intact and an empty Docks; a re-write persists at v2. This is the
// load-bearing "sessions migrate, never break" guarantee (ADR 0034 addendum).
func TestMigrateV1SessionForward(t *testing.T) {
	dir := t.TempDir()
	// Hand-write a v0.27-shaped session.json (Version 1, no docks field).
	v1 := `{
  "version": 1,
  "body_catalog_hash": "deadbeef",
  "roster": [
    {"fingerprint": "local", "handle": "jason", "role": "host", "calibrated": true},
    {"fingerprint": "SHA256:gern", "handle": "gern", "role": "guest", "calibrated": true}
  ],
  "invites": [{"code": "AB2C-DE3F", "handle": "ada"}]
}`
	if err := os.MkdirAll(dir+"/players", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir+"/session.json", []byte(v1), 0o644); err != nil {
		t.Fatal(err)
	}

	s, err := Open(dir) // Open must not re-init over the existing file
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	m, err := s.Meta()
	if err != nil {
		t.Fatalf("Meta: %v", err)
	}
	if m.Version != MetaVersion {
		t.Errorf("migrated Version = %d, want %d", m.Version, MetaVersion)
	}
	if len(m.Roster) != 2 || m.Roster[1].Handle != "gern" {
		t.Errorf("roster not carried forward: %+v", m.Roster)
	}
	if len(m.Invites) != 1 || m.Invites[0].Code != "AB2C-DE3F" {
		t.Errorf("invites not carried forward: %+v", m.Invites)
	}
	if len(m.Docks) != 0 {
		t.Errorf("migrated v1 session gained docks: %+v", m.Docks)
	}
	if m.BodyCatalogHash != "deadbeef" {
		t.Errorf("catalog hash changed on migrate: %q", m.BodyCatalogHash)
	}

	// A dock cross-ref persists and round-trips at v2.
	links := []DockLink{{
		ID: 1, Owner: "local", OwnerHandle: "jason", DockerCraftID: 1,
		CompositeID: 1, GuestOwner: "SHA256:gern", GuestHandle: "gern",
		GuestCraftID: 200, Phase: 1,
	}}
	if err := s.SetDocks(links); err != nil {
		t.Fatalf("SetDocks: %v", err)
	}
	m2, err := s.Meta()
	if err != nil {
		t.Fatalf("Meta after SetDocks: %v", err)
	}
	if len(m2.Docks) != 1 || m2.Docks[0].GuestCraftID != 200 || m2.Docks[0].Phase != 1 {
		t.Errorf("dock cross-ref not persisted: %+v", m2.Docks)
	}

	// The on-disk file is now stamped v2.
	data, _ := os.ReadFile(dir + "/session.json")
	var probe struct {
		Version int `json:"version"`
	}
	_ = json.Unmarshal(data, &probe)
	if probe.Version != MetaVersion {
		t.Errorf("on-disk version = %d, want %d", probe.Version, MetaVersion)
	}
}

// #274: MintInvite refuses a handle that collides case-insensitively
// with an existing roster entry, and the store is left untouched.
func TestMintInviteRefusesCaseInsensitiveRosterCollision(t *testing.T) {
	s := openStore(t)
	inv1, err := s.MintInvite("gern")
	if err != nil {
		t.Fatalf("MintInvite gern: %v", err)
	}
	if _, err := s.Enroll(inv1.Code, "SHA256:gern", "gern"); err != nil {
		t.Fatalf("Enroll gern: %v", err)
	}
	if _, err := s.MintInvite("Gern"); err == nil {
		t.Fatal("MintInvite accepted a case-different collision with an enrolled handle")
	}
	m, _ := s.Meta()
	if len(m.Invites) != 0 {
		t.Errorf("a refused mint left an invite behind: %+v", m.Invites)
	}
}

// #274: MintInvite also refuses a collision against another
// outstanding (unredeemed) invite's pre-bound handle — catching the
// clash at the host's mint step, before a second player is ever
// handed a code that can't enroll.
func TestMintInviteRefusesCaseInsensitiveInviteCollision(t *testing.T) {
	s := openStore(t)
	if _, err := s.MintInvite("dave"); err != nil {
		t.Fatalf("first MintInvite: %v", err)
	}
	if _, err := s.MintInvite("DAVE"); err == nil {
		t.Fatal("MintInvite accepted a case-different collision with an outstanding invite")
	}
	m, _ := s.Meta()
	if len(m.Invites) != 1 {
		t.Errorf("a refused mint left a second invite behind: %+v", m.Invites)
	}
}

// #274: Enroll refuses a (possibly hand-edited) handle that collides
// case-insensitively with an already-enrolled roster entry, and the
// invite code is NOT spent — the player can retry with a different
// handle using the same code.
func TestEnrollRefusesCaseInsensitiveRosterCollision(t *testing.T) {
	s := openStore(t)
	inv1, err := s.MintInvite("gern")
	if err != nil {
		t.Fatalf("MintInvite gern: %v", err)
	}
	if _, err := s.Enroll(inv1.Code, "SHA256:gern", "gern"); err != nil {
		t.Fatalf("Enroll gern: %v", err)
	}
	inv2, err := s.MintInvite("someone-new")
	if err != nil {
		t.Fatalf("MintInvite someone-new: %v", err)
	}
	if _, err := s.Enroll(inv2.Code, "SHA256:other", "GERN"); err == nil {
		t.Fatal("Enroll accepted a case-different collision with an enrolled handle")
	}
	m, _ := s.Meta()
	if len(m.Roster) != 1 {
		t.Fatalf("a refused enroll added a roster row: %+v", m.Roster)
	}
	// The code survives the refusal — Peek still finds it.
	if _, err := s.Peek(inv2.Code); err != nil {
		t.Errorf("refused enroll spent the invite code: %v", err)
	}
}

// #274: a pre-fix roster already carrying a case-insensitive
// collision (planted directly, bypassing the now-guarded Enroll, to
// simulate data written before this fix shipped) is deterministically
// repaired at Open: the later enrollee (by roster/enrollment order)
// is renamed with a numeric suffix, and both the renamed player and
// the collision counterpart who kept their name get a pending session
// note.
func TestOpenDedupesLegacyRosterHandleCollision(t *testing.T) {
	dir := t.TempDir()
	seed, err := Open(dir)
	if err != nil {
		t.Fatalf("Open (seed): %v", err)
	}
	m, err := seed.Meta()
	if err != nil {
		t.Fatalf("Meta: %v", err)
	}
	m.Roster = []Player{
		{Fingerprint: HostFingerprint, Handle: "jason", Role: RoleHost},
		{Fingerprint: "SHA256:first", Handle: "gern", Role: RoleGuest},
		{Fingerprint: "SHA256:second", Handle: "gern", Role: RoleGuest},
	}
	if err := seed.writeMeta(m); err != nil {
		t.Fatalf("seed writeMeta: %v", err)
	}

	// Re-open: this is the "load" that must repair the legacy collision.
	s, err := Open(dir)
	if err != nil {
		t.Fatalf("Open (repair): %v", err)
	}
	m2, err := s.Meta()
	if err != nil {
		t.Fatalf("Meta after repair: %v", err)
	}
	if len(m2.Roster) != 3 {
		t.Fatalf("roster rows = %d, want 3", len(m2.Roster))
	}
	byFP := map[string]Player{}
	for _, p := range m2.Roster {
		byFP[p.Fingerprint] = p
	}
	if byFP["SHA256:first"].Handle != "gern" {
		t.Errorf("first (earlier) enrollee's handle changed: %+v", byFP["SHA256:first"])
	}
	if byFP["SHA256:second"].Handle != "gern2" {
		t.Errorf("later enrollee not renamed to gern2: %+v", byFP["SHA256:second"])
	}
	if byFP["SHA256:first"].PendingNote == "" {
		t.Error("collision counterpart (kept name) has no pending session note")
	}
	if byFP["SHA256:second"].PendingNote == "" {
		t.Error("renamed player has no pending session note")
	}

	// Idempotent across repeated loads: reopening again must not
	// compound the suffix (gern2 -> gern22).
	s2, err := Open(dir)
	if err != nil {
		t.Fatalf("Open (second load): %v", err)
	}
	m3, err := s2.Meta()
	if err != nil {
		t.Fatalf("Meta after second load: %v", err)
	}
	byFP3 := map[string]Player{}
	for _, p := range m3.Roster {
		byFP3[p.Fingerprint] = p
	}
	if byFP3["SHA256:second"].Handle != "gern2" {
		t.Errorf("second load compounded the rename: %+v", byFP3["SHA256:second"])
	}
}

// A legacy collision that differs only by case ("gern" vs "Gern")
// keeps the later entrant's OWN typed case in the rename — only a
// disambiguating numeric suffix is added, the case they chose is
// never silently rewritten.
func TestOpenDedupePreservesLaterEntrantCase(t *testing.T) {
	dir := t.TempDir()
	seed, err := Open(dir)
	if err != nil {
		t.Fatalf("Open (seed): %v", err)
	}
	m, err := seed.Meta()
	if err != nil {
		t.Fatalf("Meta: %v", err)
	}
	m.Roster = []Player{
		{Fingerprint: HostFingerprint, Handle: "jason", Role: RoleHost},
		{Fingerprint: "SHA256:first", Handle: "gern", Role: RoleGuest},
		{Fingerprint: "SHA256:second", Handle: "Gern", Role: RoleGuest},
	}
	if err := seed.writeMeta(m); err != nil {
		t.Fatalf("seed writeMeta: %v", err)
	}
	s, err := Open(dir)
	if err != nil {
		t.Fatalf("Open (repair): %v", err)
	}
	m2, _ := s.Meta()
	for _, p := range m2.Roster {
		if p.Fingerprint == "SHA256:second" && p.Handle != "Gern2" {
			t.Errorf("later entrant's case not preserved: got %q, want %q", p.Handle, "Gern2")
		}
	}
}

// ConsumePendingNote returns a player's pending note exactly once,
// then clears it — a second consume is silent (empty, no error).
func TestConsumePendingNoteFiresOnce(t *testing.T) {
	s := openStore(t)
	inv, err := s.MintInvite("dave")
	if err != nil {
		t.Fatalf("MintInvite: %v", err)
	}
	if _, err := s.Enroll(inv.Code, "SHA256:dave", "dave"); err != nil {
		t.Fatalf("Enroll: %v", err)
	}
	m, _ := s.Meta()
	for i := range m.Roster {
		if m.Roster[i].Fingerprint == "SHA256:dave" {
			m.Roster[i].PendingNote = "test note"
		}
	}
	if err := s.writeMeta(m); err != nil {
		t.Fatalf("writeMeta: %v", err)
	}
	note, err := s.ConsumePendingNote("SHA256:dave")
	if err != nil || note != "test note" {
		t.Fatalf("first consume = %q, %v; want %q, nil", note, err, "test note")
	}
	note2, err := s.ConsumePendingNote("SHA256:dave")
	if err != nil || note2 != "" {
		t.Fatalf("second consume = %q, %v; want empty, nil", note2, err)
	}
}

// #329: dedupeRosterHandles must not displace an innocent third party.
// Roster [gern(A), gern(B), gern2(C)] processed in enrollment order:
// B collides with A and must NOT be handed C's already-taken "gern2" —
// C never collided with anyone and must keep their own handle. The
// old (buggy) algorithm seeded `seen` incrementally, so it let B take
// "gern2" (not yet "seen" at B's turn) and then renamed C out of their
// own handle when C's turn exposed the manufactured collision.
func TestOpenDedupeDoesNotDisplaceThirdParty(t *testing.T) {
	dir := t.TempDir()
	seed, err := Open(dir)
	if err != nil {
		t.Fatalf("Open (seed): %v", err)
	}
	m, err := seed.Meta()
	if err != nil {
		t.Fatalf("Meta: %v", err)
	}
	m.Roster = []Player{
		{Fingerprint: HostFingerprint, Handle: "jason", Role: RoleHost},
		{Fingerprint: "SHA256:A", Handle: "gern", Role: RoleGuest},
		{Fingerprint: "SHA256:B", Handle: "gern", Role: RoleGuest},
		{Fingerprint: "SHA256:C", Handle: "gern2", Role: RoleGuest},
	}
	if err := seed.writeMeta(m); err != nil {
		t.Fatalf("seed writeMeta: %v", err)
	}

	s, err := Open(dir)
	if err != nil {
		t.Fatalf("Open (repair): %v", err)
	}
	m2, err := s.Meta()
	if err != nil {
		t.Fatalf("Meta after repair: %v", err)
	}
	byFP := map[string]Player{}
	for _, p := range m2.Roster {
		byFP[p.Fingerprint] = p
	}
	if byFP["SHA256:C"].Handle != "gern2" {
		t.Fatalf("innocent third party displaced: %+v", byFP["SHA256:C"])
	}
	if byFP["SHA256:A"].Handle != "gern" {
		t.Errorf("earlier entrant's handle changed: %+v", byFP["SHA256:A"])
	}
	if byFP["SHA256:B"].Handle == "gern" || byFP["SHA256:B"].Handle == "gern2" {
		t.Errorf("later colliding entrant not disambiguated away from a taken handle: %+v", byFP["SHA256:B"])
	}
	if byFP["SHA256:C"].PendingNote != "" {
		t.Errorf("innocent third party got a pending note it shouldn't have: %+v", byFP["SHA256:C"])
	}

	// Idempotent across a second load: no further renames, no
	// suffix compounding (e.g. never "gern22").
	s2, err := Open(dir)
	if err != nil {
		t.Fatalf("Open (second load): %v", err)
	}
	m3, err := s2.Meta()
	if err != nil {
		t.Fatalf("Meta after second load: %v", err)
	}
	byFP3 := map[string]Player{}
	for _, p := range m3.Roster {
		byFP3[p.Fingerprint] = p
	}
	if byFP3["SHA256:B"].Handle != byFP["SHA256:B"].Handle {
		t.Errorf("second load changed B's handle again: %q -> %q", byFP["SHA256:B"].Handle, byFP3["SHA256:B"].Handle)
	}
	if byFP3["SHA256:C"].Handle != "gern2" {
		t.Errorf("second load displaced C: %+v", byFP3["SHA256:C"])
	}
}

// #329: the repair must also seed `seen` from outstanding invites, not
// just the roster — otherwise it can mint a roster rename directly
// onto a handle a live invite is already pre-bound to, permanently
// orphaning that invite (EnrollWithInvite's roster-collision guard
// would then refuse it forever).
func TestOpenDedupeAvoidsOutstandingInviteCollision(t *testing.T) {
	dir := t.TempDir()
	seed, err := Open(dir)
	if err != nil {
		t.Fatalf("Open (seed): %v", err)
	}
	m, err := seed.Meta()
	if err != nil {
		t.Fatalf("Meta: %v", err)
	}
	m.Roster = []Player{
		{Fingerprint: HostFingerprint, Handle: "jason", Role: RoleHost},
		{Fingerprint: "SHA256:A", Handle: "gern", Role: RoleGuest},
		{Fingerprint: "SHA256:B", Handle: "gern", Role: RoleGuest},
	}
	m.Invites = []Invite{
		{Code: "AAAA-BBBB", Handle: "gern2"},
	}
	if err := seed.writeMeta(m); err != nil {
		t.Fatalf("seed writeMeta: %v", err)
	}

	s, err := Open(dir)
	if err != nil {
		t.Fatalf("Open (repair): %v", err)
	}
	m2, err := s.Meta()
	if err != nil {
		t.Fatalf("Meta after repair: %v", err)
	}
	for _, p := range m2.Roster {
		if p.Fingerprint == "SHA256:B" && foldHandle(p.Handle) == "gern2" {
			t.Fatalf("rename landed on a handle an outstanding invite is pre-bound to: %+v", p)
		}
	}
}

// #329: PendingNote must APPEND rather than assign, so a player who
// accumulates two separate facts across two separate repair passes
// (an unconsumed "you were renamed" note from one pass, then a
// "someone else collided with your name" note from a later pass, once
// more legacy junk data collides with the handle they were renamed
// to) learns both — not just the last one written.
func TestPendingNoteAppendsAcrossRepeatedRepairs(t *testing.T) {
	dir := t.TempDir()
	seed, err := Open(dir)
	if err != nil {
		t.Fatalf("Open (seed): %v", err)
	}
	m, err := seed.Meta()
	if err != nil {
		t.Fatalf("Meta: %v", err)
	}
	// Round 1: a plain two-way legacy collision. "second" gets renamed
	// to "gern2" and gets a PendingNote; "first" (kept name) gets a
	// counterpart PendingNote.
	m.Roster = []Player{
		{Fingerprint: HostFingerprint, Handle: "jason", Role: RoleHost},
		{Fingerprint: "SHA256:first", Handle: "gern", Role: RoleGuest},
		{Fingerprint: "SHA256:second", Handle: "gern", Role: RoleGuest},
	}
	if err := seed.writeMeta(m); err != nil {
		t.Fatalf("seed writeMeta: %v", err)
	}
	s, err := Open(dir)
	if err != nil {
		t.Fatalf("Open (round 1 repair): %v", err)
	}
	m1, err := s.Meta()
	if err != nil {
		t.Fatalf("Meta after round 1: %v", err)
	}
	var secondNoteRound1 string
	for _, p := range m1.Roster {
		if p.Fingerprint == "SHA256:second" {
			if p.Handle != "gern2" {
				t.Fatalf("round 1: second not renamed to gern2: %+v", p)
			}
			secondNoteRound1 = p.PendingNote
			if secondNoteRound1 == "" {
				t.Fatal("round 1: second has no pending note")
			}
		}
	}

	// Round 2: without "second" ever consuming their note, more legacy
	// junk data (simulating an out-of-band edit / further pre-existing
	// data) introduces a THIRD entry that collides with "second"'s new
	// handle "gern2". "second" is the earlier entrant this round, so
	// it keeps "gern2" and becomes the collision COUNTERPART for the
	// new entry's rename — while still holding its round-1 unconsumed
	// note.
	m1.Roster = append(m1.Roster, Player{Fingerprint: "SHA256:third", Handle: "gern2", Role: RoleGuest})
	if err := s.writeMeta(m1); err != nil {
		t.Fatalf("seed round 2 writeMeta: %v", err)
	}
	s2, err := Open(dir)
	if err != nil {
		t.Fatalf("Open (round 2 repair): %v", err)
	}
	m2, err := s2.Meta()
	if err != nil {
		t.Fatalf("Meta after round 2: %v", err)
	}
	var secondNoteRound2 string
	for _, p := range m2.Roster {
		if p.Fingerprint == "SHA256:second" {
			if p.Handle != "gern2" {
				t.Fatalf("round 2: second's handle changed unexpectedly: %+v", p)
			}
			secondNoteRound2 = p.PendingNote
		}
		if p.Fingerprint == "SHA256:third" && p.Handle == "gern2" {
			t.Fatalf("round 2: third not disambiguated away from second's handle: %+v", p)
		}
	}
	if !strings.Contains(secondNoteRound2, secondNoteRound1) {
		t.Errorf("round 2 note lost the round 1 fact: round1=%q round2=%q", secondNoteRound1, secondNoteRound2)
	}
	if secondNoteRound2 == secondNoteRound1 {
		t.Errorf("round 2 note didn't append the new counterpart fact: %q", secondNoteRound2)
	}

	// A third load with nothing new to repair must not touch the note
	// again (idempotent).
	s3, err := Open(dir)
	if err != nil {
		t.Fatalf("Open (round 3, no-op): %v", err)
	}
	m3, err := s3.Meta()
	if err != nil {
		t.Fatalf("Meta after round 3: %v", err)
	}
	for _, p := range m3.Roster {
		if p.Fingerprint == "SHA256:second" && p.PendingNote != secondNoteRound2 {
			t.Errorf("a no-op load changed second's note: %q -> %q", secondNoteRound2, p.PendingNote)
		}
	}
}

// #332: Enroll must refuse a handle that case-insensitively collides
// with an OUTSTANDING INVITE, not just the roster — matching
// MintInvite's own check. Without this, a host mints "gern" for Bob;
// before Bob redeems it, Alice enrolls (on an unrelated code) as
// "Gern"; Bob's code is now permanently dead — the exact failure the
// mint-side check exists to prevent, arriving through the enroll
// door instead.
func TestEnrollRefusesCaseInsensitiveInviteCollision(t *testing.T) {
	s := openStore(t)
	bobInvite, err := s.MintInvite("gern")
	if err != nil {
		t.Fatalf("MintInvite gern (bob): %v", err)
	}
	aliceInvite, err := s.MintInvite("someone-else")
	if err != nil {
		t.Fatalf("MintInvite someone-else (alice): %v", err)
	}
	if _, err := s.Enroll(aliceInvite.Code, "SHA256:alice", "GERN"); err == nil {
		t.Fatal("Enroll accepted a handle colliding with another outstanding invite")
	}
	// Both invites must survive the refusal: Alice's because a refused
	// enroll never spends the code, Bob's because it was never
	// touched.
	if _, err := s.Peek(aliceInvite.Code); err != nil {
		t.Errorf("refused enroll spent alice's invite code: %v", err)
	}
	if _, err := s.Peek(bobInvite.Code); err != nil {
		t.Errorf("bob's invite code was disturbed by alice's refused enroll: %v", err)
	}
	// Bob can still redeem his own code for "gern" cleanly afterward.
	if _, err := s.Enroll(bobInvite.Code, "SHA256:bob", "gern"); err != nil {
		t.Fatalf("bob's own enroll for his own pre-bound handle failed: %v", err)
	}
}

// #332 regression guard: Enroll must NOT refuse a player redeeming
// their OWN invite with its own pre-bound handle (the ordinary,
// overwhelmingly common path) just because that invite is still
// sitting in m.Invites at the moment of the check — the collision
// check must exclude the invite being redeemed from the invite set it
// compares against.
func TestEnrollAcceptsOwnPreboundHandle(t *testing.T) {
	s := openStore(t)
	inv, err := s.MintInvite("dave")
	if err != nil {
		t.Fatalf("MintInvite: %v", err)
	}
	if _, err := s.Enroll(inv.Code, "SHA256:dave", "dave"); err != nil {
		t.Fatalf("Enroll with own pre-bound handle refused: %v", err)
	}
}
