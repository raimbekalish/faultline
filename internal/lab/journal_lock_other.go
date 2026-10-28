//go:build !darwin && !linux

package lab

import (
	"context"
	"os"
)

func privateRegular(os.FileInfo) bool                       { return false }
func lockJournal(context.Context, *os.Root) (func(), error) { return nil, ErrStateIO }
