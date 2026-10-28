//go:build !darwin && !linux

package lab

import "os"

func openNoFollow(*os.Root, string) (*os.File, error)   { return nil, ErrFile }
func ownedDirectory(os.FileInfo) bool                   { return false }
func socketNumbers(os.FileInfo) (uint64, uint64, error) { return 0, 0, ErrInspection }
