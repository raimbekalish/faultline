//go:build darwin || linux

package lab

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
)

func TestDescriptorSymlinkFIFOAndReplacement(t *testing.T) {
	dir := privateDir(t)
	path := filepath.Join(dir, "app.json")
	mustWrite(t, path, []byte(goodDescriptor))
	link := filepath.Join(dir, "link.json")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	fifo := filepath.Join(dir, "fifo")
	if err := syscall.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	large := filepath.Join(dir, "large")
	mustWrite(t, large, bytes.Repeat([]byte{' '}, MaxDescriptorBytes+1))
	for _, bad := range []string{dir, link, fifo, large, filepath.Join(dir, "missing"), "https://example.invalid/file"} {
		if _, err := readDescriptorFile(context.Background(), bad); err == nil {
			t.Fatal("unsafe input accepted", bad)
		}
	}
	r, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	before, err := r.Lstat("app.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path, path+".old"); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, path, []byte(goodDescriptor))
	if _, err := readDescriptorIdentity(context.Background(), r, "app.json", before); err == nil {
		t.Fatal("changed inode accepted")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(path+".old", path); err != nil {
		t.Fatal(err)
	}
	if _, err := readDescriptorIdentity(context.Background(), r, "app.json", before); err == nil {
		t.Fatal("substituted symlink followed")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(path, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readDescriptorIdentity(context.Background(), r, "app.json", before); err == nil {
		t.Fatal("substituted FIFO read")
	}
}

func TestAtomicPublicationLimitsCancellationAndExistingObjects(t *testing.T) {
	dir := privateDir(t)
	r, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	mustWrite(t, filepath.Join(dir, "original"), []byte("DO NOT CHANGE"))
	if err := r.Symlink("original", "link"); err != nil {
		t.Fatal(err)
	}
	if err := r.Mkdir("directory", 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"original", "link", "directory"} {
		if err := publish(context.Background(), r, name, []byte("new")); err == nil {
			t.Fatal("overwrite", name)
		}
	}
	before, _ := os.ReadFile(filepath.Join(dir, "original"))
	if string(before) != "DO NOT CHANGE" {
		t.Fatal("original overwritten")
	}
	if err := publish(context.Background(), r, "oversized", make([]byte, MaxPlanBytes+1)); err == nil {
		t.Fatal("output size cap")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := publish(ctx, r, "canceled", []byte("new")); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := publish(&cancelChecks{Context: context.Background()}, r, "late-canceled", []byte("new")); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 3 {
		t.Fatal("temporary/partial publication leaked", entries)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Go(func() { results <- publish(context.Background(), r, "winner", []byte("complete")) })
	}
	wg.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
		}
	}
	if success != 1 {
		t.Fatal("no-replace race", success)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "winner"))
	if string(b) != "complete" {
		t.Fatal("partial publication")
	}
	entries, _ = os.ReadDir(dir)
	if len(entries) != 4 {
		t.Fatal("temp leak", entries)
	}
	nonprivate := filepath.Join(dir, "shared")
	if err := os.Mkdir(nonprivate, 0755); err != nil {
		t.Fatal(err)
	}
	if root, _, err := openParent(filepath.Join(nonprivate, "plan"), true); err == nil {
		root.Close()
		t.Fatal("public directory accepted")
	}
	if err := os.Symlink(dir, filepath.Join(dir, "parent-link")); err != nil {
		t.Fatal(err)
	}
	if root, _, err := openParent(filepath.Join(dir, "parent-link", "plan"), true); err == nil {
		root.Close()
		t.Fatal("symlinked output parent accepted")
	}
}

type cancelChecks struct {
	context.Context
	n int
}

func (c *cancelChecks) Err() error {
	c.n++
	if c.n >= 3 {
		return context.Canceled
	}
	return nil
}

func TestActualUnixSocketIdentityAndNoRegularSocketSubstitute(t *testing.T) {
	dir, err := os.MkdirTemp(os.TempDir(), "s-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	path := filepath.Join(dir, "d.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	s, err := inspectSocket("unix://" + path)
	if err != nil || s.Inode == 0 {
		t.Fatal(s, err)
	}
	alias := path + ".alias"
	if err := os.Symlink(path, alias); err != nil {
		t.Fatal(err)
	}
	a, err := inspectSocket("unix://" + alias)
	if err != nil || a != s {
		t.Fatal("socket canonicalization", a, s, err)
	}
	regular := path + ".file"
	mustWrite(t, regular, []byte("not a socket"))
	if _, err := inspectSocket("unix://" + regular); err == nil {
		t.Fatal("regular file endpoint")
	}
}

func TestFailedPlanNeverPublishesOrCreatesJournals(t *testing.T) {
	dir := privateDir(t)
	app := filepath.Join(dir, "app.json")
	out := filepath.Join(dir, "plan.json")
	for _, invalid := range []string{"{}", goodDescriptor + "{}", strings.Replace(goodDescriptor, "linux/arm64", "linux/amd64", 1)} {
		mustWrite(t, app, []byte(invalid))
		f := &recordingReader{}
		if _, err := create(context.Background(), app, out, testInspector(f), rand.Reader); err == nil || len(f.calls) != 0 {
			t.Fatal("inspection before descriptor validation", err)
		}
	}
	mustWrite(t, app, []byte(goodDescriptor))
	ctx, cancel := context.WithCancel(context.Background())
	f := &recordingReader{change: func(_ readCommand, _ []byte) ([]byte, error) { cancel(); return nil, ctx.Err() }}
	if _, err := create(ctx, app, out, testInspector(f), rand.Reader); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatal("state created on error", entries)
	}
}
