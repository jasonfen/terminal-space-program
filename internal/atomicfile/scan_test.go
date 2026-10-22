package atomicfile

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func hasBareRename(src string) bool { return strings.Contains(src, "os.Rename(") }

// Positive control: the scan can flag a planted bad case.
func TestScanFlagsPlantedRename(t *testing.T) {
	if !hasBareRename("os.WriteFile(tmp, d, 0o644)\nos.Rename(tmp, p)") {
		t.Fatal("scan missed a planted bare rename")
	}
}

// No non-test code may rename a file into place itself: that is the
// WriteFile+Rename sequence without fsync. Use atomicfile.Write.
func TestNoBareRenameOutsideAtomicfile(t *testing.T) {
	root := filepath.Join("..", "..")
	var bad []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return err
		}
		if filepath.Base(filepath.Dir(p)) == "atomicfile" {
			return nil
		}
		b, _ := os.ReadFile(p)
		if hasBareRename(string(b)) {
			bad = append(bad, p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(bad) > 0 {
		t.Fatalf("bare os.Rename in %v; use atomicfile.Write", bad)
	}
}
