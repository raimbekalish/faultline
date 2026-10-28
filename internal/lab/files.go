package lab

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

var ErrFile = errors.New("local input/output must use trusted regular files and a private output directory")
var ErrPublish = errors.New("plan publication failed; a complete private plan may already exist")

func openParent(path string, private bool) (*os.Root, string, error) {
	if path == "" || strings.Contains(path, "://") {
		return nil, "", ErrFile
	}
	base := filepath.Base(path)
	if base == "." || base == ".." || base == string(filepath.Separator) {
		return nil, "", ErrFile
	}
	var expected os.FileInfo
	if private {
		var err error
		expected, err = os.Lstat(filepath.Dir(path))
		if err != nil || !expected.IsDir() {
			return nil, "", ErrFile
		}
	}
	r, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return nil, "", ErrFile
	}
	info, err := r.Stat(".")
	if err != nil || !info.IsDir() || (private && (info.Mode().Perm()&0077 != 0 || !ownedDirectory(info) || !os.SameFile(expected, info))) {
		r.Close()
		return nil, "", ErrFile
	}
	return r, base, nil
}

func readDescriptorFile(ctx context.Context, path string) (Descriptor, error) {
	if err := ctx.Err(); err != nil {
		return Descriptor{}, err
	}
	r, base, err := openParent(path, false)
	if err != nil {
		return Descriptor{}, err
	}
	defer r.Close()
	return readDescriptorAt(ctx, r, base)
}

func readDescriptorAt(ctx context.Context, r *os.Root, base string) (Descriptor, error) {
	info, err := r.Lstat(base)
	if err != nil || !info.Mode().IsRegular() || info.Size() > MaxDescriptorBytes {
		return Descriptor{}, ErrFile
	}
	return readDescriptorIdentity(ctx, r, base, info)
}

func readDescriptorIdentity(ctx context.Context, r *os.Root, base string, info os.FileInfo) (Descriptor, error) {
	f, err := openNoFollow(r, base)
	if err != nil {
		return Descriptor{}, ErrFile
	}
	defer f.Close()
	current, err := f.Stat()
	if err != nil || !current.Mode().IsRegular() || current.Size() > MaxDescriptorBytes || !os.SameFile(info, current) {
		return Descriptor{}, ErrFile
	}
	return DecodeDescriptor(ctx, f)
}

// publish uses an opened private directory capability and an atomic no-replace
// hard link. No path-derived recursive cleanup, chmod race or overwrite fallback.
func publish(ctx context.Context, r *os.Root, base string, b []byte) (err error) {
	if len(b) > MaxPlanBytes {
		return ErrPublish
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	var token [16]byte
	if _, err = rand.Read(token[:]); err != nil {
		return ErrPublish
	}
	temp := ".faultline-plan-" + hex.EncodeToString(token[:]) + ".tmp"
	f, err := r.OpenFile(temp, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		return ErrPublish
	}
	defer func() {
		_ = f.Close()
		if e := r.Remove(temp); e != nil {
			err = errors.Join(err, ErrPublish)
		}
	}()
	if n, e := f.Write(b); e != nil || n != len(b) {
		return ErrPublish
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if f.Sync() != nil || f.Close() != nil {
		return ErrPublish
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if r.Link(temp, base) != nil {
		return ErrPublish
	}
	d, e := r.Open(".")
	if e != nil {
		return ErrPublish
	}
	defer d.Close()
	if d.Sync() != nil {
		return ErrPublish
	}
	return nil
}

func writeSummary(out io.Writer, b []byte) error {
	n, err := out.Write(b)
	if err != nil || n != len(b) {
		return errors.New("cannot write plan summary; private plan remains published")
	}
	return nil
}
