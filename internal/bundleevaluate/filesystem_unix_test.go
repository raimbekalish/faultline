//go:build darwin || linux

package bundleevaluate

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestNoFollowAndSpecialFileRejection(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "policy")
	if err := os.WriteFile(path, []byte(validPolicy), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if f, err := openPolicy(link); err == nil {
		f.Close()
		t.Fatal("final symlink followed after the initial identity check")
	}
	fifo := filepath.Join(dir, "fifo")
	if err := syscall.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadFile(context.Background(), fifo); err == nil {
		t.Fatal("FIFO accepted")
	}
}
