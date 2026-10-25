package history

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/raimbekalish/faultline/internal/experiment"
	"github.com/raimbekalish/faultline/internal/spec"
)

func testSpec() spec.Spec {
	return spec.FromConfig(experiment.Config{Samples: 1, LatencyMS: 80, RequestTimeoutMS: 1000})
}
func testReport(status string, n int) experiment.Report {
	s := experiment.Summary{Requests: 1, MinMS: float64(n), MeanMS: float64(n), P50MS: float64(n), P95MS: float64(n), P99MS: float64(n), MaxMS: float64(n)}
	p := experiment.Phase{Samples: []experiment.Sample{{DurationMS: float64(n), Status: 200}}, Summary: s}
	r := experiment.Report{SchemaVersion: 1, RunID: fmt.Sprintf("faultline-%032x", n), Status: status, Config: testSpec().Config(), Target: experiment.Target, StartedAt: time.Now().UTC().Add(-time.Second), FinishedAt: time.Now().UTC(), Baseline: p, Fault: p, Recovery: p, CleanupComplete: true}
	if status != "complete" {
		r.Error = "fixture failure"
	}
	return r
}
func newStore(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "history.sqlite")
	s, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s, path
}

func TestMigrationsOwnershipAndPragmas(t *testing.T) {
	s, path := newStore(t)
	for query, want := range map[string]int{"PRAGMA user_version": 1, "PRAGMA application_id": applicationID, "PRAGMA synchronous": 3, "PRAGMA foreign_keys": 1, "PRAGMA busy_timeout": 500} {
		var got int
		if err := s.db.QueryRow(query).Scan(&got); err != nil || got != want {
			t.Fatalf("%s = %d: %v", query, got, err)
		}
	}
	var journal string
	_ = s.db.QueryRow("PRAGMA journal_mode").Scan(&journal)
	if journal != "delete" {
		t.Fatal(journal)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatalf("mode %o", info.Mode().Perm())
	}
	s.Close()
	s, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.db.Exec("PRAGMA user_version=99"); err != nil {
		t.Fatal(err)
	}
	if other, err := Open(context.Background(), path); err == nil {
		other.Close()
		t.Fatal("future schema accepted")
	}
	var version int
	_ = s.db.QueryRow("PRAGMA user_version").Scan(&version)
	if version != 99 {
		t.Fatal("future version overwritten")
	}
	foreign := filepath.Join(t.TempDir(), "foreign.sqlite")
	db, err := sql.Open("sqlite3", foreign)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec("CREATE TABLE unrelated(value INTEGER); INSERT INTO unrelated VALUES(42)"); err != nil {
		t.Fatal(err)
	}
	if other, err := Open(context.Background(), foreign); err == nil {
		other.Close()
		t.Fatal("foreign database accepted")
	}
	var value int
	_ = db.QueryRow("SELECT value FROM unrelated").Scan(&value)
	if value != 42 {
		t.Fatal("foreign data changed")
	}
}

func TestMigrationRollback(t *testing.T) {
	db, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "migration.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := migrate(context.Background(), db, []string{"CREATE TABLE partial(value INTEGER); INVALID SQL"}); err == nil {
		t.Fatal("invalid migration accepted")
	}
	for _, q := range []string{"PRAGMA user_version", "PRAGMA application_id", "SELECT count(*) FROM sqlite_schema WHERE name='partial'"} {
		var n int
		if err := db.QueryRow(q).Scan(&n); err != nil || n != 0 {
			t.Fatalf("migration left changes: %s %d %v", q, n, err)
		}
	}
}

