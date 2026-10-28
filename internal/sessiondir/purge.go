package sessiondir

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// backupStampLayout is the UTC stamp both --reset-fleet and a purge prefix
// to the copied payload's file name.
const backupStampLayout = "20060102T150405Z"

// backupPayload copies the raw payload at src into <dir>/backup/<stamp>-<name>
// byte for byte and returns where it went. Callers read src themselves so
// the bytes they report on are the bytes copied. Shared by --reset-fleet and
// the purge: both promise the previous envelope survives, unparsed.
func (s *Store) backupPayload(src string, data []byte, stamp string) (string, error) {
	bdir := filepath.Join(s.dir, "backup")
	if err := os.MkdirAll(bdir, 0o755); err != nil {
		return "", fmt.Errorf("backup dir: %w", err)
	}
	bak := filepath.Join(bdir, stamp+"-"+filepath.Base(src))
	if err := os.WriteFile(bak, data, 0o644); err != nil {
		return "", err
	}
	return bak, nil
}

// PurgeResult says what PurgePlayer did.
type PurgeResult struct {
	// BackupPath is where the payload was copied, empty when the player had
	// none (never saved, or already purged).
	BackupPath string
}

// PurgePlayer deletes a player's persisted world (ADR 0054 slice E, kick and
// delete vessels). The payload is copied byte-identical to backup/ first, as
// --reset-fleet does, and the delete happens only once that copy is on disk:
// the payload is the only copy of their vessels. A player with no payload is
// not an error. The roster row, dock records and invites are not touched
// here.
func (s *Store) PurgePlayer(fingerprint string) (PurgeResult, error) {
	if fingerprint == HostFingerprint {
		return PurgeResult{}, errors.New("sessiondir: can't purge the host")
	}
	// Resolved before the lock: FindPlayer takes it too.
	handle := ""
	if p, err := s.FindPlayer(fingerprint); err == nil {
		handle = p.Handle
	}
	defer s.lock()()
	src := s.payloadPath(fingerprint)
	data, err := os.ReadFile(src)
	if errors.Is(err, os.ErrNotExist) {
		s.removeRecordings(fingerprint, handle)
		return PurgeResult{}, nil
	}
	if err != nil {
		return PurgeResult{}, fmt.Errorf("sessiondir: purge: read: %w", err)
	}
	bak, err := s.backupPayload(src, data, time.Now().UTC().Format(backupStampLayout))
	if err != nil {
		return PurgeResult{}, fmt.Errorf("sessiondir: purge: backup: %w", err)
	}
	if err := os.Remove(src); err != nil && !errors.Is(err, os.ErrNotExist) {
		return PurgeResult{BackupPath: bak}, fmt.Errorf("sessiondir: purge: delete: %w", err)
	}
	s.removeRecordings(fingerprint, handle)
	return PurgeResult{BackupPath: bak}, nil
}

// removeRecordings drops the player's Flight Recording folders (#817): under
// the roster handle, and under the fingerprint a kicked player's were named
// by. Best effort: recordings are not worth failing a purge over.
func (s *Store) removeRecordings(fingerprint, handle string) {
	for _, h := range []string{handle, fingerprint} {
		if h != "" {
			_ = os.RemoveAll(s.RecordingsDir(h))
		}
	}
}
