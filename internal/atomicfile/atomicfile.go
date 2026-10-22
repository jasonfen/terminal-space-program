// Package atomicfile writes a file so a crash leaves the old or the new
// content, never a truncated one.
package atomicfile

import (
	"fmt"
	"os"
)

// syncFile is a var only so tests can observe the call.
var syncFile = (*os.File).Sync

// Write writes data to path via a sibling ".tmp" file and a rename.
func Write(path string, data []byte, perm os.FileMode) error {
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return fmt.Errorf("write tmpfile: %w", err)
	}
	_, err = f.Write(data)
	if err == nil {
		err = syncFile(f)
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(tmp)
		return fmt.Errorf("write tmpfile: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("rename into place: %w", err)
	}
	return nil
}
