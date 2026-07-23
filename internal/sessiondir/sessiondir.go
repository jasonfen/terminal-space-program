// Package sessiondir is the server-side session directory for
// multiplayer (v0.27 S3, ADR 0034): a versioned session.json holding
// the player roster, outstanding invite codes, and the body-catalog
// hash, plus one save envelope per enrolled player (current save.SchemaVersion).
// The local single-player save.json and saves/ directory are never
// touched by anything here.
//
// The Store serialises in-process mutations behind a mutex — every
// ssh session lives in the host's process (the ssh-only MVP has no
// wire), so this is the whole concurrency story. A `serve invite`
// CLI run against a live server is a separate process and races
// last-write-wins; acceptable while sessions are friends-hosted.
package sessiondir

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/save"
	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// MetaVersion is the session.json schema version. Bump + migrate on
// shape changes, mirroring the save-envelope discipline.
//
// v1 (v0.27): roster + invites + catalog hash.
// v2 (v0.28 S5): adds Docks — the cross-player-dock cross-ref, so a
// guest riding another player's stack (whose craft therefore isn't in
// its own payload) resumes docked-as-guest on reconnect. A v1
// session.json migrates forward (Docks defaults empty) via
// migrateMetaV1ToV2; live v0.27 sessions never break.
const MetaVersion = 2

// Roster roles. Host is the session's root operator (ADR 0034): the
// player whose machine runs the session, with invite/removal authority
// and the sole power to delegate administration. Admin is a guest the
// host has promoted — it carries the invite/removal capability but not
// delegation (single-rooted escalation, v0.30 S2). Everyone else is a
// guest. The role is a plain persisted string on the roster entry, so
// adding admin is additive: no MetaVersion bump, no migration.
const (
	RoleHost  = "host"
	RoleAdmin = "admin"
	RoleGuest = "guest"
)

// HostFingerprint marks the host's roster entry: the host plays
// in-process over local stdio, so there is no ssh key to print.
const HostFingerprint = "local"

var (
	ErrUnknownInvite = errors.New("sessiondir: unknown or already-used invite code")
	ErrNotEnrolled   = errors.New("sessiondir: fingerprint not in roster")
)

// Player is one roster entry. Fingerprint is the ssh public-key
// SHA256 fingerprint (the stable identity — handles are editable).
type Player struct {
	Fingerprint string    `json:"fingerprint"`
	Handle      string    `json:"handle"`
	Role        string    `json:"role"`
	EnrolledAt  time.Time `json:"enrolled_at"`
	Calibrated  bool      `json:"calibrated"`
}

// Invite is one outstanding (unredeemed) invite code. The handle is
// pre-bound at mint and editable at enroll (ADR 0034 addendum).
type Invite struct {
	Code      string    `json:"code"`
	Handle    string    `json:"handle"`
	CreatedAt time.Time `json:"created_at"`
}

// DockLink is one cross-player dock's durable cross-ref (v0.28 S5, ADR
// 0034 §6). It is the persisted, serialisable subset of the live
// relay.DockRecord — enough for a reconnecting session to resume: the
// stack owner + composite, and the guest player + their craft riding in
// it. The transient in-flight payloads (the craft handoffs) are NOT
// persisted; a dock that was mid-handshake at shutdown resolves fresh.
// Phase is the relay.DockPhase int (0 pending / 1 active); sessiondir
// stays below relay so it carries the raw int rather than importing it.
type DockLink struct {
	ID            uint64 `json:"id"`
	Owner         string `json:"owner"`
	OwnerHandle   string `json:"owner_handle,omitempty"`
	DockerCraftID uint64 `json:"docker_craft_id"`
	CompositeID   uint64 `json:"composite_id,omitempty"`
	GuestOwner    string `json:"guest_owner"`
	GuestHandle   string `json:"guest_handle,omitempty"`
	GuestCraftID  uint64 `json:"guest_craft_id"`
	Phase         int    `json:"phase"`
}

