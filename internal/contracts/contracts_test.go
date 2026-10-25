// Package contracts tests deliberately synthetic public format-1 examples.
package contracts

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/raimbekalish/faultline/internal/bundle"
	"github.com/raimbekalish/faultline/internal/bundlecli"
	"github.com/raimbekalish/faultline/internal/jsonstrict"
)

func TestOfflineV1Contracts(t *testing.T) {
	dir := t.TempDir()
	paths := map[string]string{}
	for _, name := range []string{"complete", "legacy", "canceled"} {
		raw, err := os.ReadFile("testdata/v1/" + name + ".json")
		if err != nil {
			t.Fatal(err)
		}
		var e bundle.Evidence
		if err = json.Unmarshal(raw, &e); err != nil {
			t.Fatal(err)
		}
		b, err := bundle.Build(context.Background(), e)
		if err != nil {
			t.Fatal(err)
		}
		if name == "complete" {
			want, err := os.ReadFile("testdata/v1/complete.bundle.sha256")
			if err != nil {
				t.Fatal(err)
			}
			if string(want) != fmt.Sprintf("%x\n", sha256.Sum256(b)) {
				t.Fatal("format-1 bytes changed; review archive compatibility")
			}
		}
		path := filepath.Join(dir, name+".flb")
		if err = os.WriteFile(path, b, 0600); err != nil {
			t.Fatal(err)
		}
		paths[name] = path
		var out, stderr bytes.Buffer
		if code := bundlecli.Execute(context.Background(), []string{"inspect", path}, &out, &stderr); code != 0 {
			t.Fatal(code, stderr.String())
		}
		assertJSON(t, name+".inspect.json", out.Bytes())
	}
	for _, name := range []string{"complete", "legacy", "canceled"} {
		var out, stderr bytes.Buffer
		if code := bundlecli.Execute(context.Background(), []string{"compare", paths["complete"], paths[name]}, &out, &stderr); code != 0 {
			t.Fatal(code, stderr.String())
		}
		assertJSON(t, name+".compare.json", out.Bytes())
	}
	var out, stderr bytes.Buffer
	if code := bundlecli.Execute(context.Background(), []string{"verify", paths["complete"]}, &out, &stderr); code != 0 {
		t.Fatal(code, stderr.String())
	}
	assertJSON(t, "complete.verify.json", out.Bytes())
}
func assertJSON(t *testing.T, name string, got []byte) {
	t.Helper()
	want, err := os.ReadFile("testdata/v1/" + name)
	if err != nil {
		t.Fatal(err)
	}
	var a, b bytes.Buffer
	if err = json.Compact(&a, got); err != nil {
		t.Fatal(err)
	}
	if err = json.Compact(&b, want); err != nil {
		t.Fatal(err)
	}
	// Ignore whitespace only. Field ordering is deliberately frozen for this encoder.
	if !bytes.Equal(a.Bytes(), b.Bytes()) {
		t.Fatalf("contract %s changed; review fields, units, nulls, reasons and versions\ngot %s", name, got)
	}
}
func TestUnsupportedFixture(t *testing.T) {
	manifest, err := os.ReadFile("testdata/v1/unsupported.manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	for _, name := range []string{"manifest.json", "history.json", "spec.json", "report.json"} {
		h := &zip.FileHeader{Name: name, Method: zip.Store, ModifiedDate: 33}
		h.SetMode(0600)
		w, err := z.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		data := []byte("{}")
		if name == "manifest.json" {
			data = bytes.TrimSpace(manifest)
		}
		if _, err = w.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err = z.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = bundle.Read(context.Background(), bytes.NewReader(b.Bytes())); err == nil || !strings.Contains(err.Error(), "unsupported evidence format") {
		t.Fatal(err)
	}
}

func TestInvalidJSONFixtures(t *testing.T) {
	b, err := os.ReadFile("testdata/v1/invalid.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name string
		JSON string
	}
	if err = json.Unmarshal(b, &cases); err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			if err := jsonstrict.Validate([]byte(tc.JSON)); err == nil {
				t.Fatal("invalid fixture accepted")
			}
		})
	}
}
