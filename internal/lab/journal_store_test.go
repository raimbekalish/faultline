//go:build darwin || linux

package lab

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"
)

func privateStore(t *testing.T) (*JournalStore, string) {
	t.Helper()
	d := t.TempDir()
	if e := os.Chmod(d, 0700); e != nil {
		t.Fatal(e)
	}
	s, e := OpenJournalStore(d)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	return s, d
}
func TestJournalStoreConsumptionUpdatesAndReplay(t *testing.T) {
	ctx := context.Background()
	s, d := privateStore(t)
	j, a := consumed(t)
	j, e := s.Consume(ctx, j.Plan, a)
	if e != nil {
		t.Fatal(e)
	}
	info, e := os.Stat(filepath.Join(d, journalName(j.Plan.Identity.LaunchID, 0)))
	if e != nil || info.Mode().Perm() != 0600 {
		t.Fatal(info, e)
	}
	if _, e = s.Consume(ctx, j.Plan, a); !errors.Is(e, ErrReplay) {
		t.Fatal("replay", e)
	}
	for _, op := range []Operation{Prepare, CleanupBegin, CleanupComplete} {
		j, e = s.Append(ctx, j, a, Event{Operation: op})
		if e != nil {
			t.Fatal(e)
		}
	}
	got, e := s.Load(ctx, j.Plan.Identity.LaunchID)
	if e != nil || !journalsEqual(got, j) {
		t.Fatal(e)
	}
	if s.WriteReceipt(ctx, j, a) != nil || s.WriteReceipt(ctx, j, a) != nil {
		t.Fatal("idempotent exact receipt")
	}
	if _, e = s.Consume(ctx, j.Plan, a); !errors.Is(e, ErrReplay) {
		t.Fatal("cleaned replay", e)
	}
	receipt := filepath.Join(d, j.Plan.Identity.LaunchID+".receipt.json")
	if e = os.WriteFile(receipt, []byte("altered"), 0600); e != nil {
		t.Fatal(e)
	}
	if s.WriteReceipt(ctx, j, a) == nil {
		t.Fatal("overwrote receipt")
	}
}
func TestJournalStoreConcurrentConsumptionAndStaleUpdates(t *testing.T) {
	s, _ := privateStore(t)
	j, a := consumed(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	results := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, e := s.Consume(ctx, j.Plan, a); results <- e }()
	}
	wg.Wait()
	close(results)
	success := 0
	for e := range results {
		if e == nil {
			success++
		} else if !errors.Is(e, ErrReplay) {
			t.Fatal(e)
		}
	}
	if success != 1 {
		t.Fatal(success)
	}
	initial, e := s.Load(ctx, j.Plan.Identity.LaunchID)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Append(ctx, initial, a, Event{Operation: Prepare}); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Append(ctx, initial, a, Event{Operation: CleanupBegin}); !errors.Is(e, ErrStateIO) {
		t.Fatal("stale update", e)
	}
}
func TestJournalStoreFaultsAndCancellation(t *testing.T) {
	s, d := privateStore(t)
	j, a := consumed(t)
	ctx := context.Background()
	c, cancel := context.WithCancel(ctx)
	cancel()
	if _, e := s.Consume(c, j.Plan, a); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	s.save = func(context.Context, *os.Root, string, []byte) error { return errors.New("synthetic disk failure") }
	if _, e := s.Consume(ctx, j.Plan, a); !errors.Is(e, ErrStateIO) {
		t.Fatal(e)
	}
	s.save = publish
	j, e := s.Consume(ctx, j.Plan, a)
	if e != nil {
		t.Fatal(e)
	}
	s.save = func(c context.Context, r *os.Root, n string, b []byte) error {
		if e := publish(c, r, n, b); e != nil {
			return e
		}
		return errors.New("synthetic post-publication sync uncertainty")
	}
	if _, e = s.Append(ctx, j, a, Event{Operation: Prepare}); !errors.Is(e, ErrStateIO) {
		t.Fatal(e)
	}
	// Never infer absence from a reported write error: reload the complete record.
	got, e := s.Load(ctx, j.Plan.Identity.LaunchID)
	if e != nil || len(got.Events) != 1 {
		t.Fatal(e)
	}
	s.save = publish
	if _, e = s.Consume(ctx, j.Plan, a); !errors.Is(e, ErrReplay) {
		t.Fatal(e)
	}
	if _, e = s.Append(c, got, a, Event{Operation: CleanupBegin}); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	next, e := s.Append(context.Background(), got, a, Event{Operation: CleanupBegin})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Append(ctx, next, a, Event{Operation: CleanupComplete}); e != nil {
		t.Fatal(e)
	}
	files, e := os.ReadDir(d)
	if e != nil {
		t.Fatal(e)
	}
	for _, f := range files {
		if bytes.HasPrefix([]byte(f.Name()), []byte(".faultline-plan-")) {
			t.Fatal("temporary publication leak")
		}
	}
}
func TestJournalStoreCorruptionAndFilesystemRefusals(t *testing.T) {
	for _, kind := range []string{"torn", "gap", "symlink", "fifo", "hardlink", "public", "missing-consumption", "foreign-binding"} {
		t.Run(kind, func(t *testing.T) {
			s, d := privateStore(t)
			j, a := consumed(t)
			ctx := context.Background()
			j, e := s.Consume(ctx, j.Plan, a)
			if e != nil {
				t.Fatal(e)
			}
			path := filepath.Join(d, journalName(j.Plan.Identity.LaunchID, 1))
			switch kind {
			case "torn":
				e = os.WriteFile(path, []byte(`{"schema_version":`), 0600)
			case "gap":
				n := advance(t, j, a, Event{Operation: Prepare})
				b, _ := n.Encode()
				e = os.WriteFile(filepath.Join(d, journalName(j.Plan.Identity.LaunchID, 2)), b, 0600)
			case "symlink":
				e = os.Symlink(filepath.Join(d, journalName(j.Plan.Identity.LaunchID, 0)), path)
			case "fifo":
				e = syscall.Mkfifo(path, 0600)
			case "hardlink":
				e = os.Link(filepath.Join(d, journalName(j.Plan.Identity.LaunchID, 0)), path)
			case "public":
				e = os.Chmod(filepath.Join(d, journalName(j.Plan.Identity.LaunchID, 0)), 0644)
			case "missing-consumption":
				n := advance(t, j, a, Event{Operation: Prepare})
				b, _ := n.Encode()
				e = os.WriteFile(path, b, 0600)
				if e == nil {
					e = os.Remove(filepath.Join(d, journalName(j.Plan.Identity.LaunchID, 0)))
				}
			case "foreign-binding":
				n := advance(t, j, a, Event{Operation: Prepare})
				n.Plan.PrivateBinding.SocketInode++
				b, _ := n.Plan.Encode()
				_ = b
				raw, _ := jsonBytes(n)
				e = os.WriteFile(path, raw, 0600)
			}
			if e != nil {
				t.Fatal(e)
			}
			if _, e = s.Load(ctx, j.Plan.Identity.LaunchID); e == nil {
				t.Fatal("corruption accepted")
			}
			if _, e = s.Consume(ctx, j.Plan, a); e == nil {
				t.Fatal("corruption enabled replay")
			}
		})
	}
	s, d := privateStore(t)
	_ = s
	public := filepath.Join(d, "public")
	if os.Mkdir(public, 0755) != nil {
		t.Fatal("mkdir")
	}
	if _, e := OpenJournalStore(public); e == nil {
		t.Fatal("public root")
	}
	link := filepath.Join(d, "link")
	if os.Symlink(d, link) != nil {
		t.Fatal("symlink")
	}
	if _, e := OpenJournalStore(link); e == nil {
		t.Fatal("symlink root")
	}
}

