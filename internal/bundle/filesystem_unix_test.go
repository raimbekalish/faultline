//go:build darwin || linux

package bundle

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestRejectNonRegularInputs(t *testing.T) {
	dir := t.TempDir()
	fifo := filepath.Join(dir, "pipe.flb")
	if err := syscall.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{dir, fifo} {
		if _, err := ReadFile(context.Background(), path); err == nil {
			t.Fatal("accepted nonregular input", path)
		}
	}
	link := filepath.Join(dir, "missing.flb")
	if err := os.Symlink(filepath.Join(dir, "absent"), link); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadFile(context.Background(), link); err == nil {
		t.Fatal("accepted dangling symlink")
	}
}
