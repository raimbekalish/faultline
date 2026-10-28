//go:build darwin || linux

package lab

import (
	"os"
	"syscall"
)

func openNoFollow(r *os.Root, name string) (*os.File, error) {
	return r.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
}
func ownedDirectory(info os.FileInfo) bool {
	s, ok := info.Sys().(*syscall.Stat_t)
	return ok && s.Uid == uint32(os.Geteuid())
}
func socketNumbers(info os.FileInfo) (uint64, uint64, error) {
	s, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, 0, ErrInspection
	}
	return uint64(s.Dev), uint64(s.Ino), nil
}