// Meta is the session.json shape.
type Meta struct {
	Version         int      `json:"version"`
	BodyCatalogHash string   `json:"body_catalog_hash"`
	Roster          []Player `json:"roster"`
	Invites         []Invite `json:"invites"`
	// Docks is the cross-player-dock cross-ref (v2+, v0.28 S5). Empty
	// in a fresh or non-docking session; the serve layer syncs it from
	// the live relay ledger on change.
	Docks []DockLink `json:"docks,omitempty"`
}

// Store owns one session directory. All mutations re-read
// session.json under the lock, so a CLI mint between server reads is
// picked up on the next connect.
type Store struct {
	dir string
	mu  sync.Mutex
}

// DefaultDir is $XDG_STATE_HOME/terminal-space-program/session
// (falling back to ~/.local/state), sibling of save.json and saves/.
func DefaultDir() (string, error) {
	if x := os.Getenv("XDG_STATE_HOME"); x != "" {
		return filepath.Join(x, "terminal-space-program", "session"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	return filepath.Join(home, ".local", "state", "terminal-space-program", "session"), nil
}

// Open creates the directory if needed and initialises session.json
// on first use, stamping the current body-catalog hash.
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(filepath.Join(dir, "players"), 0o755); err != nil {
		return nil, fmt.Errorf("sessiondir: %w", err)
	}
	s := &Store{dir: dir}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := os.Stat(s.metaPath()); errors.Is(err, os.ErrNotExist) {
		hash, err := bodies.CatalogHash()
		if err != nil {
			return nil, fmt.Errorf("sessiondir: catalog hash: %w", err)
		}
		if err := s.writeMeta(Meta{Version: MetaVersion, BodyCatalogHash: hash}); err != nil {
			return nil, err
		}
	}
	return s, nil
}

func (s *Store) metaPath() string { return filepath.Join(s.dir, "session.json") }

// Meta returns a fresh read of session.json.
func (s *Store) Meta() (Meta, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readMeta()
}

func (s *Store) readMeta() (Meta, error) {
	data, err := os.ReadFile(s.metaPath())
	if err != nil {
		return Meta{}, fmt.Errorf("sessiondir: %w", err)
	}
	var m Meta
	if err := json.Unmarshal(data, &m); err != nil {
		return Meta{}, fmt.Errorf("sessiondir: parse session.json: %w", err)
	}
	if m.Version > MetaVersion {
		return Meta{}, fmt.Errorf("sessiondir: session.json version %d is newer than this build (%d)", m.Version, MetaVersion)
	}
	migrateMeta(&m)
	return m, nil
}

// migrateMeta walks a just-read Meta forward through the typed migration
// ladder to the current MetaVersion, mirroring save.Load's per-step
// migrateV{N}toV{N+1} discipline. Each step is a pure shape fix; the last
// stamps Version = MetaVersion so a re-write persists at the current schema.
func migrateMeta(m *Meta) {
	if m.Version < 2 {
		migrateMetaV1ToV2(m)
	}
	m.Version = MetaVersion
}

// migrateMetaV1ToV2 carries a v0.27 session (roster/invites, no docks)
// forward: v1 had no cross-player docking, so Docks is simply absent. The
// nil default is already correct — the function exists to keep the ladder
// explicit and to give the next shape change a home. v0.28 S5.
func migrateMetaV1ToV2(m *Meta) {
	if m.Docks == nil {
		m.Docks = nil // explicit: a v1 session never had docks
	}
}

// SetDocks persists the cross-player-dock cross-ref (v0.28 S5). The serve
// layer calls it when the live relay ledger changes, so a reconnecting
// guest resumes docked-as-guest. Re-reads under the lock so a concurrent
// roster edit isn't clobbered.
func (s *Store) SetDocks(docks []DockLink) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.readMeta()
	if err != nil {
		return err
	}
	m.Docks = docks
	return s.writeMeta(m)
}