func TestHistoryLifecycleComparisonAndAliases(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	a, err := s.Start(ctx, testSpec())
	if err != nil {
		t.Fatal(err)
	}
	entry, err := s.Get(ctx, a)
	if err != nil || entry.Status != "running" || entry.Report != nil {
		t.Fatalf("intent: %+v %v", entry, err)
	}
	if err := s.Finish(ctx, a, testReport("complete", 1)); err != nil {
		t.Fatal(err)
	}
	if err := s.Finish(ctx, a, testReport("complete", 2)); !errors.Is(err, ErrFinalized) {
		t.Fatalf("overwrite accepted: %v", err)
	}
	byAlias, err := s.Get(ctx, testReport("complete", 1).RunID)
	if err != nil || byAlias.ID != a {
		t.Fatalf("alias: %+v %v", byAlias, err)
	}
	b, _ := s.Start(ctx, testSpec())
	if err := s.Finish(ctx, b, testReport("complete", 3)); err != nil {
		t.Fatal(err)
	}
	comparison, err := s.Compare(ctx, a, b)
	if err != nil || !comparison.SameSpec || comparison.Phases[0].Delta.MeanDeltaMS != 2 {
		t.Fatalf("compare: %+v %v", comparison, err)
	}
	list, err := s.List(ctx, 1)
	if err != nil || len(list) != 1 || list[0].ID != b || list[0].Report != nil {
		t.Fatalf("list: %+v %v", list, err)
	}
	unfinished, _ := s.Start(ctx, testSpec())
	if _, err := s.Compare(ctx, a, unfinished); err == nil {
		t.Fatal("unfinished comparison accepted")
	}
	if _, err := s.List(ctx, 101); err == nil {
		t.Fatal("unbounded listing")
	}
}

func TestFailedCanceledAndTraceReferences(t *testing.T) {
	for _, status := range []string{"failed", "canceled"} {
		t.Run(status, func(t *testing.T) {
			s, _ := newStore(t)
			id, _ := s.Start(context.Background(), testSpec())
			r := testReport(status, 1)
			r.Baseline.Samples[0].TraceID = strings.Repeat("a", 32)
			r.Baseline.Samples[0].SpanID = strings.Repeat("b", 16)
			if err := s.Finish(context.Background(), id, r); err != nil {
				t.Fatal(err)
			}
			e, err := s.Get(context.Background(), id)
			if err != nil || e.Status != status || !strings.Contains(string(e.Report), r.Baseline.Samples[0].TraceID) {
				t.Fatalf("lost report: %+v %v", e, err)
			}
			if _, err := s.Compare(context.Background(), id, id); err == nil {
				t.Fatal("failed comparison accepted")
			}
		})
	}
}

func TestTransactionRollbackAndWriteFailures(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	id, _ := s.Start(ctx, testSpec())
	r := testReport("complete", 1)
	if _, err := s.db.Exec("CREATE TRIGGER reject_report BEFORE UPDATE ON runs BEGIN SELECT RAISE(ABORT,'injected persistence failure'); END"); err != nil {
		t.Fatal(err)
	}
	if err := s.Finish(ctx, id, r); err == nil {
		t.Fatal("write failure hidden")
	}
	e, err := s.Get(ctx, id)
	if err != nil || e.Status != "running" || e.Report != nil {
		t.Fatalf("partial commit: %+v %v", e, err)
	}
	_, _ = s.db.Exec("DROP TRIGGER reject_report")
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if err := s.Finish(canceled, id, r); err == nil {
		t.Fatal("canceled write accepted")
	}
	if _, err := s.Start(canceled, testSpec()); err == nil {
		t.Fatal("canceled intent accepted")
	}
	_, _ = s.db.Exec("PRAGMA query_only=ON")
	if err := s.Finish(ctx, id, r); err == nil {
		t.Fatal("read-only write accepted")
	}
	_, _ = s.db.Exec("PRAGMA query_only=OFF")
	if err := s.Finish(ctx, id, r); err != nil {
		t.Fatal(err)
	}
}

func TestMissingMalformedAndPaths(t *testing.T) {
	s, path := newStore(t)
	ctx := context.Background()
	if _, err := s.Get(ctx, "history-"+strings.Repeat("f", 32)); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := OpenExisting(ctx, filepath.Join(t.TempDir(), "missing.sqlite")); !os.IsNotExist(err) {
		t.Fatalf("missing database: %v", err)
	}
	id, _ := s.Start(ctx, testSpec())
	r := testReport("complete", 1)
	if err := s.Finish(ctx, id, r); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec("UPDATE runs SET report_json='{}' WHERE id=?", id); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(ctx, id); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("malformed report accepted: %v", err)
	}
	link := filepath.Join(t.TempDir(), "link.sqlite")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if other, err := Open(ctx, link); err == nil {
		other.Close()
		t.Fatal("symlink accepted")
	}
	special, err := Open(ctx, filepath.Join(t.TempDir(), "history ?#.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer special.Close()
	if _, err := special.Start(ctx, testSpec()); err != nil {
		t.Fatal(err)
	}
}

func TestMalformedSummaryAndPayloadByteLimits(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	id, _ := s.Start(ctx, testSpec())
	r := testReport("failed", 1)
	r.Baseline.Summary.MeanMS = -1
	if err := s.Finish(ctx, id, r); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("invalid summary accepted: %v", err)
	}
	r = testReport("failed", 1)
	r.Error = strings.Repeat("é", 600000)
	if err := s.Finish(ctx, id, r); err == nil {
		t.Fatal("oversized byte payload accepted")
	}
	if err := s.Finish(ctx, id, testReport("failed", 1)); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(r)
	if _, err := s.db.Exec("UPDATE runs SET report_json=? WHERE id=?", string(b), id); err == nil {
		t.Fatal("SQL byte constraint did not reject oversized payload")
	}
	_, _ = s.db.Exec("PRAGMA ignore_check_constraints=ON")
	if _, err := s.db.Exec("UPDATE runs SET report_json=? WHERE id=?", string(b), id); err != nil {
		t.Fatal(err)
	}
	_, _ = s.db.Exec("PRAGMA ignore_check_constraints=OFF")
	if _, err := s.Get(ctx, id); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("corrupt oversized record accepted: %v", err)
	}
}

