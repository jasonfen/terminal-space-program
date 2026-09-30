package sessiondir

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func osStat(p string) (fs.FileInfo, error) { return os.Stat(p) }

// snapshotDir lists every entry with size and mtime, so any write shows.
func snapshotDir(t *testing.T, dir string) string {
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
	return fmt.Sprint(out)
}
