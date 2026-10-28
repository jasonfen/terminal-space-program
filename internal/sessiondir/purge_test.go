package sessiondir

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// ADR 0054 slice E: PurgePlayer deletes a player's payload after copying it,
// byte for byte, to backup/ (--reset-fleet's stamp and folder).
func TestPurgePlayerBacksUpByteIdenticalThenDeletes(t *testing.T) {
	s := openStore(t)
	const fp = "SHA256:pilot2"
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SavePlayer(fp, w); err != nil {
		t.Fatal(err)
	}
	src := s.payloadPath(fp)
	before, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}

	res, err := s.PurgePlayer(fp)
	if err != nil {
		t.Fatalf("PurgePlayer: %v", err)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Errorf("players/<sha>.json still there after the purge: %v", err)
	}
	if res.BackupPath == "" {
		t.Fatal("no backup path reported for a player who had a payload")
	}
	if filepath.Dir(res.BackupPath) != filepath.Join(s.dir, "backup") {
		t.Errorf("backup at %s, want under %s/backup", res.BackupPath, s.dir)
	}
	if !strings.HasSuffix(res.BackupPath, "-"+filepath.Base(src)) {
		t.Errorf("backup %s is not <stamp>-%s", res.BackupPath, filepath.Base(src))
	}
	after, err := os.ReadFile(res.BackupPath)
	if err != nil {
		t.Fatalf("backup unreadable: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Errorf("backup differs from the pre-purge payload: %d bytes vs %d", len(after), len(before))
	}
}

func TestPurgePlayerWithoutPayloadIsQuiet(t *testing.T) {
	s := openStore(t)
	res, err := s.PurgePlayer("SHA256:never-saved")
	if err != nil {
		t.Fatalf("PurgePlayer with nothing to purge: %v", err)
	}
	if res.BackupPath != "" {
		t.Errorf("backup path %q for a player with no payload", res.BackupPath)
	}
	if _, err := os.Stat(filepath.Join(s.dir, "backup")); err == nil {
		t.Error("an empty backup folder was created for nothing")
	}
}

func TestPurgePlayerRefusesTheHost(t *testing.T) {
	s := openStore(t)
	if _, err := s.PurgePlayer(HostFingerprint); err == nil {
		t.Error("the host's fingerprint was purgeable")
	}
}

// A backup that cannot be written stops the purge before the delete: the
// payload is the only copy.
func TestPurgePlayerKeepsThePayloadWhenTheBackupFails(t *testing.T) {
	s := openStore(t)
	const fp = "SHA256:pilot2"
	w, _ := sim.NewWorld()
	if err := s.SavePlayer(fp, w); err != nil {
		t.Fatal(err)
	}
	// A file where the backup folder must go.
	if err := os.WriteFile(filepath.Join(s.dir, "backup"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PurgePlayer(fp); err == nil {
		t.Fatal("purge succeeded although the backup could not be written")
	}
	if !s.HasPayload(fp) {
		t.Error("the payload was deleted although its backup failed")
	}
}

// #817 item 3: a purge takes the player's Flight Recordings with it, under
// their roster handle and under the raw fingerprint (a kicked player's row is
// gone, so their folder was named by it); a neighbour's folder stays. Runs
// with and without a payload.
func TestPurgePlayerRemovesRecordings(t *testing.T) {
	for _, withPayload := range []bool{true, false} {
		s := openStore(t)
		const fp = "SHA256:pilot/2"
		inv, err := s.MintInvite("placeholder")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.Enroll(inv.Code, fp, "pilot2"); err != nil {
			t.Fatal(err)
		}
		if withPayload {
			w, _ := sim.NewWorld()
			if err := s.SavePlayer(fp, w); err != nil {
				t.Fatal(err)
			}
		}
		for _, h := range []string{"pilot2", fp, "neighbour"} {
			d := s.RecordingsDir(h)
			if err := os.MkdirAll(d, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(d, "0001-x.txt"), []byte("rows\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := s.PurgePlayer(fp); err != nil {
			t.Fatal(err)
		}
		for h, want := range map[string]bool{"pilot2": false, fp: false, "neighbour": true} {
			if _, err := os.Stat(s.RecordingsDir(h)); (err == nil) != want {
				t.Errorf("payload=%v: recordings for %q present=%v, want %v", withPayload, h, err == nil, want)
			}
		}
	}
}
