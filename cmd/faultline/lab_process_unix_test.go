//go:build darwin || linux

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/raimbekalish/faultline/internal/lab"
)

// Full CLI/parser/filesystem/real-subprocess integration with a recording Docker
// fixture. All image/runtime responses are synthetic; no Docker daemon is used.
func TestLabPlanWithRecordingDockerProcess(t *testing.T) {
	dir, err := os.MkdirTemp(os.TempDir(), "lp-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	socket := filepath.Join(dir, "s")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	bin := filepath.Join(dir, "bin")
	if err := os.Mkdir(bin, 0700); err != nil {
		t.Fatal(err)
	}
	put := func(name, stringValue string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(stringValue), 0600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	appID := "sha256:" + strings.Repeat("a", 64)
	baseImage := func(id string) map[string]any {
		return map[string]any{"id": id, "os": "linux", "arch": "arm64", "env_count": 0, "volume_count": 0, "healthcheck": false, "onbuild_count": 0}
	}
	imageFixture := func(name string, m map[string]any) string { b, _ := json.Marshal(m); return put(name, string(b)) }
	app := imageFixture("app-image.json", baseImage(appID))
	m := baseImage("sha256:" + strings.Repeat("b", 64))
	m["contract"] = "http-work-v1"
	m["revision"] = strings.Repeat("d", 40)
	catalog := imageFixture("catalog-image.json", m)
	m = baseImage("sha256:" + strings.Repeat("c", 64))
	m["env_count"] = 1
	m["pinned_digest"] = true
	proxy := imageFixture("proxy-image.json", m)
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }
	endpoint, _ := json.Marshal("unix://" + socket)
	log := filepath.Join(dir, "commands")
	script := "#!/bin/sh\nset -eu\n" +
		"test -z \"${COMPOSE_FILE-}${DOCKER_HOST-}${DOCKER_CONTEXT-}${HTTP_PROXY-}${OTEL_EXPORTER_OTLP_HEADERS-}\"\n" +
		"printf '%s\\t%s\\t%s\\n' \"$1\" \"${2-}\" \"${3-}\" >> " + quote(log) + "\n" +
		"if [ \"$1\" = context ]; then printf '%s' " + quote(string(endpoint)) + "; exit 0; fi\n" +
		"[ \"$1\" = --host ] || exit 90\nshift 2\ncase \"$1\" in\n" +
		"version) printf '%s' '{\"version\":\"29.0.0\",\"os\":\"linux\",\"arch\":\"arm64\"}' ;;\n" +
		"compose) [ \"$2\" = version ] || exit 91; printf '%s' '{\"version\":\"v5.5.1\"}' ;;\n" +
		"info) printf '%s' '\"PRIVATE-DAEMON-ID\"' ;;\n" +
		"image) [ \"$2\" = inspect ] || exit 92; case \"$5\" in\n" +
		quote(appID) + ") /bin/cat " + quote(app) + " ;;\n" +
		quote(lab.LabReference) + ") /bin/cat " + quote(catalog) + " ;;\n" +
		quote(lab.ProxyReference) + ") /bin/cat " + quote(proxy) + " ;;\n*) exit 93 ;;\nesac ;;\n*) exit 94 ;;\nesac\n"
	if err := os.WriteFile(filepath.Join(bin, "docker"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("DOCKER_CONTEXT", "")
	t.Setenv("DOCKER_HOST", "")
	t.Setenv("COMPOSE_FILE", "PRIVATE-SECRET")
	t.Setenv("HTTP_PROXY", "PRIVATE-SECRET")
	t.Setenv("OTEL_EXPORTER_OTLP_HEADERS", "PRIVATE-SECRET")
	desc := put("application.json", `{"schema_version":1,"adapter":"http-work-v1","image_id":"`+appID+`","platform":"linux/arm64","experiment":{"schema_version":1,"samples_per_phase":5,"latency_ms":80,"request_timeout_ms":1000},"tracing":"off"}`)
	plan := filepath.Join(dir, "plan.json")
	var out, stderr bytes.Buffer
	code := execute(context.Background(), []string{"lab", "plan", "--application", desc, "--out", plan}, &out, &stderr)
	if code != 0 || stderr.Len() != 0 {
		t.Fatal(code, out.String(), stderr.String())
	}
	var p lab.Plan
	b, err := os.ReadFile(plan)
	if err != nil || json.Unmarshal(b, &p) != nil || p.Validate() != nil {
		t.Fatal("invalid private plan", err)
	}
	if strings.Contains(out.String(), socket) || strings.Contains(out.String(), "PRIVATE") || strings.Contains(out.String(), "private_binding") || !strings.Contains(out.String(), `"execution_available": false`) {
		t.Fatal("redaction/qualification")
	}
	logBytes, _ := os.ReadFile(log)
	lines := strings.Split(strings.TrimSpace(string(logBytes)), "\n")
	if len(lines) != 8 {
		t.Fatal("unexpected operations", string(logBytes))
	}
	for _, line := range lines {
		parts := strings.Split(line, "\t")
		if parts[0] == "context" {
			if parts[1] != "inspect" {
				t.Fatal(line)
			}
		} else if parts[0] != "--host" || (parts[2] != "version" && parts[2] != "compose" && parts[2] != "info" && parts[2] != "image") {
			t.Fatal("mutation", line)
		}
	}
	before := string(logBytes)
	out.Reset()
	stderr.Reset()
	if code := execute(context.Background(), []string{"lab", "run"}, &out, &stderr); code != 2 {
		t.Fatal("run implemented")
	}
	if code := execute(context.Background(), []string{"lab", "cleanup"}, &out, &stderr); code != 2 {
		t.Fatal("cleanup implemented")
	}
	after, _ := os.ReadFile(log)
	if string(after) != before {
		t.Fatal("execution command reached Docker")
	}
}