// writeMeta persists atomically (tmpfile + rename), matching the save
// package's crash discipline.
func (s *Store) writeMeta(m Meta) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("sessiondir: %w", err)
	}
	tmp := s.metaPath() + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("sessiondir: %w", err)
	}
	if err := os.Rename(tmp, s.metaPath()); err != nil {
		return fmt.Errorf("sessiondir: %w", err)
	}
	return nil
}

// EnsureHost auto-enrolls the serving player as roster entry #1 with
// the Host role on first --serve (idempotent — an existing host entry
// is returned untouched, so a renamed handle survives restarts).
func (s *Store) EnsureHost(handle string) (Player, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.readMeta()
	if err != nil {
		return Player{}, err
	}
	for _, p := range m.Roster {
		if p.Role == RoleHost {
			return p, nil
		}
	}
	host := Player{
		Fingerprint: HostFingerprint,
		Handle:      handle,
		Role:        RoleHost,
		EnrolledAt:  time.Now().UTC(),
		Calibrated:  true, // the host's own terminal is theirs to judge
	}
	m.Roster = append([]Player{host}, m.Roster...)
	return host, s.writeMeta(m)
}

// MintInvite creates a one-time code pre-bound to handle.
func (s *Store) MintInvite(handle string) (Invite, error) {
	handle = strings.TrimSpace(handle)
	if handle == "" {
		return Invite{}, errors.New("sessiondir: invite needs a handle")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.readMeta()
	if err != nil {
		return Invite{}, err
	}
	code, err := mintCode()
	if err != nil {
		return Invite{}, err
	}
	inv := Invite{Code: code, Handle: handle, CreatedAt: time.Now().UTC()}
	m.Invites = append(m.Invites, inv)
	return inv, s.writeMeta(m)
}

// mintCode returns a short read-out-loud code ("AB2C-DE3F"): 40 bits
// from crypto/rand in base32 (A–Z, 2–7 — no 0/O or 1/I confusion).
func mintCode() (string, error) {
	raw := make([]byte, 5)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("sessiondir: %w", err)
	}
	c := base32.StdEncoding.EncodeToString(raw) // 8 chars, no padding at 5 bytes
	return c[:4] + "-" + c[4:], nil
}

// Peek validates an invite code without consuming it — the enroll
// flow shows the pre-bound handle for editing before committing.
func (s *Store) Peek(code string) (Invite, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.readMeta()
	if err != nil {
		return Invite{}, err
	}
	for _, inv := range m.Invites {
		if normalizeCode(inv.Code) == normalizeCode(code) {
			return inv, nil
		}
	}
	return Invite{}, ErrUnknownInvite
}

// Enroll redeems the code (one-time) and adds the player to the
// roster in a single locked step. The code is re-validated here, so a
// Peek that raced another enrollment fails cleanly instead of
// double-spending. Calibrated is stamped true — the enroll flow runs
// behind the calibration card.
func (s *Store) Enroll(code, fingerprint, handle string) (Player, error) {
	handle = strings.TrimSpace(handle)
	if handle == "" {
		return Player{}, errors.New("sessiondir: handle can't be empty")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.readMeta()
	if err != nil {
		return Player{}, err
	}
	// One roster entry per key (review follow-up): a fingerprint that
	// is already enrolled — a second concurrent session redeeming a
	// second code — gets its existing identity back and the code is
	// NOT spent. RemovePlayer's single-match delete stays sound.
	for _, p := range m.Roster {
		if p.Fingerprint == fingerprint {
			return p, nil
		}
	}
	idx := -1
	for i, inv := range m.Invites {
		if normalizeCode(inv.Code) == normalizeCode(code) {
			idx = i
			break
		}
	}
	if idx < 0 {
		return Player{}, ErrUnknownInvite
	}
	m.Invites = append(m.Invites[:idx], m.Invites[idx+1:]...)
	p := Player{
		Fingerprint: fingerprint,
		Handle:      handle,
		Role:        RoleGuest,
		EnrolledAt:  time.Now().UTC(),
		Calibrated:  true,
	}
	m.Roster = append(m.Roster, p)
	return p, s.writeMeta(m)
}

func normalizeCode(c string) string {
	return strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(c), "-", ""))
}

