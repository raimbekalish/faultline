package main

import (
	"bytes"
	"context"
	"github.com/raimbekalish/faultline/internal/history"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRejectsRemoteAndUnboundedServer(t *testing.T) {
	for _, args := range [][]string{{"-listen", "0.0.0.0:8080"}, {"-listen", "localhost:8080"}, {"-ttl", "0s"}, {"-ttl", "9h"}, {"unexpected"}} {
		var b bytes.Buffer
		if err := serve(context.Background(), args, &b); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
}

type startupWriter struct{ lines chan string }

func (w startupWriter) Write(b []byte) (int, error) {
	select {
	case w.lines <- string(b):
	default:
	}
	return len(b), nil
}
func TestNativeServerCancellationClosesListener(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.sqlite")
	s, err := history.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	assets := t.TempDir()
	if err = os.WriteFile(filepath.Join(assets, "index.html"), []byte("dashboard"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w := startupWriter{make(chan string, 1)}
	done := make(chan error, 1)
	go func() { done <- serve(ctx, []string{"-db", path, "-assets", assets, "-ttl", "1s"}, w) }()
	var address string
	select {
	case line := <-w.lines:
		address = strings.Fields(line)[2]
	case err := <-done:
		t.Fatal(err)
	case <-time.After(5 * time.Second):
		t.Fatal("startup timeout")
	}
	client := &http.Client{Timeout: time.Second}
	response, err := client.Get(address + "/api/v1/health")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatal(response.StatusCode)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("server lifetime not bounded")
	}
	if response, err = client.Get(address + "/api/v1/health"); err == nil {
		response.Body.Close()
		t.Fatal("listener survived expiry")
	}
}

func TestVersionNeedsNoAssetsOrDatabase(t *testing.T) {
	var b bytes.Buffer
	if err := serve(context.Background(), []string{"--version"}, &b); err != nil || !bytes.Contains(b.Bytes(), []byte(`"revision"`)) {
		t.Fatal(err, b.String())
	}
}
