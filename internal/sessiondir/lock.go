package sessiondir

import (
	"os"
	"path/filepath"
)

// lock takes the in-process mutex and, for writable stores, an advisory
// file lock on <dir>/session.lock, and returns the matching unlock.
// Store.mu only serialises one process; the CLI (promote/demote/invite)
// and a running server are separate processes, so without the file lock
// a read-modify-write of session.json in one can lose the other's write.
//
// The file lock is best-effort: if the lock file can't be opened the
// store degrades to the in-process mutex alone. Read-only stores skip it
// (reads are atomic against the tmpfile+rename write) so they create
// nothing. On Windows the file lock is a no-op (see lock_windows.go).
func (s *Store) lock() func() {
	s.mu.Lock()
	if s.readOnly {
		return s.mu.Unlock
	}
	f, err := os.OpenFile(filepath.Join(s.dir, "session.lock"), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return s.mu.Unlock
	}
	if err := flockExclusive(f); err != nil {
		f.Close()
		return s.mu.Unlock
	}
	return func() {
		flockRelease(f)
		f.Close()
		s.mu.Unlock()
	}
}
