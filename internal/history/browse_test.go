package history

import (
	"context"
	"errors"
	"testing"
)

func TestBrowseSnapshotPaginationAndFilters(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	ids := []string{}
	for i, status := range []string{"complete", "failed", "canceled", "running", "complete"} {
		id, err := s.Start(ctx, testSpec())
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
		if status != "running" {
			if err = s.Finish(ctx, id, testReport(status, i+1)); err != nil {
				t.Fatal(err)
			}
		}
	}
	// Equal timestamps exercise the ID tie-breaker rather than clock resolution.
	if _, err := s.db.Exec("UPDATE runs SET created_at='2026-01-01T00:00:00.000000000Z'"); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	cursor := ""
	for {
		p, err := s.Browse(ctx, Filter{Limit: 2, Cursor: cursor})
		if err != nil {
			t.Fatal(err)
		}
		if p.Total != 5 || p.Counts != (Counts{Complete: 2, Failed: 1, Canceled: 1, Running: 1}) {
			t.Fatal(p)
		}
		for _, e := range p.Items {
			if seen[e.ID] {
				t.Fatal("duplicate page item")
			}
			seen[e.ID] = true
			if e.Status != "running" && len(e.Report) == 0 {
				t.Fatal("unvalidated projection")
			}
		}
		if p.NextCursor == nil {
			break
		}
		cursor = *p.NextCursor
	}
	if len(seen) != 5 {
		t.Fatal(seen)
	}
	p, err := s.Browse(ctx, Filter{Limit: 50, Status: "complete"})
	if err != nil || p.Total != 2 || len(p.Items) != 2 || p.Counts.Running != 1 {
		t.Fatalf("%+v %v", p, err)
	}
	p, err = s.Browse(ctx, Filter{Limit: 20, Search: ids[2]})
	if err != nil || p.Total != 1 || p.Items[0].Status != "canceled" {
		t.Fatalf("%+v %v", p, err)
	}
	p, err = s.Browse(ctx, Filter{Limit: 20, Search: `"latency_ms":80`})
	if err != nil || p.Total != 5 {
		t.Fatalf("%+v %v", p, err)
	}
	p, err = s.Browse(ctx, Filter{Limit: 20, Search: "%' OR 1=1 --"})
	if err != nil || p.Total != 0 {
		t.Fatalf("%+v %v", p, err)
	}
	if _, err = s.db.Exec("UPDATE runs SET report_json=json_set(report_json,'$.fault.summary.mean_ms',9) WHERE id=?", ids[0]); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Browse(ctx, Filter{Limit: 50}); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("corrupt record accepted: %v", err)
	}
}
func TestBrowseEmptyInvalidAndCanceled(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	p, err := s.Browse(ctx, Filter{Limit: 20})
	if err != nil || len(p.Items) != 0 || p.NextCursor != nil || p.Total != 0 {
		t.Fatalf("%+v %v", p, err)
	}
	for _, f := range []Filter{{Limit: 0}, {Limit: 51}, {Limit: 1, Status: "unknown"}, {Limit: 1, Cursor: "../x"}, {Limit: 1, Search: string(make([]byte, 129))}} {
		if _, err = s.Browse(ctx, f); err == nil {
			t.Fatalf("accepted %+v", f)
		}
	}
	if _, err = s.Browse(ctx, Filter{Limit: 1, Cursor: "history-00000000000000000000000000000000"}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err = s.Browse(canceled, Filter{Limit: 1}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestExistingReaderDoesNotReserveWriter(t *testing.T) {
	writer, path := newStore(t)
	reader, err := OpenExisting(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	tx, err := reader.db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	// Before the first SELECT, a deferred read transaction holds no writer
	// reservation. The engine's independent intent can commit normally.
	if _, err = writer.Start(context.Background(), testSpec()); err != nil {
		t.Fatalf("reader reserved writer lock: %v", err)
	}
	if err = tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if _, err = reader.Browse(context.Background(), Filter{Limit: 20}); err != nil {
		t.Fatal(err)
	}
}