func TestBusyWriteIsBounded(t *testing.T) {
	s, path := newStore(t)
	ctx := context.Background()
	id, _ := s.Start(ctx, testSpec())
	other, err := OpenExisting(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	tx, err := other.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	// Query connections use deferred transactions. Acquire a real writer lock
	// explicitly instead of relying on BeginTx to reserve one for a reader.
	if _, err := tx.ExecContext(ctx, "UPDATE runs SET status=status WHERE id=?", id); err != nil {
		t.Fatal(err)
	}
	deadline, cancel := context.WithTimeout(ctx, 30*time.Millisecond)
	defer cancel()
	start := time.Now()
	err = s.Finish(deadline, id, testReport("complete", 1))
	if err == nil || time.Since(start) > 3*time.Second {
		t.Fatalf("unbounded/hidden lock failure: %v", err)
	}
	_ = tx.Rollback()
	e, err := s.Get(ctx, id)
	if err != nil || e.Status != "running" {
		t.Fatalf("lock failure changed intent: %+v %v", e, err)
	}
}

func TestAbruptProcessExitRollsBackJournal(t *testing.T) {
	s, path := newStore(t)
	ctx := context.Background()
	id, _ := s.Start(ctx, testSpec())
	committed, _ := s.Start(ctx, testSpec())
	if err := s.Finish(ctx, committed, testReport("complete", 1)); err != nil {
		t.Fatal(err)
	}
	s.Close()
	deadline, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(deadline, os.Args[0], "-test.run=^TestCrashHelper$")
	cmd.Env = append(os.Environ(), "FAULTLINE_HISTORY_CRASH_PATH="+path, "FAULTLINE_HISTORY_CRASH_ID="+id)
	output, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 23 {
		t.Fatalf("helper: %v %s", err, output)
	}
	journal, err := os.ReadFile(path + "-journal")
	if err != nil || len(journal) < 512 || string(journal[:8]) == strings.Repeat("\x00", 8) {
		t.Fatalf("no hot journal to recover: %v bytes=%d", err, len(journal))
	}
	recovered, err := OpenExisting(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer recovered.Close()
	e, err := recovered.Get(ctx, id)
	if err != nil || e.Status != "running" || e.Report != nil {
		t.Fatalf("uncommitted update survived: %+v %v", e, err)
	}
	e, err = recovered.Get(ctx, committed)
	if err != nil || e.Status != "complete" {
		t.Fatalf("committed data lost: %+v %v", e, err)
	}
}

func TestCrashHelper(t *testing.T) {
	path := os.Getenv("FAULTLINE_HISTORY_CRASH_PATH")
	if path == "" {
		t.Skip("subprocess fixture")
	}
	s, err := OpenExisting(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec("PRAGMA cache_size=1; PRAGMA cache_spill=ON"); err != nil {
		t.Fatal(err)
	}
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	r := testReport("failed", 2)
	r.Error = strings.Repeat("x", 128<<10)
	b, _ := json.Marshal(r)
	_, err = tx.Exec("UPDATE runs SET status='failed',engine_run_id=?,report_json=?,finished_at=? WHERE id=?", r.RunID, string(b), time.Now().UTC().Format(timestampLayout), os.Getenv("FAULTLINE_HISTORY_CRASH_ID"))
	if err != nil {
		t.Fatal(err)
	}
	os.Exit(23) // Intentionally bypass all defers/SQLite close with an open write.
}
