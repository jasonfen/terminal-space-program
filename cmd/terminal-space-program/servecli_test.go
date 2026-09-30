package main

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/jasonfen/terminal-space-program/internal/sessiondir"
)

// seedStore builds a session dir with host "jason", guest "ansi" and
// guest "Fen", returning the dir and store.
func seedStore(t *testing.T) (string, *sessiondir.Store) {
	t.Helper()
	dir := t.TempDir()
	st, err := sessiondir.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.EnsureHost("jason"); err != nil {
		t.Fatal(err)
	}
	for i, h := range []string{"ansi", "Fen"} {
		inv, err := st.MintInvite(h)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := st.Enroll(inv.Code, "SHA256:fp"+string(rune('A'+i)), h); err != nil {
			t.Fatal(err)
		}
	}
	return dir, st
}

func runCLI(dir string, args ...string) (int, string, string) {
	var out, errb bytes.Buffer
	code := serveCLI(args, dir, &out, &errb)
	return code, out.String(), errb.String()
}

func roleOf(t *testing.T, st *sessiondir.Store, fp string) string {
	t.Helper()
	p, err := st.FindPlayer(fp)
	if err != nil {
		t.Fatal(err)
	}
	return p.Role
}

func TestServeCLIPromoteByHandle(t *testing.T) {
	dir, st := seedStore(t)
	// case-insensitive handle match
	code, out, errs := runCLI(dir, "promote", "FEN")
	if code != 0 {
		t.Fatalf("exit %d, stderr %q", code, errs)
	}
	if got := roleOf(t, st, "SHA256:fpB"); got != sessiondir.RoleAdmin {
		t.Errorf("Fen role = %q, want admin", got)
	}
	if got := roleOf(t, st, "SHA256:fpA"); got != sessiondir.RoleGuest {
		t.Errorf("ansi role = %q, want guest (untouched)", got)
	}
	if !strings.Contains(out, "Fen") || !strings.Contains(out, "admin") {
		t.Errorf("stdout %q should name Fen and admin", out)
	}
	// idempotent
	if code, _, errs := runCLI(dir, "promote", "Fen"); code != 0 {
		t.Errorf("re-promote exit %d: %s", code, errs)
	}
	// demote returns to guest
	if code, _, errs := runCLI(dir, "demote", "fen"); code != 0 {
		t.Fatalf("demote exit %d: %s", code, errs)
	}
	if got := roleOf(t, st, "SHA256:fpB"); got != sessiondir.RoleGuest {
		t.Errorf("Fen role after demote = %q, want guest", got)
	}
}

func TestServeCLIDemoteRefusesHost(t *testing.T) {
	dir, st := seedStore(t)
	for _, verb := range []string{"demote", "promote"} {
		code, _, errs := runCLI(dir, verb, "jason")
		if code == 0 {
			t.Errorf("%s host: exit 0, want failure", verb)
		}
		if !strings.Contains(errs, "host") {
			t.Errorf("%s host stderr %q should mention host", verb, errs)
		}
	}
	if got := roleOf(t, st, sessiondir.HostFingerprint); got != sessiondir.RoleHost {
		t.Errorf("host role = %q, want host", got)
	}
}

func TestServeCLIRosterLists(t *testing.T) {
	dir, _ := seedStore(t)
	runCLI(dir, "promote", "ansi")
	code, out, errs := runCLI(dir, "roster")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errs)
	}
	for _, want := range []string{"jason", "host", "ansi", "admin", "Fen", "guest"} {
		if !strings.Contains(out, want) {
			t.Errorf("roster output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "SHA256:") {
		t.Errorf("roster should not print fingerprints:\n%s", out)
	}
}

func TestServeCLIUnknownHandleErrors(t *testing.T) {
	dir, _ := seedStore(t)
	code, _, errs := runCLI(dir, "promote", "nobody")
	if code == 0 || !strings.Contains(errs, "nobody") {
		t.Errorf("exit %d stderr %q: want failure naming the handle", code, errs)
	}
}

func TestResolveHandleAmbiguousErrors(t *testing.T) {
	roster := []sessiondir.Player{
		{Fingerprint: "a", Handle: "Fen", Role: sessiondir.RoleGuest},
		{Fingerprint: "b", Handle: "fen", Role: sessiondir.RoleGuest},
		{Fingerprint: "c", Handle: "ansi", Role: sessiondir.RoleGuest},
	}
	if _, err := resolveHandle(roster, "FEN"); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Errorf("err = %v, want ambiguous", err)
	}
	if p, err := resolveHandle(roster, "ansi"); err != nil || p.Fingerprint != "c" {
		t.Errorf("ansi: %v %v", p, err)
	}
}

func TestServeCLIUsageAndInvite(t *testing.T) {
	dir, _ := seedStore(t)
	if code, _, errs := runCLI(dir); code != 2 || !strings.Contains(errs, "promote") {
		t.Errorf("usage: exit %d stderr %q", code, errs)
	}
	code, out, _ := runCLI(dir, "invite", "newbie")
	if code != 0 || !strings.Contains(out, "invite minted for newbie") {
		t.Errorf("invite: exit %d out %q", code, out)
	}
}

// Handles may be non-ASCII; the role column must start at the same
// display column on every row (pad by display width, not bytes).
func TestServeCLIRosterAlignsNonASCIIHandles(t *testing.T) {
	dir, st := seedStore(t)
	for i, h := range []string{"José", "日本語"} {
		inv, err := st.MintInvite(h)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := st.Enroll(inv.Code, "SHA256:u"+string(rune('a'+i)), h); err != nil {
			t.Fatal(err)
		}
	}
	code, out, errs := runCLI(dir, "roster")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errs)
	}
	col := -1
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		f := strings.Fields(line)
		role := f[len(f)-1]
		prefix := strings.TrimSuffix(line, role)
		got := lipgloss.Width(prefix)
		if col == -1 {
			col = got
		} else if got != col {
			t.Errorf("role column at %d on %q, want %d\n%s", got, line, col, out)
		}
	}
}

// A read-only command must not write: a missing session dir is reported
// (not created), and an existing one is left byte-for-byte untouched.
func TestServeCLIRosterIsReadOnly(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope")
	code, _, errs := runCLI(missing, "roster")
	if code == 0 || !strings.Contains(errs, "no session") {
		t.Errorf("missing dir: exit %d, stderr %q; want failure saying no session", code, errs)
	}
	if _, err := os.Stat(missing); err == nil {
		t.Error("roster created the session dir")
	}

	dir, _ := seedStore(t)
	before := dirListing(t, dir)
	if code, _, errs := runCLI(dir, "roster"); code != 0 {
		t.Fatalf("exit %d: %s", code, errs)
	}
	if after := dirListing(t, dir); after != before {
		t.Errorf("roster changed the session dir:\nbefore %s\nafter  %s", before, after)
	}
}

func dirListing(t *testing.T, dir string) string {
	t.Helper()
	var out []string
	filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		fi, _ := d.Info()
		out = append(out, fmt.Sprintf("%s:%d:%d", p, fi.Size(), fi.ModTime().UnixNano()))
		return nil
	})
	sort.Strings(out)
	return strings.Join(out, " ")
}
