package sessiondir

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode"

	"github.com/charmbracelet/x/ansi"
)

func hasNonPrintable(s string) bool {
	for _, r := range s {
		if !unicode.IsPrint(r) {
			return true
		}
	}
	return false
}

// A pasted handle with an escape sequence and whitespace controls must be
// stored without any of them, and the seat must still be the same key.
func TestEnrollStripsControlCharacters(t *testing.T) {
	s := openStore(t)
	inv, err := s.MintInvite("placeholder")
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.Enroll(inv.Code, "SHA256:bob", "bob\x1b[2J\n\t\r")
	if err != nil {
		t.Fatalf("Enroll: %v", err)
	}
	m, _ := s.Meta()
	for _, h := range []string{p.Handle, m.Roster[0].Handle} {
		if hasNonPrintable(h) {
			t.Errorf("stored handle %q still holds non-printable runes", h)
		}
	}
	if m.Roster[0].Handle != "bob[2J" {
		t.Errorf("stored handle = %q, want %q", m.Roster[0].Handle, "bob[2J")
	}
}

func TestEnrollRefusesHandleOfOnlyControlCharacters(t *testing.T) {
	s := openStore(t)
	inv, _ := s.MintInvite("placeholder")
	if _, err := s.Enroll(inv.Code, "SHA256:x", "\x1b\x1b\n"); err == nil {
		t.Fatal("Enroll accepted a handle with no printable characters")
	}
}

// 24 CJK runes are 48 columns; the cap bounds display width.
func TestEnrollBoundsHandleDisplayWidth(t *testing.T) {
	s := openStore(t)
	inv, _ := s.MintInvite("placeholder")
	p, err := s.Enroll(inv.Code, "SHA256:cjk", strings.Repeat("漢", 24))
	if err != nil {
		t.Fatal(err)
	}
	if w := ansi.StringWidth(p.Handle); w > 24 {
		t.Errorf("handle %q is %d columns, want <= 24", p.Handle, w)
	}
}

func TestMintInviteStripsControlCharacters(t *testing.T) {
	s := openStore(t)
	inv, err := s.MintInvite("eve\x1b[2J")
	if err != nil {
		t.Fatal(err)
	}
	if hasNonPrintable(inv.Handle) {
		t.Errorf("minted handle %q holds non-printable runes", inv.Handle)
	}
}

// A session.json written before the filter: Open cleans it, the seat keeps
// its fingerprint, and a cleaned name that now equals another is deduped.
func TestOpenCleansLegacyControlHandles(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "players"), 0o755); err != nil {
		t.Fatal(err)
	}
	legacy := `{"version":5,"body_catalog_hash":"x","roster":[
	 {"fingerprint":"SHA256:a","handle":"bob\u001b[2J","role":"guest"},
	 {"fingerprint":"SHA256:b","handle":"bob[2J","role":"guest"},
	 {"fingerprint":"SHA256:c","handle":"\u001b\n","role":"guest"}],
	 "invites":[{"code":"AAAA","handle":"ev\u001bil"}]}`
	if err := os.WriteFile(filepath.Join(dir, "session.json"), []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	m, _ := s.Meta()
	if len(m.Roster) != 3 {
		t.Fatalf("roster = %d rows, want 3", len(m.Roster))
	}
	seen := map[string]bool{}
	for i, fp := range []string{"SHA256:a", "SHA256:b", "SHA256:c"} {
		p := m.Roster[i]
		if p.Fingerprint != fp {
			t.Errorf("row %d fingerprint = %q, want %q", i, p.Fingerprint, fp)
		}
		if p.Handle == "" || hasNonPrintable(p.Handle) {
			t.Errorf("row %d handle %q not clean/non-empty", i, p.Handle)
		}
		if seen[foldHandle(p.Handle)] {
			t.Errorf("duplicate handle %q after cleaning", p.Handle)
		}
		seen[foldHandle(p.Handle)] = true
	}
	if hasNonPrintable(m.Invites[0].Handle) {
		t.Errorf("invite handle %q not cleaned", m.Invites[0].Handle)
	}
}
