//go:build darwin || linux

package lab

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func quote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }
func scriptReader(t *testing.T, body string) *cliReader {
	t.Helper()
	path := filepath.Join(t.TempDir(), "docker-recording-fixture")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nset -eu\n"+body), 0700); err != nil {
		t.Fatal(err)
	}
	return &cliReader{path: path, env: []string{"PATH=/usr/bin:/bin"}}
}

func TestCommandBoundaryAndSubprocessPrivacy(t *testing.T) {
	log := filepath.Join(t.TempDir(), "calls")
	r := scriptReader(t, `printf '%s\n' "$@" >> `+quote(log)+"\nprintf '%s' 'PRIVATE-CREDENTIAL' >&2\nprintf '%s' '\"SYNTHETIC-ID\"'\n")
	if _, err := r.read(context.Background(), readCommand{op: operation(999)}); err == nil {
		t.Fatal("unknown operation executed")
	}
	if _, err := os.Stat(log); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("invalid operation reached process")
	}
	b, err := r.read(context.Background(), readCommand{op: serverIdentity, endpoint: privateSocket})
	if err != nil || string(b) != `"SYNTHETIC-ID"` {
		t.Fatal(err, string(b))
	}
	logged, _ := os.ReadFile(log)
	if !strings.Contains(string(logged), "info\n--format\n{{json .ID}}") || strings.Contains(string(logged), "PRIVATE-CREDENTIAL") {
		t.Fatal("bad read command log")
	}
	for _, bad := range []readCommand{{op: contextEndpoint, value: "--help"}, {op: applicationImage, endpoint: privateSocket, value: "attacker:latest"}, {op: labImage, endpoint: privateSocket, value: "other"}, {op: proxyImage, endpoint: privateSocket, value: LabReference}, {op: serverIdentity, endpoint: "tcp://remote:2375"}, {op: serverVersion, endpoint: privateSocket, value: "--help"}} {
		if _, err := r.read(context.Background(), bad); err == nil {
			t.Fatal("invalid command executed")
		}
	}
	after, _ := os.ReadFile(log)
	if string(logged) != string(after) {
		t.Fatal("rejected command reached process")
	}
	r = scriptReader(t, "printf '%s' 'PRIVATE-CREDENTIAL /private/path' >&2\nexit 17\n")
	if _, err := r.read(context.Background(), readCommand{op: serverIdentity, endpoint: privateSocket}); err == nil || strings.Contains(PublicError(err), "PRIVATE") {
		t.Fatal(err)
	}
}

func TestSubprocessBoundsAndCancellation(t *testing.T) {
	r := scriptReader(t, "/usr/bin/yes X\n")
	for _, body := range []string{"/usr/bin/yes X\n", "/usr/bin/yes PRIVATE-SECRET >&2\n"} {
		r = scriptReader(t, body)
		start := time.Now()
		if b, err := r.read(context.Background(), readCommand{op: serverIdentity, endpoint: privateSocket}); err == nil || len(b) > maxInspectionBytes {
			t.Fatal("unbounded success", err)
		}
		if time.Since(start) > 4*time.Second {
			t.Fatal("output-bound termination too slow")
		}
	}
	r = scriptReader(t, "/bin/sleep 20 &\nwait\n")
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := r.read(ctx, readCommand{op: serverIdentity, endpoint: privateSocket}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("child group/pipe retained after cancellation")
	}
	ctx, cancel = context.WithCancel(context.Background())
	cancel()
	if _, err := r.read(ctx, readCommand{op: serverIdentity, endpoint: privateSocket}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestProductionEnvironmentDropsTargetOverridesAndSecrets(t *testing.T) {
	dir := t.TempDir()
	stub := filepath.Join(dir, "docker")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	for _, key := range []string{"DOCKER_CONTEXT", "DOCKER_HOST", "DOCKER_TLS_VERIFY", "DOCKER_API_VERSION", "COMPOSE_FILE", "COMPOSE_PROFILES", "HTTP_PROXY", "OTEL_EXPORTER_OTLP_HEADERS", "PRIVATE_TOKEN"} {
		t.Setenv(key, "PRIVATE-SECRET")
	}
	r, err := newCLIReader()
	if err != nil {
		t.Fatal(err)
	}
	if r.path != stub {
		t.Fatal("wrong CLI path")
	}
	for _, entry := range r.env {
		if strings.Contains(entry, "PRIVATE-SECRET") {
			t.Fatal("ambient secret/target leaked")
		}
	}
}
