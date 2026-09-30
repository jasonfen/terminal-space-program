//go:build windows

package sessiondir

import "os"

// Windows has no flock in the stdlib syscall package and the project
// avoids extra deps for a host-side admin path that is not run there in
// practice; the in-process mutex still applies.
func flockExclusive(*os.File) error { return nil }

func flockRelease(*os.File) {}
