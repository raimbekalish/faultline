package lab

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"text/template"
)

// Synthetic command responses: not actual Docker qualification or app evidence.
const privateSocket = "unix:///private/PRIVATE-SOCKET.sock"
const privateDaemon = "PRIVATE-DAEMON-ID"

type recordingReader struct {
	calls  []readCommand
	change func(readCommand, []byte) ([]byte, error)
}

func (f *recordingReader) read(ctx context.Context, c readCommand) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if _, err := c.args(); err != nil {
		return nil, err
	}
	f.calls = append(f.calls, c)
	var value any
	switch c.op {
	case contextEndpoint:
		value = privateSocket
	case serverVersion:
		value = map[string]any{"version": "29.0.0", "os": "linux", "arch": "arm64"}
	case composeVersion:
		value = map[string]any{"version": "v5.5.1"}
	case serverIdentity:
		value = privateDaemon
	case applicationImage, labImage, proxyImage:
		id := c.value
		if c.op == labImage {
			id = "sha256:" + strings.Repeat("b", 64)
		}
		if c.op == proxyImage {
			id = "sha256:" + strings.Repeat("c", 64)
		}
		m := map[string]any{"id": id, "os": "linux", "arch": "arm64", "env_count": 0, "volume_count": 0, "healthcheck": false, "onbuild_count": 0}
		if c.op == labImage {
			m["contract"] = "http-work-v1"
			m["revision"] = strings.Repeat("d", 40)
		}
		if c.op == proxyImage {
			m["env_count"] = 1
			m["pinned_digest"] = true
		}
		value = m
	default:
		return nil, errors.New("forbidden command")
	}
	b, _ := json.Marshal(value)
	if f.change != nil {
		return f.change(c, b)
	}
	return b, nil
}
func testInspector(f *recordingReader) inspector {
	return inspector{f, selection{}, func(e string) (socketIdentity, error) {
		if _, err := unixPath(e); err != nil {
			return socketIdentity{}, err
		}
		return socketIdentity{e, 7, 9}, nil
	}}
}
func testSnapshot(t *testing.T) inspected {
	t.Helper()
	s, err := testInspector(&recordingReader{}).inspect(context.Background(), descriptor(t))
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func fixedEntropy() io.Reader { return bytes.NewReader(bytes.Repeat([]byte{0x42}, 96)) }

func TestPlanDeterminismIdentityAndRevalidation(t *testing.T) {
	d, s := descriptor(t), testSnapshot(t)
	a, err := buildPlan(context.Background(), d, s, fixedEntropy())
	if err != nil {
		t.Fatal(err)
	}
	b, err := buildPlan(context.Background(), d, s, fixedEntropy())
	if err != nil {
		t.Fatal(err)
	}
	ab, _ := a.Encode()
	bb, _ := b.Encode()
	if !bytes.Equal(ab, bb) {
		t.Fatal("nondeterministic plan")
	}
	var parsed Plan
	if json.Unmarshal(ab, &parsed) != nil || parsed.Validate() != nil {
		t.Fatal("invalid encoded plan")
	}
	x, err := buildPlan(context.Background(), d, s, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	y, err := buildPlan(context.Background(), d, s, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if x.Identity == y.Identity || x.AuthorizationSHA256 == y.AuthorizationSHA256 || x.ConfigurationSHA256 != y.ConfigurationSHA256 || x.CatalogSHA256 != y.CatalogSHA256 {
		t.Fatal("identity/configuration conflated")
	}
	var out bytes.Buffer
	if err := a.summary().Write(&out); err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{privateSocket, privateDaemon, a.PrivateBinding.DaemonFingerprint, a.PrivateBinding.Salt, a.Identity.AuthorizationNonce, "private_binding", "socket_inode", "socket_device"} {
		if strings.Contains(out.String(), secret) {
			t.Fatal("public binding leak", secret)
		}
	}
	if !strings.Contains(string(ab), "private_binding") || strings.Contains(string(ab), privateDaemon) {
		t.Fatal("private binding missing or raw daemon ID persisted")
	}
	if a.Status != "PLANNED" || a.RuntimeQualification != "NOT_PERFORMED" || a.ApplicationQualification != "NOT_PERFORMED" || a.ExecutionAvailable {
		t.Fatal("false qualification")
	}
	for _, change := range []func(*Plan){
		func(p *Plan) { p.Descriptor.Tracing = "jaeger" }, func(p *Plan) { p.ExecutionAvailable = true }, func(p *Plan) { p.RuntimeQualification = "QUALIFIED" }, func(p *Plan) { p.Catalog.Roles[0].Command = append(p.Catalog.Roles[0].Command, "evil") }, func(p *Plan) { p.AuthorizationSHA256 = strings.Repeat("0", 64) }, func(p *Plan) { p.Components[1].Reference = "attacker:latest" }, func(p *Plan) { p.PrivateBinding.Endpoint = "ssh://private" }, func(p *Plan) { p.Runtime.Arch = "amd64" }, func(p *Plan) { p.Identity.AuthorizationNonce = "short" },
	} {
		var copy Plan
		_ = json.Unmarshal(ab, &copy)
		change(&copy)
		if _, err := copy.Encode(); err == nil {
			t.Fatal("invalid plan encoded")
		}
	}
	s.Application.EnvCount = 1
	if _, err := buildPlan(context.Background(), d, s, fixedEntropy()); err == nil {
		t.Fatal("unvalidated inspection accepted")
	}
	if _, err := buildPlan(context.Background(), d, testSnapshot(t), bytes.NewReader(nil)); err == nil {
		t.Fatal("entropy failure hidden")
	}
}

func TestEndpointPrecedenceAndRefusal(t *testing.T) {
	for _, tc := range []struct {
		name     string
		sel      selection
		resolved string
		ok       bool
	}{
		{"default", selection{}, privateSocket, true},
		{"host", selection{Host: privateSocket}, privateSocket, true},
		{"context_over_remote_host", selection{"chosen", "tcp://unrelated.invalid:2375"}, privateSocket, true},
		{"remote_context_over_local_host", selection{"chosen", privateSocket}, "ssh://unrelated.invalid", false},
		{"remote_default", selection{}, "tcp://unrelated.invalid:2375", false},
		{"remote_host", selection{Host: "tcp://unrelated.invalid:2375"}, privateSocket, false},
		{"flag_context", selection{Context: "--help"}, privateSocket, false},
		{"long_context", selection{Context: strings.Repeat("a", 65)}, privateSocket, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &recordingReader{change: func(c readCommand, b []byte) ([]byte, error) {
				if c.op == contextEndpoint {
					return json.Marshal(tc.resolved)
				}
				return b, nil
			}}
			i := testInspector(f)
			i.selected = tc.sel
			_, err := i.inspect(context.Background(), descriptor(t))
			if (err == nil) != tc.ok {
				t.Fatal(err)
			}
			for _, call := range f.calls {
				if call.op != contextEndpoint && (!tc.ok || call.endpoint != tc.resolved) {
					t.Fatal("unsafe selected endpoint", call.op)
				}
			}
		})
	}
	for _, endpoint := range []string{"", "unix://host/path", "unix:relative", "unix:///", "unix:///tmp/../socket", "unix:///tmp/socket?x=1", "unix:///tmp/socket?", "unix:///tmp/socket#x", "unix:///tmp/%00socket", "unix:///tmp/socket\n", "http://127.0.0.1:2375", "npipe:////./pipe/docker_engine"} {
		if _, err := unixPath(endpoint); err == nil {
			t.Fatal("endpoint accepted", endpoint)
		}
	}
}

func TestInspectionMalformedMissingAndConflict(t *testing.T) {
	for _, op := range []operation{contextEndpoint, serverVersion, composeVersion, serverIdentity, applicationImage, labImage, proxyImage} {
		for _, raw := range []string{`null`, `{}`, `[]`, `"\ud800"`, `{"version":"29.0.0","VERSION":"secret"}`, strings.Repeat("x", maxInspectionBytes+1)} {
			f := &recordingReader{change: func(c readCommand, b []byte) ([]byte, error) {
				if c.op == op {
					return []byte(raw), nil
				}
				return b, nil
			}}
			if _, err := testInspector(f).inspect(context.Background(), descriptor(t)); err == nil {
				t.Fatal("invalid response accepted", op)
			}
		}
		f := &recordingReader{change: func(c readCommand, b []byte) ([]byte, error) {
			if c.op == op {
				return nil, ErrInspection
			}
			return b, nil
		}}
		if _, err := testInspector(f).inspect(context.Background(), descriptor(t)); err == nil {
			t.Fatal("missing metadata/image accepted", op)
		}
	}
	for key, value := range map[string]any{"id": "sha256:" + strings.Repeat("f", 64), "os": "windows", "arch": "amd64", "env_count": 1, "volume_count": 1, "healthcheck": true, "onbuild_count": 1, "unrecognized": "PRIVATE-SECRET", "env_count_bad": -1} {
		f := &recordingReader{change: func(c readCommand, b []byte) ([]byte, error) {
			if c.op == applicationImage {
				var m map[string]any
				_ = json.Unmarshal(b, &m)
				if key == "env_count_bad" {
					m["env_count"] = -1
				} else {
					m[key] = value
				}
				return json.Marshal(m)
			}
			return b, nil
		}}
		if _, err := testInspector(f).inspect(context.Background(), descriptor(t)); err == nil {
			t.Fatal("metadata conflict accepted", key)
		}
	}
	for _, tc := range []struct {
		op    operation
		key   string
		value any
	}{{labImage, "contract", "different"}, {labImage, "revision", "dirty"}, {labImage, "env_count", 1}, {proxyImage, "pinned_digest", false}, {proxyImage, "env_count", 0}, {serverVersion, "arch", "amd64"}, {serverVersion, "os", "windows"}, {serverVersion, "version", "PRIVATE-SECRET"}, {composeVersion, "version", "PRIVATE-SECRET"}} {
		f := &recordingReader{change: func(c readCommand, b []byte) ([]byte, error) {
			if c.op == tc.op {
				var m map[string]any
				_ = json.Unmarshal(b, &m)
				m[tc.key] = tc.value
				return json.Marshal(m)
			}
			return b, nil
		}}
		if _, err := testInspector(f).inspect(context.Background(), descriptor(t)); err == nil {
			t.Fatal("catalog/runtime conflict accepted", tc.op, tc.key)
		}
	}
	f := &recordingReader{}
	i := testInspector(f)
	calls := 0
	i.socket = func(e string) (socketIdentity, error) { calls++; return socketIdentity{e, 7, uint64(calls)}, nil }
	if _, err := i.inspect(context.Background(), descriptor(t)); err == nil {
		t.Fatal("replaced socket accepted")
	}
	f = &recordingReader{}
	ids := 0
	f.change = func(c readCommand, b []byte) ([]byte, error) {
		if c.op == serverIdentity {
			ids++
			if ids == 2 {
				return json.Marshal("REPLACED-DAEMON")
			}
		}
		return b, nil
	}
	if _, err := testInspector(f).inspect(context.Background(), descriptor(t)); err == nil {
		t.Fatal("replaced daemon accepted")
	}
}

func TestCreateReadOnlyPrivateAndNoReplace(t *testing.T) {
	dir := privateDir(t)
	input := filepath.Join(dir, "app.json")
	mustWrite(t, input, []byte(goodDescriptor))
	out := filepath.Join(dir, "plan.json")
	f := &recordingReader{}
	summary, err := create(context.Background(), input, out, testInspector(f), fixedEntropy())
	if err != nil {
		t.Fatal(err)
	}
	if len(f.calls) != 8 {
		t.Fatal("unexpected command count", len(f.calls))
	}
	for _, call := range f.calls {
		a, err := call.args()
		if err != nil {
			t.Fatal(err)
		}
		if !allowedReadArgs(a) {
			t.Fatal("mutation", a)
		}
	}
	info, err := os.Stat(out)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("private plan mode", info, err)
	}
	b, _ := os.ReadFile(out)
	var p Plan
	if json.Unmarshal(b, &p) != nil || p.Validate() != nil || summary.AuthorizationSHA256 != p.AuthorizationSHA256 {
		t.Fatal("invalid publication")
	}
	before := append([]byte(nil), b...)
	f.calls = nil
	if _, err := create(context.Background(), input, out, testInspector(f), rand.Reader); err == nil || len(f.calls) != 0 {
		t.Fatal("output replacement attempted")
	}
	b, _ = os.ReadFile(out)
	if !bytes.Equal(before, b) {
		t.Fatal("output changed")
	}
	original, _ := os.ReadFile(input)
	if string(original) != goodDescriptor {
		t.Fatal("input changed")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 2 {
		t.Fatal("extra journal/resource files", entries)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := create(ctx, input, filepath.Join(dir, "canceled.json"), testInspector(f), rand.Reader); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	f.change = func(c readCommand, b []byte) ([]byte, error) { return nil, errors.New("PRIVATE-SECRET") }
	if _, err := create(context.Background(), input, filepath.Join(dir, "failed.json"), testInspector(f), rand.Reader); err == nil || strings.Contains(PublicError(err), "PRIVATE") {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "failed.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("failed plan published")
	}
	if err := summary.Write(shortWriter{}); err == nil {
		t.Fatal("short stdout write hidden")
	}
	if err := summary.Write(errorWriter{}); err == nil {
		t.Fatal("failed stdout write hidden")
	}
	if err := (Summary{Notes: []string{strings.Repeat("x", MaxSummaryBytes)}}).Write(&bytes.Buffer{}); err == nil {
		t.Fatal("summary size cap")
	}
}

func allowedReadArgs(a []string) bool {
	if len(a) >= 2 && a[0] == "context" && a[1] == "inspect" {
		return true
	}
	if len(a) < 3 || a[0] != "--host" {
		return false
	}
	return a[2] == "version" || a[2] == "info" || len(a) > 3 && (a[2] == "compose" && a[3] == "version" || a[2] == "image" && a[3] == "inspect")
}
func mustWrite(t *testing.T, path string, b []byte) {
	t.Helper()
	if err := os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
}

func privateDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	return dir
}

type shortWriter struct{}

func (shortWriter) Write(b []byte) (int, error) { return len(b) / 2, nil }

type errorWriter struct{}

func (errorWriter) Write([]byte) (int, error) { return 0, errors.New("PRIVATE-SECRET") }

func TestInspectionTemplatesNeverCopyPayloadMetadata(t *testing.T) {
	object := map[string]any{"Id": "sha256:" + strings.Repeat("b", 64), "Os": "linux", "Architecture": "arm64", "RepoDigests": []string{proxyRepoDigest, "PRIVATE-REGISTRY/secret@sha256:other"}, "Config": map[string]any{"Env": []string{"TOKEN=PRIVATE-CREDENTIAL"}, "Volumes": map[string]any{}, "Healthcheck": nil, "OnBuild": []string{}, "Labels": map[string]string{"com.faultline.lab.contract": "http-work-v1", "org.opencontainers.image.revision": strings.Repeat("d", 40), "secret": "PRIVATE-LABEL"}}}
	for _, format := range []string{appFormat, labFormat, proxyFormat} {
		tmpl, err := template.New("inspect").Funcs(template.FuncMap{"json": func(v any) (string, error) { b, e := json.Marshal(v); return string(b), e }}).Parse(format)
		if err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer
		if err := tmpl.Execute(&out, object); err != nil {
			t.Fatal(err)
		}
		if !json.Valid(out.Bytes()) || strings.Contains(out.String(), "PRIVATE") {
			t.Fatal("private image metadata copied", out.String())
		}
	}
	if !reflect.DeepEqual(Catalog().Roles[0].Networks, []string{"edge", "data"}) {
		t.Fatal("application network scope")
	}
}

func TestInspectionTemplatesOmittedAndNullOptionalFields(t *testing.T) {
	for _, config := range []map[string]any{{}, {"Env": nil, "Volumes": nil, "Healthcheck": nil, "OnBuild": nil, "Labels": nil}} {
		object := map[string]any{"Id": "sha256:" + strings.Repeat("a", 64), "Os": "linux", "Architecture": "arm64", "Config": config}
		for _, format := range []string{appFormat, labFormat, proxyFormat} {
			tmpl, err := template.New("Docker-optional-fields").Funcs(template.FuncMap{"json": func(v any) (string, error) { b, e := json.Marshal(v); return string(b), e }}).Parse(format)
			if err != nil {
				t.Fatal(err)
			}
			var out bytes.Buffer
			if err := tmpl.Execute(&out, object); err != nil {
				t.Fatal(err)
			}
			var m imageMetadata
			if err := json.Unmarshal(out.Bytes(), &m); err != nil || m.EnvCount != 0 || m.VolumeCount != 0 || m.Healthcheck || m.OnBuildCount != 0 || m.PinnedDigest {
				t.Fatal("omitted metadata misinterpreted", out.String(), err)
			}
			if format == appFormat {
				if _, err := metadata(out.Bytes()); err != nil {
					t.Fatal(err)
				}
			} else if format == labFormat {
				if _, err := metadata(out.Bytes(), "contract", "revision"); err == nil {
					t.Fatal("absent required catalog declarations accepted")
				}
			}
		}
	}
}

func TestInspectionTemplateRejectsDeclaredEmptyHealthcheck(t *testing.T) {
	object := map[string]any{"Id": "sha256:" + strings.Repeat("a", 64), "Os": "linux", "Architecture": "arm64", "Config": map[string]any{"Healthcheck": map[string]any{}}}
	tmpl, err := template.New("healthcheck-presence").Funcs(template.FuncMap{"json": func(v any) (string, error) { b, e := json.Marshal(v); return string(b), e }}).Parse(appFormat)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := tmpl.Execute(&out, object); err != nil {
		t.Fatal(err)
	}
	if _, err := metadata(out.Bytes()); err == nil {
		t.Fatal("declared empty healthcheck was confused with missing/null")
	}
}

func TestPlanCodecRejectsAmbiguousOrIgnoredMaterial(t *testing.T) {
	p, err := buildPlan(context.Background(), descriptor(t), testSnapshot(t), fixedEntropy())
	if err != nil {
		t.Fatal(err)
	}
	b, err := p.Encode()
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{
		strings.Replace(string(b), `"schema_version": 1`, `"schema_version": 1,"EXTRA":"private"`, 1),
		strings.Replace(string(b), `"schema_version": 1`, `"SCHEMA_VERSION": 1`, 1),
		strings.Replace(string(b), `"schema_version": 1`, `"schema_version": 1,"\u0073chema_version":1`, 1),
		strings.Replace(string(b), `"image_id"`, `"IMAGE_ID"`, 1),
		strings.Replace(string(b), `"application_environment": [`, `"application_environment": [null,`, 1),
		string(b) + `{}`, strings.Repeat(" ", MaxPlanBytes+1),
	} {
		var decoded Plan
		if json.Unmarshal([]byte(raw), &decoded) == nil {
			t.Fatal("ambiguous plan accepted")
		}
	}
	var compact bytes.Buffer
	_ = json.Compact(&compact, b)
	var decoded Plan
	if json.Unmarshal(compact.Bytes(), &decoded) != nil || decoded.Validate() != nil {
		t.Fatal("canonical compact plan refused")
	}
}

type cancelEntropy struct{ cancel context.CancelFunc }

func (r cancelEntropy) Read(b []byte) (int, error) {
	for i := range b {
		b[i] = 1
	}
	r.cancel()
	return len(b), nil
}
func TestPlanCancellationDuringFullEntropyRead(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if _, err := buildPlan(ctx, descriptor(t), testSnapshot(t), cancelEntropy{cancel}); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation discarded after full entropy read", err)
	}
}
