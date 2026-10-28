//go:build darwin || linux

package lab

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"syscall"
	"time"
)

func privateRegular(info os.FileInfo) bool {
	st, ok := info.Sys().(*syscall.Stat_t)
	return ok && info.Mode().IsRegular() && info.Mode().Perm() == 0600 && st.Uid == uint32(os.Geteuid()) && st.Nlink == 1
}

// The stable guard is intentionally retained. Unlinking it would permit split
// locks. Kernel flock release on process exit is tested on the host filesystem.
func lockJournal(ctx context.Context, r *os.Root) (func(), error) {
	return lockNamed(ctx, r, "journal.guard")
}
func lockNamed(ctx context.Context, r *os.Root, name string) (func(), error) {
	if name != "journal.guard" && !(len(name) == 78 && strings.HasPrefix(name, "runtime-") && strings.HasSuffix(name, ".guard") && digest.MatchString(name[8:72])) {
		return nil, ErrStateIO
	}
	if e := ctx.Err(); e != nil {
		return nil, e
	}

	// Create exclusively, then open an existing guard without O_CREATE. Root's
	// symlink-safe resolution may otherwise follow an in-root terminal link;
	// it must never create that link's missing target as a side effect.
	var f *os.File
	var e error
	for attempt := 0; attempt < 16; attempt++ {
		if e = ctx.Err(); e != nil {
			return nil, e
		}
		f, e = r.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_RDWR|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0600)
		if e == nil {
			break
		}
		if errors.Is(e, os.ErrExist) {
			before, err := r.Lstat(name)
			if err != nil || !privateRegular(before) {
				return nil, ErrStateIO
			}
			f, e = r.OpenFile(name, os.O_RDWR|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
			if e == nil {
				actual, err := f.Stat()
				if err != nil || !privateRegular(actual) || !os.SameFile(before, actual) {
					f.Close()
					return nil, ErrStateIO
				}
				break
			}
		}
		// Darwin can transiently return ENOENT during simultaneous creation.
		// Retry only this absent guard; no unlink, chmod or replacement fallback.
		if !errors.Is(e, os.ErrNotExist) {
			return nil, ErrStateIO
		}
	}
	if e != nil {
		return nil, ErrStateIO
	}
	fail := func(stage string) (func(), error) {
		f.Close()
		return nil, fmt.Errorf("%w: guard %s", ErrStateIO, stage)
	}
	info, e := f.Stat()
	if e != nil || !privateRegular(info) {
		return fail("metadata")
	}
	for {
		if e = ctx.Err(); e != nil {
			f.Close()
			return nil, e
		}
		e = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if e == nil {
			break
		}
		if errors.Is(e, syscall.EINTR) {
			continue
		}
		if !errors.Is(e, syscall.EWOULDBLOCK) && !errors.Is(e, syscall.EAGAIN) {
			return fail("flock")
		}
		select {
		case <-ctx.Done():
			f.Close()
			return nil, ctx.Err()
		case <-time.After(5 * time.Millisecond):
		}
	}
	latest, e := r.Lstat(name)
	if e != nil || !privateRegular(latest) || !os.SameFile(info, latest) {
		syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		return fail("identity")
	}
	return func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN); _ = f.Close() }, nil
}
