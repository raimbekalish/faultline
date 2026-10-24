package experiment

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type fakeControl struct {
	created, added, removed, deleted        bool
	createErr, addErr, removeErr, deleteErr error
	onAdd                                   func()
	cleanupContextErr                       error
}

func (f *fakeControl) Create(context.Context, string) error { f.created = true; return f.createErr }
func (f *fakeControl) AddLatency(context.Context, string, time.Duration) error {
	f.added = true
	if f.onAdd != nil {
		f.onAdd()
	}
	return f.addErr
}
func (f *fakeControl) RemoveLatency(context.Context, string) error {
	f.removed = true
	return f.removeErr
}
func (f *fakeControl) Delete(ctx context.Context, _ string) error {
	f.deleted = true
	f.cleanupContextErr = ctx.Err()
	return f.deleteErr
}

func TestLifecycleCleanup(t *testing.T) {
	for _, tc := range []struct {
		name      string
		configure func(*fakeControl, context.CancelFunc)
		wantErr   bool
		canceled  bool
	}{
		{"success", func(*fakeControl, context.CancelFunc) {}, false, false},
		{"ambiguous create", func(f *fakeControl, _ context.CancelFunc) { f.createErr = errors.New("lost response") }, true, false},
		{"control deadline", func(f *fakeControl, _ context.CancelFunc) { f.createErr = context.DeadlineExceeded }, true, false},
		{"injection failure", func(f *fakeControl, _ context.CancelFunc) { f.addErr = errors.New("injection failed") }, true, false},
		{"removal failure", func(f *fakeControl, _ context.CancelFunc) { f.removeErr = errors.New("removal failed") }, true, false},
		{"cleanup failure", func(f *fakeControl, _ context.CancelFunc) { f.deleteErr = errors.New("server unavailable") }, true, false},
		{"cleanup deadline", func(f *fakeControl, _ context.CancelFunc) { f.deleteErr = context.DeadlineExceeded }, true, false},
		{"cancel during fault", func(f *fakeControl, cancel context.CancelFunc) { f.onAdd = cancel }, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("ok")) }))
			defer s.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			f := &fakeControl{}
			tc.configure(f, cancel)
			cfg := DefaultConfig()
			cfg.Samples = 2
			r, err := run(ctx, cfg, "test", f, s.URL)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err=%v", err)
			}
			if !f.deleted || f.cleanupContextErr != nil {
				t.Fatalf("cleanup missing or canceled: %+v", f)
			}
			if r.CleanupComplete != (f.deleteErr == nil) {
				t.Fatalf("cleanup report: %+v", r)
			}
			if tc.canceled && (!errors.Is(err, context.Canceled) || r.Status != "canceled") {
				t.Fatalf("cancel: %+v %v", r, err)
			}
			if !tc.wantErr && (r.Status != "complete" || !f.removed || r.Recovery.Summary.Requests != 2) {
				t.Fatalf("incomplete lifecycle: %+v", r)
			}
			if tc.wantErr && (r.Status == "complete" || r.Error == "") {
				t.Fatalf("hidden error: %+v", r)
			}
			if tc.wantErr && !tc.canceled && r.Status != "failed" {
				t.Fatalf("operational failure classified as cancellation: %+v", r)
			}
		})
	}
}

func TestInvalidConfigAndCanceledParentCannotCreateProxy(t *testing.T) {
	for _, cfg := range []Config{{}, {101, 100, 1000}, {1, 0, 1000}, {1, 5001, 1000}, {1, 100, 9}, {1, 100, 5001}} {
		f := &fakeControl{}
		if _, err := run(context.Background(), cfg, "test", f, ""); err == nil || f.created {
			t.Fatalf("accepted config: %+v", cfg)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	f := &fakeControl{}
	if _, err := run(ctx, DefaultConfig(), "test", f, ""); !errors.Is(err, context.Canceled) || f.created {
		t.Fatal("canceled run created proxy")
	}
}

func TestBaselineFailurePreventsInjection(t *testing.T) {
	requests := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if requests > 1 {
			w.WriteHeader(503)
		}
	}))
	defer s.Close()
	f := &fakeControl{}
	r, err := run(context.Background(), DefaultConfig(), "test", f, s.URL)
	if err == nil || f.added || !f.deleted || r.Baseline.Summary.Failures != 10 {
		t.Fatalf("report=%+v control=%+v err=%v", r, f, err)
	}
}
