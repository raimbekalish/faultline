package lab

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var ErrStateIO = errors.New("private journal unavailable, corrupt, stale or publication unconfirmed")

// JournalStore is a trusted private directory capability, not a user-selected
// recovery namespace for RUN. Future execution must use one fixed consumption
// ledger and a separate runtime slot. No production CLI uses this store yet.
// Updates are immutable numbered snapshots, not replacement of mutable files.
type JournalStore struct {
	root *os.Root
	// Only a test fault-injection seam; never exposed as a production transport.
	save func(context.Context, *os.Root, string, []byte) error
}

func OpenJournalStore(directory string) (*JournalStore, error) {
	// All state path components must already be canonical/trusted; do not follow
	// symlinked generated state components or create directories implicitly.
	absolute, e := filepath.Abs(directory)
	if e != nil {
		return nil, ErrStateIO
	}
	resolved, e := filepath.EvalSymlinks(absolute)
	if e != nil || resolved != absolute {
		return nil, ErrStateIO
	}
	directory = absolute
	// Pin the actual final directory, rejecting a symlinked parent just as plans do.
	r, _, e := openParent(directory+string(os.PathSeparator)+"journal.guard", true)
	if e != nil {
		return nil, ErrStateIO
	}
	return &JournalStore{root: r, save: publish}, nil
}
func (s *JournalStore) Close() error      { return s.root.Close() }
func journalName(id string, n int) string { return fmt.Sprintf("%s.%03d.journal.json", id, n) }
func (s *JournalStore) lock(ctx context.Context) (func(), error) {
	return lockJournal(ctx, s.root)
}
func readJournalFile(ctx context.Context, r *os.Root, name string) (Journal, error) {
	before, e := r.Lstat(name)
	if e != nil {
		return Journal{}, e
	}
	if !privateRegular(before) || before.Size() > MaxJournalBytes {
		return Journal{}, ErrStateIO
	}
	f, e := openNoFollow(r, name)
	if e != nil {
		return Journal{}, ErrStateIO
	}
	defer f.Close()
	now, e := f.Stat()
	if e != nil || !privateRegular(now) || !os.SameFile(before, now) {
		return Journal{}, ErrStateIO
	}
	j, e := DecodeJournal(ctx, f)
	if e != nil {
		return Journal{}, ErrStateIO
	}
	return j, nil
}

