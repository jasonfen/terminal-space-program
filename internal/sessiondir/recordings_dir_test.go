package sessiondir

import (
	"path/filepath"
	"testing"
)

// New file: sessiondir_test.go is over the ~600-line rule.
func TestGuestRecordingsDirUnderSession(t *testing.T) {
	s := &Store{dir: "/sess"}
	for _, c := range []struct{ handle, want string }{
		{"ansi", "/sess/recordings/ansi"},
		{"SHA256:ab/cd+==", "/sess/recordings/SHA256_ab_cd___"},
		{"", "/sess/recordings/_"},
		{"..", "/sess/recordings/_.."},
	} {
		if got := s.RecordingsDir(c.handle); got != filepath.FromSlash(c.want) {
			t.Errorf("RecordingsDir(%q) = %q, want %q", c.handle, got, c.want)
		}
	}
}