// jsonBytes deliberately bypasses validation to model a hostile/torn disk record.
func jsonBytes(j Journal) ([]byte, error) { return json.Marshal(j) }
func TestJournalGuardRefusalsAndCancellation(t *testing.T) {
	for _, kind := range []string{"symlink", "fifo", "public"} {
		t.Run(kind, func(t *testing.T) {
			s, d := privateStore(t)
			target := filepath.Join(d, "journal.guard")
			var e error
			switch kind {
			case "symlink":
				e = os.Symlink("elsewhere", target)
			case "fifo":
				e = syscall.Mkfifo(target, 0600)
			case "public":
				e = os.WriteFile(target, []byte{}, 0644)
			}
			if e != nil {
				t.Fatal(e)
			}
			j, a := consumed(t)
			if _, e = s.Consume(context.Background(), j.Plan, a); e == nil {
				t.Fatal("unsafe guard accepted")
			}
			if kind == "symlink" {
				if _, e := os.Lstat(filepath.Join(d, "elsewhere")); !errors.Is(e, os.ErrNotExist) {
					t.Fatal("created symlink target", e)
				}
			}
		})
	}
	s, _ := privateStore(t)
	unlock, e := s.lock(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	defer unlock()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e = s.Load(ctx, syntheticPlan(t).Identity.LaunchID); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
}
func TestJournalProcessExitRecovery(t *testing.T) {
	if os.Getenv("FAULTLINE_JOURNAL_HELPER") == "1" {
		s, e := OpenJournalStore(os.Getenv("FAULTLINE_JOURNAL_DIRECTORY"))
		if e != nil {
			os.Exit(31)
		}
		j, a := consumed(t)
		j, e = s.Consume(context.Background(), j.Plan, a)
		if e != nil {
			os.Exit(32)
		}
		for _, event := range []Event{{Operation: Prepare}, {Operation: CreateIntent, Key: Edge}} {
			j, e = s.Append(context.Background(), j, a, event)
			if e != nil {
				os.Exit(33)
			}
		}
		// Retain a torn uncommitted temporary snapshot and exit holding the guard.
		_, e = s.lock(context.Background())
		if e != nil {
			os.Exit(34)
		}
		if os.WriteFile(filepath.Join(os.Getenv("FAULTLINE_JOURNAL_DIRECTORY"), ".faultline-plan-interrupted.tmp"), []byte(`{"torn":`), 0600) != nil {
			os.Exit(35)
		}
		os.Exit(7)
	}
	s, d := privateStore(t)
	cmd := exec.Command(os.Args[0], "-test.run=^TestJournalProcessExitRecovery$")
	cmd.Env = append(os.Environ(), "FAULTLINE_JOURNAL_HELPER=1", "FAULTLINE_JOURNAL_DIRECTORY="+d)
	if e := cmd.Run(); e == nil {
		t.Fatal("helper did not exit")
	} else if x, ok := e.(*exec.ExitError); !ok || x.ExitCode() != 7 {
		t.Fatal(e)
	}
	p := syntheticPlan(t)
	j, e := s.Load(context.Background(), p.Identity.LaunchID)
	if e != nil {
		t.Fatal(e)
	}
	state, _ := j.State()
	if state.Resources[0].State != "create_intent" {
		t.Fatal("interruption guessed away")
	}
	if _, e = s.Consume(context.Background(), p, authorizationFor(p)); !errors.Is(e, ErrReplay) {
		t.Fatal(e)
	}
	if _, e = j.Receipt(authorizationFor(p)); e == nil {
		t.Fatal("invented receipt")
	}
	if _, e = os.Stat(filepath.Join(d, ".faultline-plan-interrupted.tmp")); e != nil {
		t.Fatal("discarded recovery evidence")
	}
}

func TestJournalConsumptionPublicationUncertaintyNeverReplays(t *testing.T) {
	s, _ := privateStore(t)
	j, a := consumed(t)
	s.save = func(c context.Context, r *os.Root, n string, b []byte) error {
		if e := publish(c, r, n, b); e != nil {
			return e
		}
		return errors.New("synthetic fsync uncertainty")
	}
	if _, e := s.Consume(context.Background(), j.Plan, a); !errors.Is(e, ErrStateIO) {
		t.Fatal(e)
	}
	s.save = publish
	if _, e := s.Consume(context.Background(), j.Plan, a); !errors.Is(e, ErrReplay) {
		t.Fatal("uncertain publication replayed", e)
	}
}
func TestJournalLockWaitCancellationAndOverflowSlot(t *testing.T) {
	s, d := privateStore(t)
	j, a := consumed(t)
	j, e := s.Consume(context.Background(), j.Plan, a)
	if e != nil {
		t.Fatal(e)
	}
	unlock, e := s.lock(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	if _, e = s.Load(ctx, j.Plan.Identity.LaunchID); !errors.Is(e, context.DeadlineExceeded) {
		t.Fatal(e)
	}
	unlock()
	if e = os.WriteFile(filepath.Join(d, journalName(j.Plan.Identity.LaunchID, MaxJournalEvents+1)), []byte(`{}`), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Load(context.Background(), j.Plan.Identity.LaunchID); !errors.Is(e, ErrStateIO) {
		t.Fatal("overflow revision ignored", e)
	}
}
func TestJournalWriteFailureStopsSyntheticTransport(t *testing.T) {
	s, _ := privateStore(t)
	j, a := consumed(t)
	ctx := context.Background()
	j, e := s.Consume(ctx, j.Plan, a)
	if e != nil {
		t.Fatal(e)
	}
	j, e = s.Append(ctx, j, a, Event{Operation: Prepare})
	if e != nil {
		t.Fatal(e)
	}
	f := syntheticTransport{}
	s.save = func(context.Context, *os.Root, string, []byte) error { return errors.New("synthetic disk full") }
	next, e := s.Append(ctx, j, a, Event{Operation: CreateIntent, Key: Edge})
	if e == nil {
		_, _ = f.create(Edge)
		j = next
	}
	if len(f.calls) != 0 {
		t.Fatal("creation before durable intent")
	}
	recovered, e := s.Load(ctx, j.Plan.Identity.LaunchID)
	if e != nil || !journalsEqual(j, recovered) {
		t.Fatal("last committed record changed", e)
	}
}
