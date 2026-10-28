package lab

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"text/template"
)

func fullJournal(t *testing.T) Journal { t.Helper(); j, a := consumed(t); return createAll(t, j, a) }
func projectedContainer(t *testing.T, j Journal, k ResourceKey) containerObservation {
	t.Helper()
	return projectedContainerNoTest(j, k)
}
func projectedNetwork(t *testing.T, j Journal, k ResourceKey) networkObservation {
	t.Helper()
	return projectedNetworkNoTest(j, k)
}
func TestActualProjectionVerification(t *testing.T) {
	j := fullJournal(t)
	for _, k := range resourceKeys {
		var v any
		if resourceIndex(k) < 4 {
			v = projectedNetwork(t, j, k)
		} else {
			v = projectedContainer(t, j, k)
		}
		b, _ := json.Marshal(v)
		p, e := verifyResource(b, j, k)
		if e != nil {
			t.Fatal(k, e)
		}
		s, _ := j.State()
		if !sameProof(p, s.Resources[resourceIndex(k)].Proof) {
			t.Fatal("digest not recomputed correctly")
		}
	}
	for _, change := range []func(*containerObservation){func(v *containerObservation) { v.ID = strings.Repeat("f", 64) }, func(v *containerObservation) { v.Name = "/renamed" }, func(v *containerObservation) { v.Nonce = strings.Repeat("f", 64) }, func(v *containerObservation) { v.Role = string(Runner) }, func(v *containerObservation) { v.LabelCount++ }, func(v *containerObservation) { v.Image = "sha256:" + strings.Repeat("f", 64) }, func(v *containerObservation) { v.Command = []string{"/bin/sh"} }, func(v *containerObservation) { v.SecurityOK = false }, func(v *containerObservation) { v.ExtrasOK = false }, func(v *containerObservation) { v.PortsClear = false }, func(v *containerObservation) { v.Mounts = []mountObservation{{"bind", "/secret", true}} }, func(v *containerObservation) { v.Networks = append(v.Networks, endpointObservation{Name: "foreign"}) }, func(v *containerObservation) { v.Networks[0].ID = strings.Repeat("f", 64) }, func(v *containerObservation) { v.Networks[0].Aliases = append(v.Networks[0].Aliases, "external") }, func(v *containerObservation) { v.Memory++ }, func(v *containerObservation) { v.DNS = []string{"8.8.8.8"} }, func(v *containerObservation) { v.NetworkMode = "host" }, func(v *containerObservation) { v.EnvironmentOK = false }} {
		v := projectedContainer(t, j, Application)
		change(&v)
		b, _ := json.Marshal(v)
		if _, e := verifyResource(b, j, Application); e == nil {
			t.Fatal("unexpected configuration accepted")
		}
	}
	for _, change := range []func(*networkObservation){func(v *networkObservation) { v.Driver = "host" }, func(v *networkObservation) { v.Internal = false }, func(v *networkObservation) { v.IPv6 = true }, func(v *networkObservation) { v.Options["extra"] = "untrusted" }, func(v *networkObservation) { v.Containers = []string{strings.Repeat("f", 64)} }, func(v *networkObservation) { v.IPAMOK = false }, func(v *networkObservation) { v.Nonce = "foreign" }} {
		v := projectedNetwork(t, j, Edge)
		change(&v)
		b, _ := json.Marshal(v)
		if _, e := verifyResource(b, j, Edge); e == nil {
			t.Fatal("foreign network accepted")
		}
	}
}
func TestStrictProjectionRejectsAmbiguousOversizedMetadata(t *testing.T) {
	j := fullJournal(t)
	v := projectedContainer(t, j, Application)
	b, _ := json.Marshal(v)
	for _, bad := range [][]byte{nil, []byte(`null`), []byte(`[]`), append(b, []byte(`{}`)...), bytes.Replace(b, []byte(`"id":`), []byte(`"ID":"ignored","id":`), 1), bytes.Replace(b, []byte(`"id":`), []byte(`"\u0069d":"ignored","id":`), 1), bytes.Replace(b, []byte(`"id"`), []byte(`"Id"`), 1), bytes.Replace(b, []byte(`"memory":134217728`), []byte(`"memory":1e10000`), 1), bytes.Repeat([]byte(" "), maxInspectionBytes+1), []byte(`{"a":"\ud800"}`), []byte("\xff")} {
		if _, e := verifyResource(bad, j, Application); e == nil {
			t.Fatal("malformed accepted")
		}
	}
}

