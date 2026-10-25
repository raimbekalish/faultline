package bundle

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/raimbekalish/faultline/internal/experiment"
	"github.com/raimbekalish/faultline/internal/spec"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fixture(status string) Evidence {
	sp := spec.FromConfig(experiment.Config{Samples: 2, LatencyMS: 80, RequestTimeoutMS: 1000})
	p := experiment.Phase{Samples: []experiment.Sample{{DurationMS: 1, Status: 200}, {DurationMS: 5, Status: 200}}}
	p.Summary = experiment.Summarize(p.Samples)
	at := time.Date(2026, 10, 24, 1, 0, 0, 0, time.UTC)
	r := experiment.Report{SchemaVersion: 1, RunID: "faultline-" + strings.Repeat("a", 32), Status: status, Config: sp.Config(), Target: experiment.Target, StartedAt: at, FinishedAt: at.Add(time.Second), CleanupComplete: true, Baseline: p, Fault: p, Recovery: p}
	if status != "complete" {
		r.Error = "context canceled"
		r.Recovery = experiment.Phase{}
	}
	report, _ := json.Marshal(r)
	hash, _ := sp.Hash()
	return Evidence{Metadata{"history-" + strings.Repeat("b", 32), r.RunID, hash, at, at.Add(2 * time.Second), status}, sp, report}
}
func TestRoundtripDeterminismAndPartialEvidence(t *testing.T) {
	for _, status := range []string{"complete", "failed", "canceled"} {
		t.Run(status, func(t *testing.T) {
			e := fixture(status)
			b, err := Build(context.Background(), e)
			if err != nil {
				t.Fatal(err)
			}
			b2, err := Build(context.Background(), e)
			if err != nil || !bytes.Equal(b, b2) {
				t.Fatal("nondeterministic", err)
			}
			got, err := Read(context.Background(), bytes.NewReader(b))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got.Report, e.Report) || got.History != e.History || len(got.Warnings) < 3 {
				t.Fatal(got)
			}
			if status != "complete" && !strings.Contains(strings.Join(got.Warnings, " "), "0/2 observations") {
				t.Fatal(got.Warnings)
			}
		})
	}
}
func entries(t *testing.T, b []byte) map[string][]byte {
	t.Helper()
	z, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatal(err)
	}
	result := map[string][]byte{}
	for _, f := range z.File {
		r, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		result[f.Name], err = io.ReadAll(r)
		r.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	return result
}
func archive(t *testing.T, files map[string][]byte, order []string, method uint16, symlink bool) []byte {
	t.Helper()
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	for _, name := range order {
		h := &zip.FileHeader{Name: name, Method: method, ModifiedDate: 33}
		h.SetMode(0600)
		if symlink {
			h.SetMode(os.ModeSymlink | 0600)
		}
		w, err := z.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = w.Write(files[name]); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
func rehash(t *testing.T, files map[string][]byte) {
	t.Helper()
	m := Manifest{Format: "faultline-evidence", Version: 1}
	for _, name := range names[1:] {
		m.Files = append(m.Files, digest(name, files[name]))
	}
	files[names[0]], _ = json.Marshal(m)
}
func TestRejectMalformedAndUnsafeArchives(t *testing.T) {
	good, _ := Build(context.Background(), fixture("complete"))
	for _, tc := range []struct {
		name   string
		change func(map[string][]byte) []byte
	}{
		{"duplicate", func(f map[string][]byte) []byte {
			return archive(t, f, []string{names[0], names[1], names[2], names[2]}, zip.Store, false)
		}},
		{"missing", func(f map[string][]byte) []byte { return archive(t, f, names[:3], zip.Store, false) }},
		{"traversal", func(f map[string][]byte) []byte {
			f["../report.json"] = f[names[3]]
			return archive(t, f, append(append([]string{}, names[:3]...), "../report.json"), zip.Store, false)
		}},
		{"absolute", func(f map[string][]byte) []byte {
			f["/report.json"] = f[names[3]]
			return archive(t, f, append(append([]string{}, names[:3]...), "/report.json"), zip.Store, false)
		}},
		{"symlink", func(f map[string][]byte) []byte { return archive(t, f, names, zip.Store, true) }},
		{"compression", func(f map[string][]byte) []byte { return archive(t, f, names, zip.Deflate, false) }},
		{"unsupported version", func(f map[string][]byte) []byte {
			f[names[0]] = bytes.Replace(f[names[0]], []byte(`"format_version":1`), []byte(`"format_version":2`), 1)
			return archive(t, f, names, zip.Store, false)
		}},
		{"oversized entry", func(f map[string][]byte) []byte {
			f[names[3]] = bytes.Repeat([]byte("x"), MaxReport+1)
			return archive(t, f, names, zip.Store, false)
		}},
		{"checksum", func(f map[string][]byte) []byte {
			f[names[3]] = bytes.Replace(f[names[3]], []byte(`"mean_ms":3`), []byte(`"mean_ms":2`), 1)
			return archive(t, f, names, zip.Store, false)
		}},
		{"plausible altered mean rehashed", func(f map[string][]byte) []byte {
			f[names[3]] = bytes.Replace(f[names[3]], []byte(`"mean_ms":3`), []byte(`"mean_ms":2`), 1)
			rehash(t, f)
			return archive(t, f, names, zip.Store, false)
		}},
		{"wrong identity rehashed", func(f map[string][]byte) []byte {
			f[names[1]] = bytes.Replace(f[names[1]], []byte(`"status":"complete"`), []byte(`"status":"canceled"`), 1)
			rehash(t, f)
			return archive(t, f, names, zip.Store, false)
		}},
		{"duplicate JSON", func(f map[string][]byte) []byte {
			f[names[3]] = bytes.Replace(f[names[3]], []byte(`"schema_version":1`), []byte(`"schema_version":1,"SCHEMA_VERSION":1`), 1)
			rehash(t, f)
			return archive(t, f, names, zip.Store, false)
		}},
		{"deep JSON", func(f map[string][]byte) []byte {
			f[names[3]] = []byte(strings.Repeat("[", 40) + "0" + strings.Repeat("]", 40))
			rehash(t, f)
			return archive(t, f, names, zip.Store, false)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bad := tc.change(entries(t, good))
			if _, err := Read(context.Background(), bytes.NewReader(bad)); err == nil {
				t.Fatal("accepted malformed archive")
			}
		})
	}
	for _, b := range [][]byte{nil, good[:len(good)-1], append(append([]byte{}, good...), 0), append([]byte("prefix"), good...), bytes.Repeat([]byte("x"), MaxArchive+1)} {
		if _, err := Read(context.Background(), bytes.NewReader(b)); err == nil {
			t.Fatal("accepted malformed container")
		}
	}
	// Actual stored bytes altered without recomputing CRC/checksums.
	bad := append([]byte{}, good...)
	i := bytes.Index(bad, []byte(`"mean_ms":3`))
	bad[i+10] = '2'
	if _, err := Read(context.Background(), bytes.NewReader(bad)); err == nil {
		t.Fatal("CRC corruption accepted")
	}
}
func TestSaveNoReplaceAndCancellation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "run.flb")
	b, _ := Build(context.Background(), fixture("complete"))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := Save(ctx, path, b); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("canceled file published")
	}
	if err := Save(context.Background(), path, b); err != nil {
		t.Fatal(err)
	}
	if err := Save(context.Background(), path, []byte("wrong")); !os.IsExist(err) {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if !bytes.Equal(got, b) {
		t.Fatal("existing evidence overwritten")
	}
	if _, err := ReadFile(context.Background(), path); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.flb")
	if err := os.Symlink(path, link); err == nil {
		if err := Save(context.Background(), link, b); err == nil {
			t.Fatal("symlink overwritten")
		}
		if _, err := ReadFile(context.Background(), link); err == nil {
			t.Fatal("symlink input accepted")
		}
	}
	if err := Save(context.Background(), filepath.Join(path, "bad.flb"), b); err == nil {
		t.Fatal("invalid output parent accepted")
	}
	leftovers, _ := filepath.Glob(filepath.Join(dir, ".faultline-evidence-*"))
	if len(leftovers) != 0 {
		t.Fatal(leftovers)
	}
	if _, err := Read(ctx, bytes.NewReader(b)); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := Build(ctx, fixture("complete")); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

type brokenWriter struct{ cancel context.CancelFunc }

func (w brokenWriter) Write(b []byte) (int, error) {
	if w.cancel != nil {
		w.cancel()
		return len(b), nil
	}
	return 0, io.ErrClosedPipe
}
func TestFailedAndCanceledWrites(t *testing.T) {
	if err := writeBytes(context.Background(), brokenWriter{}, []byte("x")); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	if err := writeBytes(ctx, brokenWriter{cancel}, bytes.Repeat([]byte("x"), 65536)); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
func FuzzReadNeverPanics(f *testing.F) {
	good, _ := Build(context.Background(), fixture("complete"))
	f.Add(good)
	f.Add([]byte("PK"))
	f.Fuzz(func(t *testing.T, b []byte) {
		if len(b) > MaxArchive+1 {
			return
		}
		_, _ = Read(context.Background(), bytes.NewReader(b))
	})
}

func TestPartialFaultAndFailureBeforeRunID(t *testing.T) {
	e := fixture("canceled")
	var r experiment.Report
	_ = json.Unmarshal(e.Report, &r)
	r.Fault = experiment.Phase{Samples: []experiment.Sample{{DurationMS: 1, Status: 0, Error: "context canceled"}}}
	r.Fault.Summary = experiment.Summarize(r.Fault.Samples)
	r.Comparison = experiment.Comparison{MeanDeltaMS: -2, P95DeltaMS: -4, FailureRateDelta: 1}
	e.Report, _ = json.Marshal(r)
	b, err := Build(context.Background(), e)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Read(context.Background(), bytes.NewReader(b))
	if err != nil || !strings.Contains(strings.Join(got.Warnings, " "), "fault: 1/2") {
		t.Fatal(got.Warnings, err)
	}
	e = fixture("failed")
	_ = json.Unmarshal(e.Report, &r)
	r.RunID = ""
	r.CleanupComplete = false
	r.Baseline = experiment.Phase{}
	r.Fault = experiment.Phase{}
	r.Recovery = experiment.Phase{}
	r.Comparison = experiment.Comparison{}
	e.History.RunID = ""
	e.Report, _ = json.Marshal(r)
	b, err = Build(context.Background(), e)
	if err != nil {
		t.Fatal(err)
	}
	got, err = Read(context.Background(), bytes.NewReader(b))
	if err != nil || !strings.Contains(strings.Join(got.Warnings, " "), "cleanup was not confirmed") {
		t.Fatal(got.Warnings, err)
	}
}
