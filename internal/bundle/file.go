package bundle

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

func ReadFile(ctx context.Context, path string) (Inspection, error) {
	if err := ctx.Err(); err != nil {
		return Inspection{}, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return Inspection{}, err
	}
	if !info.Mode().IsRegular() || info.Size() > MaxArchive {
		return Inspection{}, fmt.Errorf("bundle must be a bounded regular file, not a symlink")
	}
	f, err := os.Open(path)
	if err != nil {
		return Inspection{}, err
	}
	defer f.Close()
	current, err := f.Stat()
	if err != nil {
		return Inspection{}, err
	}
	if !current.Mode().IsRegular() || !os.SameFile(info, current) {
		return Inspection{}, fmt.Errorf("bundle input changed while opening")
	}
	return Read(ctx, f)
}

// Save publishes a fully synced private file using an atomic no-replace link.
// A destination file/symlink/directory is never overwritten. This requires a
// local filesystem supporting hard links; no unsafe rename fallback is used.
func Save(ctx context.Context, path string, b []byte) (err error) {
	if len(b) > MaxArchive {
		return fmt.Errorf("archive too large")
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, ".faultline-evidence-*.tmp")
	if err != nil {
		return err
	}
	temp := f.Name()
	defer func() {
		_ = f.Close()
		if e := os.Remove(temp); e != nil && !os.IsNotExist(e) {
			err = errors.Join(err, fmt.Errorf("temporary file cleanup failed: %w", e))
		}
	}()
	if err = writeBytes(ctx, f, b); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = os.Link(temp, path); err != nil {
		return err
	}
	d, e := os.Open(dir)
	if e != nil {
		return fmt.Errorf("bundle published but directory sync unavailable: %w", e)
	}
	defer d.Close()
	if e = d.Sync(); e != nil {
		return fmt.Errorf("bundle published but directory sync failed: %w", e)
	}
	return nil
}