// RevokeInvite deletes an unredeemed code. ErrUnknownInvite when the
// code doesn't exist (already redeemed, already revoked, or a typo).
func (s *Store) RevokeInvite(code string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.readMeta()
	if err != nil {
		return err
	}
	for i, inv := range m.Invites {
		if normalizeCode(inv.Code) == normalizeCode(code) {
			m.Invites = append(m.Invites[:i], m.Invites[i+1:]...)
			return s.writeMeta(m)
		}
	}
	return ErrUnknownInvite
}

// RemovePlayer drops a guest from the roster: their key no longer
// resumes and they'd need a fresh invite. The persisted payload stays
// on disk — a re-invited player finds their program intact. The host
// entry can't be removed. A live session isn't kicked (MVP): removal
// gates the NEXT connect.
func (s *Store) RemovePlayer(fingerprint string) error {
	if fingerprint == HostFingerprint {
		return errors.New("sessiondir: can't remove the host")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.readMeta()
	if err != nil {
		return err
	}
	for i, p := range m.Roster {
		if p.Fingerprint == fingerprint {
			m.Roster = append(m.Roster[:i], m.Roster[i+1:]...)
			return s.writeMeta(m)
		}
	}
	return ErrNotEnrolled
}

// MayAdminister reports whether the given fingerprint may perform
// session-admin actions — minting/revoking invites and removing
// players. It is the single authorization predicate the serve-layer
// handler consults before acting (v0.30 S1, #222): authorization is a
// capability decided here, in the store, not a UI-presentation detail.
// Today only the host qualifies; the admin role (v0.30 S2) extends
// roleMayAdminister without touching any caller. An unknown or
// unenrolled fingerprint may not administer.
func (s *Store) MayAdminister(fingerprint string) bool {
	p, err := s.FindPlayer(fingerprint)
	if err != nil {
		return false
	}
	return roleMayAdminister(p.Role)
}

// roleMayAdminister centralises which roster roles carry the admin
// capability (mint/revoke invites, remove guests). The host and any
// promoted admin qualify; a plain guest does not.
func roleMayAdminister(role string) bool {
	return role == RoleHost || role == RoleAdmin
}

// MayDelegate reports whether the fingerprint may promote a guest to
// admin or demote an admin back to guest. Single-rooted escalation
// (v0.30 S2): only the host delegates administration — an admin can
// neither create nor remove another admin. Kept distinct from
// MayAdminister so the escalation tree stays single-rooted.
func (s *Store) MayDelegate(fingerprint string) bool {
	p, err := s.FindPlayer(fingerprint)
	if err != nil {
		return false
	}
	return p.Role == RoleHost
}

// MayRemove reports whether actor may remove target from the roster
// (v0.30 S3, #224) — the guardrail matrix for the first admin power that
// can lock someone out. The rules keep escalation single-rooted:
//
//   - the actor must carry the admin capability (host or admin);
//   - nobody removes themselves (avoids a self-inflicted lockout);
//   - nobody removes the host (the session's root);
//   - an admin may not remove another admin — only the host may
//     (mirrors promotion being host-only).
//
// Both fingerprints must be enrolled. The store's RemovePlayer still
// guards the host independently; this predicate is the actor-aware gate
// the serve handler consults before calling it.
func (s *Store) MayRemove(actor, target string) bool {
	if actor == target {
		return false
	}
	m, err := s.Meta()
	if err != nil {
		return false
	}
	var actorRole, targetRole string
	var haveActor, haveTarget bool
	for _, p := range m.Roster {
		switch p.Fingerprint {
		case actor:
			actorRole, haveActor = p.Role, true
		case target:
			targetRole, haveTarget = p.Role, true
		}
	}
	if !haveActor || !haveTarget {
		return false
	}
	if !roleMayAdminister(actorRole) {
		return false
	}
	if targetRole == RoleHost {
		return false
	}
	if actorRole == RoleAdmin && targetRole == RoleAdmin {
		return false
	}
	return true
}

// PromoteAdmin grants the admin role to an enrolled guest by
// fingerprint. Idempotent — re-promoting an admin is a no-op. The host
// is rejected (already root) and an unknown fingerprint returns
// ErrNotEnrolled. Only the host should call this (enforced at the
// handler via MayDelegate); the store guards the host-role invariant.
func (s *Store) PromoteAdmin(fingerprint string) error {
	return s.setRole(fingerprint, RoleAdmin)
}

// DemoteAdmin returns an admin to guest by fingerprint. Idempotent for
// a player already a guest; rejects the host and unknown fingerprints.
func (s *Store) DemoteAdmin(fingerprint string) error {
	return s.setRole(fingerprint, RoleGuest)
}

// setRole is the shared guest↔admin role mutation. The host's role is
// immutable (it is the single root); an unknown fingerprint is
// ErrNotEnrolled.
func (s *Store) setRole(fingerprint, role string) error {
	if fingerprint == HostFingerprint {
		return errors.New("sessiondir: can't change the host's role")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.readMeta()
	if err != nil {
		return err
	}
	for i := range m.Roster {
		if m.Roster[i].Fingerprint == fingerprint {
			if m.Roster[i].Role == RoleHost {
				return errors.New("sessiondir: can't change the host's role")
			}
			m.Roster[i].Role = role
			return s.writeMeta(m)
		}
	}
	return ErrNotEnrolled
}

// FindPlayer looks a fingerprint up in the roster.
func (s *Store) FindPlayer(fingerprint string) (Player, error) {
	m, err := s.Meta()
	if err != nil {
		return Player{}, err
	}
	for _, p := range m.Roster {
		if p.Fingerprint == fingerprint {
			return p, nil
		}
	}
	return Player{}, ErrNotEnrolled
}

// payloadPath maps a fingerprint to its envelope file. Fingerprints
// contain '/' (base64), so the filename is a sha256 hex of it.
func (s *Store) payloadPath(fingerprint string) string {
	sum := sha256.Sum256([]byte(fingerprint))
	return filepath.Join(s.dir, "players", hex.EncodeToString(sum[:])+".json")
}

// SavePlayer persists the player's world as a save envelope at the
// package's current SchemaVersion (the existing save machinery,
// arbitrary path).
func (s *Store) SavePlayer(fingerprint string, w *sim.World) error {
	return save.Save(w, s.payloadPath(fingerprint))
}

// LoadPlayer restores the player's world. fs.ErrNotExist means no
// payload yet (first session); save.ErrCatalogMismatch propagates so
// the connect path can reject rather than corrupt (ADR 0034 — reuses
// the existing save mechanism).
func (s *Store) LoadPlayer(fingerprint string) (*sim.World, error) {
	return save.Load(s.payloadPath(fingerprint))
}

// HasPayload reports whether the player has a persisted world.
func (s *Store) HasPayload(fingerprint string) bool {
	_, err := os.Stat(s.payloadPath(fingerprint))
	return err == nil
}

// LatestSimTime scans every persisted player payload for the maximum
// stored subspace time — offline players hold the frontier (v0.27 S4,
// ADR 0034: you can never start in someone's past, online or not).
// ok is false when no payload parses. Unreadable files are skipped:
// frontier is a floor, not an integrity check.
func (s *Store) LatestSimTime() (time.Time, bool) {
	entries, err := os.ReadDir(filepath.Join(s.dir, "players"))
	if err != nil {
		return time.Time{}, false
	}
	var max time.Time
	found := false
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.dir, "players", e.Name()))
		if err != nil {
			continue
		}
		var probe struct {
			Payload struct {
				SimTimeNano int64 `json:"sim_time_unix_nano"`
			} `json:"payload"`
		}
		if json.Unmarshal(data, &probe) != nil || probe.Payload.SimTimeNano == 0 {
			continue
		}
		t := time.Unix(0, probe.Payload.SimTimeNano).UTC()
		if !found || t.After(max) {
			max = t
			found = true
		}
	}
	return max, found
}