// Recording read-only Docker adapter. All actual-resource data is synthetic.
type observationReader struct {
	j      Journal
	calls  []readCommand
	change func(readCommand, []byte) ([]byte, error)
}

func (r *observationReader) read(ctx context.Context, c readCommand) ([]byte, error) {
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	if _, e := c.args(); e != nil {
		return nil, e
	}
	r.calls = append(r.calls, c)
	var b []byte
	var e error
	switch c.op {
	case networkActual:
		for _, k := range resourceKeys[:4] {
			s, _ := r.j.State()
			if s.Resources[resourceIndex(k)].Proof.ID == c.value {
				b, e = json.Marshal(projectedNetworkNoTest(r.j, k))
			}
		}
	case appActual, runnerActual, fixtureActual, proxyActual:
		k := []ResourceKey{Application, Runner, Fixture, Proxy}[int(c.op-appActual)]
		b, e = json.Marshal(projectedContainerNoTest(r.j, k))
	default:
		f := &recordingReader{}
		b, e = f.read(ctx, c)
	}
	if r.change != nil {
		return r.change(c, b)
	}
	return b, e
}

// The no-test variants clone the fixed fixture by decoding its generated graph;
// no current machine/runtime/environment facts enter these synthetic responses.
func projectedNetworkNoTest(j Journal, k ResourceKey) networkObservation {
	g, _ := GenerateGraph(j.Plan)
	s, _ := j.State()
	return networkObservation{ID: s.Resources[resourceIndex(k)].Proof.ID, Name: g.Networks[resourceIndex(k)].Name, LabelCount: 3, Launch: g.LaunchID, Role: string(k), Nonce: g.OwnerNonce, Driver: "bridge", Scope: "local", Internal: true, IPv4: true, Options: map[string]string{"com.docker.network.bridge.gateway_mode_ipv4": "isolated"}, IPAMOK: true, AuxClear: true, Containers: []string{}}
}
func projectedContainerNoTest(j Journal, k ResourceKey) containerObservation {
	g, _ := GenerateGraph(j.Plan)
	s, _ := j.State()
	c := g.Containers[resourceIndex(k)-4]
	lim := c.Role.Limits
	v := containerObservation{ID: s.Resources[resourceIndex(k)].Proof.ID, Name: "/" + c.Name, Image: c.ImageID, ConfiguredImage: c.ImageID, LabelCount: 3, LabelKeysOK: true, Launch: g.LaunchID, Role: string(k), Nonce: g.OwnerNonce, Path: c.Role.Entrypoint[0], Args: c.Role.Command, Entrypoint: c.Role.Entrypoint, Command: c.Role.Command, User: "65532:65532", Workdir: "/", StopSignal: "SIGTERM", EnvironmentOK: true, Memory: int64(lim.MemoryMiB) << 20, Swap: int64(lim.MemoryMiB) << 20, NanoCPUs: 500000000, PIDs: 64, NoFileOK: true, SecurityOK: true, ExtrasOK: true, PortsClear: true, DNS: []string{"127.0.0.1"}, DNSSearch: []string{"."}, Tmpfs: map[string]string{}, Mounts: []mountObservation{}, Networks: []endpointObservation{}}
	if k == Application {
		v.EnvCount = 2
		v.Tmpfs["/tmp"] = "rw,nosuid,nodev,noexec,size=33554432,mode=700"
	}
	if k == Proxy {
		v.EnvCount = 1
		v.LabelCount = 11
	}
	if k == Runner || k == Fixture {
		v.LabelCount = 5
		v.LabContract = "http-work-v1"
		v.LabRevision = j.Plan.Components[resourceIndex(k)-4].DeclaredRevision
	}
	for n, m := range c.Memberships {
		name := g.LaunchID + "-" + strings.TrimPrefix(string(m.Network), "network.")
		if n == 0 {
			v.NetworkMode = name
		}
		v.Networks = append(v.Networks, endpointObservation{Name: name, ID: s.Resources[resourceIndex(m.Network)].Proof.ID, Aliases: []string{m.Alias}})
	}
	return v
}
func recordingObserver(r *observationReader) inspector {
	return inspector{r, selection{}, func(e string) (socketIdentity, error) { return socketIdentity{e, 7, 9}, nil }}
}
func TestRecordedObservationAndRuntimeChanges(t *testing.T) {
	j := fullJournal(t)
	r := &observationReader{j: j}
	v, e := observeRecorded(context.Background(), recordingObserver(r), j, Application)
	if e != nil || !proofMatches(graphFor(j.Plan), Application, v.proof) {
		t.Fatal(e)
	}
	for _, c := range r.calls {
		a, e := c.args()
		if e != nil || len(a) == 0 {
			t.Fatal(e)
		}
		if c.op >= networkActual && !resourceID.MatchString(c.value) {
			t.Fatal("unbounded target")
		}
	}
	for name, change := range map[string]func(readCommand, []byte) ([]byte, error){"daemon": func(c readCommand, b []byte) ([]byte, error) {
		if c.op == serverIdentity {
			return []byte(`"FOREIGN-DAEMON"`), nil
		}
		return b, nil
	}, "invalid": func(c readCommand, b []byte) ([]byte, error) {
		if c.op == appActual {
			return []byte(`{}`), nil
		}
		return b, nil
	}, "missing": func(c readCommand, b []byte) ([]byte, error) {
		if c.op == appActual {
			return nil, ErrInspection
		}
		return b, nil
	}, "large": func(c readCommand, b []byte) ([]byte, error) {
		if c.op == appActual {
			return bytes.Repeat([]byte(" "), maxInspectionBytes+1), nil
		}
		return b, nil
	}} {
		t.Run(name, func(t *testing.T) {
			r := &observationReader{j: j, change: change}
			if _, e := observeRecorded(context.Background(), recordingObserver(r), j, Application); e == nil {
				t.Fatal("bad observation accepted")
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	r = &observationReader{j: j, change: func(c readCommand, b []byte) ([]byte, error) {
		if c.op == appActual {
			cancel()
		}
		return b, nil
	}}
	if _, e := observeRecorded(ctx, recordingObserver(r), j, Application); !errors.Is(e, context.Canceled) && e == nil {
		t.Fatal(e)
	}
	r = &observationReader{j: j}
	if _, e := observeRecorded(context.Background(), recordingObserver(r), j, "foreign"); e == nil || len(r.calls) != 0 {
		t.Fatal("foreign ID reached adapter")
	}
}
func TestContainerTemplateDerivesSecurityAndEnvironmentFlags(t *testing.T) {
	// Minimal trusted daemon-shaped fixture for real template evaluation. These
	// booleans are derived by the template, not trusted from an input label.
	labels := map[string]any{"com.faultline.launch": "synthetic", "com.faultline.role": "container.application", "com.faultline.owner": "synthetic"}
	root := map[string]any{"Id": strings.Repeat("a", 64), "Name": "/synthetic", "Path": "/app/service", "Args": []string{}, "Image": "sha256:" + strings.Repeat("a", 64), "Config": map[string]any{"Labels": labels, "Image": "sha256:" + strings.Repeat("a", 64), "Entrypoint": []string{"/app/service"}, "Cmd": []string{}, "User": "65532:65532", "WorkingDir": "/", "StopSignal": "SIGTERM", "Env": []string{"FAULTLINE_DEPENDENCY_URL=http://toxiproxy:8666/value", "FAULTLINE_DEPENDENCY_TIMEOUT_MS=250"}}, "HostConfig": map[string]any{"Memory": 134217728, "MemorySwap": 134217728, "NanoCpus": 500000000, "PidsLimit": 64, "Ulimits": []map[string]any{{"Name": "nofile", "Soft": 256, "Hard": 256}}, "Privileged": false, "ReadonlyRootfs": true, "CapAdd": []string{}, "CapDrop": []string{"ALL"}, "SecurityOpt": []string{"no-new-privileges"}, "PidMode": "", "IpcMode": "private", "UTSMode": "", "UsernsMode": "", "CgroupnsMode": "private", "PublishAllPorts": false, "PortBindings": map[string]any{}, "Binds": []string{}, "VolumesFrom": []string{}, "Devices": []string{}, "DeviceRequests": []string{}, "ExtraHosts": []string{}, "Links": []string{}, "Sysctls": map[string]string{}, "DnsOptions": []string{}, "RestartPolicy": map[string]any{"Name": "no", "MaximumRetryCount": 0}, "LogConfig": map[string]any{"Type": "none", "Config": map[string]string{}}, "Dns": []string{"127.0.0.1"}, "DnsSearch": []string{"."}, "NetworkMode": "synthetic", "Tmpfs": map[string]string{}}, "Mounts": []any{}, "NetworkSettings": map[string]any{"Ports": map[string]any{}, "Networks": map[string]any{}}}
	root["Path"] = "/app/service"
	root["Args"] = []string{}
	tpl, e := template.New("projection").Funcs(template.FuncMap{"json": func(v any) string { b, _ := json.Marshal(v); return string(b) }}).Parse(strings.ReplaceAll(strings.ReplaceAll(containerProjection, "ENV_CHECK", appEnvCheck), "LABEL_CHECK", labelKeyProjection(ownLabelKeys)))
	if e != nil {
		t.Fatal(e)
	}
	run := func() containerObservation {
		var b bytes.Buffer
		if e := tpl.Execute(&b, root); e != nil {
			t.Fatal(e)
		}
		var actual containerObservation
		if e := strictProjection(b.Bytes(), &actual); e != nil {
			t.Fatal(e, b.String())
		}
		return actual
	}
	actual := run()
	if !actual.SecurityOK || !actual.ExtrasOK || !actual.EnvironmentOK {
		t.Fatal("baseline template", actual)
	}
	host := root["HostConfig"].(map[string]any)
	host["Privileged"] = true
	if run().SecurityOK {
		t.Fatal("privilege hidden")
	}
	host["Privileged"] = false
	host["PortBindings"] = map[string]any{"8080/tcp": []any{"untrusted"}}
	if run().ExtrasOK {
		t.Fatal("publication hidden")
	}
	host["PortBindings"] = map[string]any{}
	root["Config"].(map[string]any)["Env"] = []string{"PRIVATE_SECRET=not-copied"}
	v := run()
	if v.EnvironmentOK {
		t.Fatal("unexpected env accepted")
	}
	encoded, _ := json.Marshal(v)
	if bytes.Contains(encoded, []byte("PRIVATE_SECRET")) {
		t.Fatal("environment leaked")
	}
}

func TestInheritedLabelsDoNotConferOwnership(t *testing.T) {
	j := fullJournal(t)
	for _, k := range []ResourceKey{Runner, Fixture, Proxy} {
		good := projectedContainerNoTest(j, k)
		for _, change := range []func(*containerObservation){func(v *containerObservation) { v.LabelKeysOK = false }, func(v *containerObservation) { v.Nonce = "foreign" }, func(v *containerObservation) { v.LabelCount = 3 }, func(v *containerObservation) { v.LabContract = "foreign-contract" }} {
			v := good
			change(&v)
			b, _ := json.Marshal(v)
			if _, e := verifyResource(b, j, k); e == nil {
				t.Fatal("foreign inherited labels", k)
			}
		}
	}
}
