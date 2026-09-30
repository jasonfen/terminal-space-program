//go:build !windows

package sessiondir

import (
	"errors"
	"os"
	"syscall"
)

func flockExclusive(f *os.File) error {
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX)
		if !errors.Is(err, syscall.EINTR) {
			return err
		}
	}
}

func flockRelease(f *os.File) { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN) }
