//go:build darwin || linux

package bundleevaluate

import (
	"os"
	"syscall"
)

// Do not follow a last-component symlink or block on a FIFO substituted after
// Lstat. f.Stat/SameFile in ReadFile then verify the opened regular inode.
func openPolicy(path string) (*os.File, error) {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), path), nil
}
