package atomicfile

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteReplacesContentAndLeavesNoTmp(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a.json")
	for _, want := range []string{"first, longer content", "second"} {
		if err := Write(p, []byte(want), 0o644); err != nil {
			t.Fatal(err)
		}
		got, _ := os.ReadFile(p)
		if string(got) != want {
			t.Fatalf("got %q want %q", got, want)
		}
		if _, err := os.Stat(p + ".tmp"); !os.IsNotExist(err) {
			t.Fatalf("tmp left behind: %v", err)
		}
	}
}

// Observes that Sync is called on the tmp file before it is renamed
// into place. It cannot observe the data reaching the disk.
func TestWriteSyncsTmpBeforeRename(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a.json")
	var calls int
	orig := syncFile
	defer func() { syncFile = orig }()
	syncFile = func(f *os.File) error {
		calls++
		if f.Name() != p+".tmp" {
			t.Errorf("synced %q, want the tmp file", f.Name())
		}
		if _, err := os.Stat(p); err == nil {
			t.Error("destination exists before sync: rename came first")
		}
		return f.Sync()
	}
	if err := Write(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("sync calls = %d, want 1", calls)
	}
}

func TestWriteSyncFailureKeepsOldFileAndRemovesTmp(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a.json")
	if err := os.WriteFile(p, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	orig := syncFile
	defer func() { syncFile = orig }()
	boom := errors.New("boom")
	syncFile = func(*os.File) error { return boom }
	err := Write(p, []byte("new"), 0o644)
	if !errors.Is(err, boom) || !strings.Contains(err.Error(), "write tmpfile") {
		t.Fatalf("err = %v", err)
	}
	if got, _ := os.ReadFile(p); string(got) != "old" {
		t.Fatalf("destination clobbered: %q", got)
	}
	if _, err := os.Stat(p + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("tmp left behind: %v", err)
	}
}

func TestWriteRenameFailureRemovesTmp(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "d")
	if err := os.MkdirAll(filepath.Join(p, "sub"), 0o755); err != nil { // rename file over non-empty dir fails
		t.Fatal(err)
	}
	err := Write(p, []byte("x"), 0o644)
	if err == nil || !strings.Contains(err.Error(), "rename into place") {
		t.Fatalf("err = %v", err)
	}
	if _, err := os.Stat(p + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("tmp left behind: %v", err)
	}
}
