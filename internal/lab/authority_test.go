package lab

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func authorityFixture(t *testing.T) (*Authority, string) {
	t.Helper()
	home := t.TempDir()
	if os.Chmod(home, 0700) != nil {
		t.Fatal("private home")
	}
	if e := initializeAuthorityAt(context.Background(), home); e != nil {
		t.Fatal(e)
	}
	a, e := openAuthorityAt(context.Background(), home)
	if e != nil {
		t.Fatal(e)
	}
	a.probeRuntime = func(_ context.Context, p Plan) (runtimeWitness, error) { return fakeWitness(p), nil }
	t.Cleanup(func() { a.Close() })
	return a, home
}
func randomPlan(t *testing.T) Plan {
	t.Helper()
	p, e := buildPlan(context.Background(), descriptor(t), testSnapshot(t), rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	return p
}
func fakeWitness(p Plan) runtimeWitness {
	return runtimeWitness{hash([]byte("faultline-runtime-v1\x00" + privateDaemon)), authorizationFor(p)}
}
func TestAuthorityFixedAccountSelection(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("FAULTLINE_STATE_DIRECTORY", t.TempDir())
	h, e := authoritativeHome()
	if e == nil && h == os.Getenv("HOME") {
		t.Fatal("environment selected authority root")
	}
	// Account lookup may fail closed in CGO-disabled Darwin tests; no env fallback.
	if e != nil && !errors.Is(e, ErrAuthority) {
		t.Fatal(e)
	}
}
func TestAuthorityConsumptionCannotUseEmptyJournalDirectory(t *testing.T) {
	a, home := authorityFixture(t)
	p := randomPlan(t)
	ctx := context.Background()
	l, e := a.begin(ctx, p, fakeWitness(p), false)
	if e != nil {
		t.Fatal(e)
	}
	l.Close()
	elsewhere := t.TempDir()
	os.Chmod(elsewhere, 0700)
	if _, e = openAuthorityAt(ctx, elsewhere); e == nil {
		t.Fatal("alternate empty directory adopted")
	}
	if _, e = a.begin(ctx, p, fakeWitness(p), false); !errors.Is(e, ErrReplay) {
		t.Fatal(e)
	}
	if e = initializeAuthorityAt(ctx, home); !errors.Is(e, ErrAuthority) {
		t.Fatal("reset allowed", e)
	}
	newPlan := randomPlan(t)
	if _, e = a.begin(ctx, newPlan, fakeWitness(newPlan), false); !errors.Is(e, ErrLease) {
		t.Fatal("unresolved reservation released", e)
	}
}
func TestAuthorityConcurrentLeaseAndCleanupRelease(t *testing.T) {
	a, _ := authorityFixture(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	leases := make(chan *TrustedLease, 8)
	errs := make(chan error, 8)
	for n := 0; n < 8; n++ {
		p := randomPlan(t)
		wg.Add(1)
		go func() {
			defer wg.Done()
			l, e := a.begin(ctx, p, fakeWitness(p), false)
			if e != nil {
				errs <- e
			} else {
				leases <- l
			}
		}()
	}
	wg.Wait()
	close(leases)
	close(errs)
	var winner *TrustedLease
	count := 0
	for l := range leases {
		winner = l
		count++
	}
	if count != 1 {
		t.Fatal("multiple leases", count)
	}
	defer winner.Close()
	for e := range errs {
		if !errors.Is(e, ErrLease) {
			t.Fatal(e)
		}
	}
	for _, op := range []Operation{Prepare, CleanupBegin, CleanupComplete} {
		if e := winner.Append(ctx, Event{Operation: op}); e != nil {
			t.Fatal(e)
		}
	}
	if e := winner.ConfirmCleanup(ctx); e != nil {
		t.Fatal(e)
	}
	old := winner.journal.Plan
	winner.Close()
	if _, e := a.begin(ctx, old, fakeWitness(old), false); !errors.Is(e, ErrReplay) {
		t.Fatal("cleaned replay", e)
	}
	p := randomPlan(t)
	next, e := a.begin(ctx, p, fakeWitness(p), false)
	if e != nil {
		t.Fatal(e)
	}
	next.Close()
}
func TestAuthorityMissingCorruptAndSubstitutedState(t *testing.T) {
	for _, target := range []string{"authority.json", "authority.head.json", "registry.000.json", "anchor", "journal"} {
		t.Run(target, func(t *testing.T) {
			a, home := authorityFixture(t)
			p := randomPlan(t)
			l, e := a.begin(context.Background(), p, fakeWitness(p), false)
			if e != nil {
				t.Fatal(e)
			}
			l.Close()
			name := target
			if target == "journal" {
				name = journalName(p.Identity.LaunchID, 0)
			}
			path := filepath.Join(home, authorityDirectory, name)
			if target == "anchor" {
				path = filepath.Join(home, enrollmentName)
			}
			if os.Remove(path) != nil {
				t.Fatal("remove fixture")
			}
			p2 := randomPlan(t)
			if _, e = a.begin(context.Background(), p2, fakeWitness(p2), false); e == nil {
				t.Fatal("missing state fresh authority")
			}
			if initializeAuthorityAt(context.Background(), home) == nil {
				t.Fatal("missing state re-enrolled")
			}
		})
	}
	for _, content := range []string{`{}`, `{"sequence":0,"sequence":1}`, strings.Repeat("x", 2049)} {
		a, home := authorityFixture(t)
		if os.WriteFile(filepath.Join(home, authorityDirectory, "authority.head.json"), []byte(content), 0600) != nil {
			t.Fatal("write")
		}
		if _, _, e := a.registry(context.Background()); e == nil {
			t.Fatal("corrupt checkpoint")
		}
	}
}
func TestAuthorityReservationUncertaintyAndMissingJournal(t *testing.T) {
	a, _ := authorityFixture(t)
	p := randomPlan(t)
	a.store.save = func(context.Context, *os.Root, string, []byte) error { return errors.New("synthetic disk failure") }
	if _, e := a.begin(context.Background(), p, fakeWitness(p), false); e == nil {
		t.Fatal("missing journal success")
	}
	a.store.save = publish
	if _, e := a.begin(context.Background(), p, fakeWitness(p), false); !errors.Is(e, ErrReplay) {
		t.Fatal("replayed uncertain consumption", e)
	}
	p2 := randomPlan(t)
	if _, e := a.begin(context.Background(), p2, fakeWitness(p2), false); !errors.Is(e, ErrAuthority) {
		t.Fatal("missing journal fresh run", e)
	}
}
func TestLegacyJournalImportPreservesIdentity(t *testing.T) {
	a, _ := authorityFixture(t)
	j, auth := consumed(t)
	j = createAll(t, j, auth)
	l, e := a.importVerified(context.Background(), j, fakeWitness(j.Plan))
	if e != nil {
		t.Fatal(e)
	}
	defer l.Close()
	if !journalsEqual(l.journal, j) || !l.cleanupOnly {
		t.Fatal("legacy mutated")
	}
	if e := l.Append(context.Background(), Event{Operation: WorkerIntent}); !errors.Is(e, ErrAuthority) {
		t.Fatal("import granted RUN", e)
	}
	l.Close()
	if _, e = a.begin(context.Background(), j.Plan, fakeWitness(j.Plan), false); !errors.Is(e, ErrReplay) {
		t.Fatal(e)
	}
}
func TestCleanupOnlyLeaseCannotReenableRun(t *testing.T) {
	a, _ := authorityFixture(t)
	p := randomPlan(t)
	ctx := context.Background()
	l, e := a.begin(ctx, p, fakeWitness(p), false)
	if e != nil {
		t.Fatal(e)
	}
	if l.Append(ctx, Event{Operation: Prepare}) != nil {
		t.Fatal("prepare")
	}
	l.Close()
	recovery, e := a.begin(ctx, p, fakeWitness(p), true)
	if e != nil {
		t.Fatal(e)
	}
	defer recovery.Close()
	if recovery.Append(ctx, Event{Operation: CreateIntent, Key: Edge}) == nil {
		t.Fatal("cleanup granted create")
	}
	if recovery.ConfirmCleanup(ctx) == nil {
		t.Fatal("unresolved cleanup released")
	}
}

func TestAuthorityCheckpointUncertaintyAndLostReceipt(t *testing.T) {
	a, home := authorityFixture(t)
	ctx := context.Background()
	p := randomPlan(t)
	l, e := a.begin(ctx, p, fakeWitness(p), false)
	if e != nil {
		t.Fatal(e)
	}
	defer l.Close()
	for _, op := range []Operation{Prepare, CleanupBegin, CleanupComplete} {
		if e = l.Append(ctx, Event{Operation: op}); e != nil {
			t.Fatal(e)
		}
	}
	if e = l.ConfirmCleanup(ctx); e != nil {
		t.Fatal(e)
	}
	l.Close()
	if e = os.Remove(filepath.Join(home, authorityDirectory, p.Identity.LaunchID+".receipt.json")); e != nil {
		t.Fatal(e)
	}
	p2 := randomPlan(t)
	if _, e = a.begin(ctx, p2, fakeWitness(p2), false); !errors.Is(e, ErrAuthority) {
		t.Fatal("lost receipt fresh execution", e)
	}
	a2, _ := authorityFixture(t)
	unlock, e := a2.store.lock(ctx)
	if e != nil {
		t.Fatal(e)
	}
	h, _, e := a2.registry(ctx)
	if e != nil {
		t.Fatal(e)
	}
	record := registryRecord{Sequence: h.Sequence + 1, Previous: h.SHA256, Authority: a2.seal.Nonce, Action: "reserve", LaunchID: p.Identity.LaunchID, PlanSHA256: p.AuthorizationSHA256, RuntimeKey: fakeWitness(p).key}
	b, _ := json.Marshal(record)
	if publish(ctx, a2.store.root, registryName(record.Sequence), b) != nil {
		t.Fatal("fixture")
	}
	unlock()
	if _, _, e = a2.registry(ctx); !errors.Is(e, ErrAuthority) {
		t.Fatal("uncheckpointed registry ignored", e)
	}
}
func TestAuthorityCancellationDoesNotEnrollOrInspect(t *testing.T) {
	a, _ := authorityFixture(t)
	p := randomPlan(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := a.Begin(ctx, p, p.AuthorizationSHA256); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	if _, e := a.begin(ctx, p, fakeWitness(p), false); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
}

func TestLiveSupervisorCannotBeDisplacedByCleanupRecovery(t *testing.T) {
	a, _ := authorityFixture(t)
	p := randomPlan(t)
	l, e := a.begin(context.Background(), p, fakeWitness(p), false)
	if e != nil {
		t.Fatal(e)
	}
	defer l.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if other, e := a.begin(ctx, p, fakeWitness(p), true); other != nil || !errors.Is(e, context.DeadlineExceeded) {
		if other != nil {
			other.Close()
		}
		t.Fatal("unbounded/displaced live lease", e)
	}
}