// load examines every bounded slot. Missing/truncated/ambiguous committed
// revisions fail closed; only uncommitted temporary files are ignored.
func (s *JournalStore) load(ctx context.Context, id string) (Journal, error) {
	if !launchID.MatchString(id) {
		return Journal{}, ErrStateIO
	}
	if err := s.checkNames(ctx, id); err != nil {
		return Journal{}, err
	}
	var last Journal
	found := false
	gap := false
	for n := 0; n <= MaxJournalEvents; n++ {
		if e := ctx.Err(); e != nil {
			return Journal{}, e
		}
		j, e := readJournalFile(ctx, s.root, journalName(id, n))
		if errors.Is(e, os.ErrNotExist) {
			gap = true
			continue
		}
		if e != nil || gap || len(j.Events) != n || j.Plan.Identity.LaunchID != id {
			return Journal{}, ErrStateIO
		}
		if n > 0 {
			prefix := j
			prefix.Events = append([]Event{}, j.Events[:n-1]...)
			if !journalsEqual(prefix, last) {
				return Journal{}, ErrStateIO
			}
		}
		last = j
		found = true
	}
	if !found {
		return Journal{}, os.ErrNotExist
	}
	return last, nil
}
func (s *JournalStore) Load(ctx context.Context, id string) (Journal, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	unlock, e := s.lock(ctx)
	if e != nil {
		return Journal{}, e
	}
	defer unlock()
	return s.load(ctx, id)
}
func (s *JournalStore) Consume(ctx context.Context, p Plan, a Authorization) (Journal, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if a.validate(p) != nil {
		return Journal{}, ErrAuthorization
	}
	unlock, e := s.lock(ctx)
	if e != nil {
		return Journal{}, e
	}
	defer unlock()
	// Any object at the consumption name blocks RUN, even corrupt or symlinked.
	if _, e = s.root.Lstat(journalName(p.Identity.LaunchID, 0)); !errors.Is(e, os.ErrNotExist) {
		return Journal{}, ErrReplay
	}
	if e = s.checkNames(ctx, p.Identity.LaunchID); e != nil {
		return Journal{}, e
	}
	// A missing first record with later revisions is corruption, not unused authority.
	for n := 1; n <= MaxJournalEvents; n++ {
		if e := ctx.Err(); e != nil {
			return Journal{}, e
		}
		if _, e = s.root.Lstat(journalName(p.Identity.LaunchID, n)); !errors.Is(e, os.ErrNotExist) {
			return Journal{}, ErrStateIO
		}
	}
	j, e := NewJournal(p, a, nil)
	if e != nil {
		return Journal{}, e
	}
	b, e := j.Encode()
	if e != nil {
		return Journal{}, e
	}
	if e = s.save(ctx, s.root, journalName(p.Identity.LaunchID, 0), b); e != nil {
		return Journal{}, fmt.Errorf("%w: consumption publication", ErrStateIO)
	}
	if e = ctx.Err(); e != nil {
		return Journal{}, e
	}
	return j, nil
}
func (s *JournalStore) Append(ctx context.Context, expected Journal, a Authorization, event Event) (Journal, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if a.validate(expected.Plan) != nil || expected.Validate() != nil {
		return Journal{}, ErrAuthorization
	}
	unlock, e := s.lock(ctx)
	if e != nil {
		return Journal{}, e
	}
	defer unlock()
	current, e := s.load(ctx, expected.Plan.Identity.LaunchID)
	if e != nil || !journalsEqual(current, expected) {
		return Journal{}, ErrStateIO
	}
	next, e := current.Advance(a, event)
	if e != nil {
		return Journal{}, e
	}
	b, e := next.Encode()
	if e != nil {
		return Journal{}, e
	}
	if e = s.save(ctx, s.root, journalName(next.Plan.Identity.LaunchID, len(next.Events)), b); e != nil {
		return Journal{}, ErrStateIO
	}
	if e = ctx.Err(); e != nil {
		return Journal{}, e
	}
	return next, nil
}
func (s *JournalStore) WriteReceipt(ctx context.Context, j Journal, a Authorization) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if a.validate(j.Plan) != nil {
		return ErrAuthorization
	}
	unlock, e := s.lock(ctx)
	if e != nil {
		return e
	}
	defer unlock()
	current, e := s.load(ctx, j.Plan.Identity.LaunchID)
	if e != nil || !journalsEqual(current, j) {
		return ErrStateIO
	}
	receipt, e := j.Receipt(a)
	if e != nil {
		return e
	}
	b, e := json.MarshalIndent(receipt, "", "  ")
	if e != nil {
		return ErrStateIO
	}
	name := j.Plan.Identity.LaunchID + ".receipt.json"
	// Idempotent retry can only acknowledge identical, private, regular bytes.
	if info, err := s.root.Lstat(name); err == nil {
		if !privateRegular(info) || info.Size() > MaxJournalBytes {
			return ErrStateIO
		}
		f, err := openNoFollow(s.root, name)
		if err != nil {
			return ErrStateIO
		}
		defer f.Close()
		now, err := f.Stat()
		if err != nil || !os.SameFile(info, now) || !privateRegular(now) {
			return ErrStateIO
		}
		actual, err := io.ReadAll(io.LimitReader(ctxReader{ctx, f}, MaxJournalBytes+1))
		if err != nil || string(actual) != string(append(b, '\n')) {
			return ErrStateIO
		}
		return ctx.Err()
	} else if !errors.Is(err, os.ErrNotExist) {
		return ErrStateIO
	}
	if s.save(ctx, s.root, name, append(b, '\n')) != nil {
		return ErrStateIO
	}
	return ctx.Err()
}

// Bound directory enumeration and reject malformed/out-of-range revision names
// in this launch's namespace. Never traverse or remove discovered entries.
func (s *JournalStore) checkNames(ctx context.Context, id string) error {
	d, e := s.root.Open(".")
	if e != nil {
		return ErrStateIO
	}
	defer d.Close()
	total := 0
	for {
		if e = ctx.Err(); e != nil {
			return e
		}
		names, err := d.Readdirnames(64)
		total += len(names)
		if total > 4096 {
			return ErrStateIO
		}
		for _, name := range names {
			if strings.HasPrefix(name, id+".") && strings.HasSuffix(name, ".journal.json") {
				valid := false
				for n := 0; n <= MaxJournalEvents; n++ {
					if name == journalName(id, n) {
						valid = true
						break
					}
				}
				if !valid {
					return ErrStateIO
				}
			}
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return ErrStateIO
		}
	}
}
