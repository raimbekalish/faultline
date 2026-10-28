package lab

import (
	"context"
	"encoding/json"
	"os"
	"testing"
)

// Opt-in diagnostics only. The plan's application/image fields are synthetic;
// no image/resource inspection, enrollment, reservation or execution is performed.
func TestLiveReadOnlyRuntimeWitness(t *testing.T) {
	if os.Getenv("FAULTLINE_READONLY_RUNTIME_TEST") != "1" {
		t.Skip("explicit local read-only diagnostic opt-in required")
	}
	ctx := context.Background()
	r, e := newCLIReader()
	if e != nil {
		t.Fatal(PublicError(e))
	}
	i := inspector{r, selectionFromEnvironment(), inspectSocket}
	socket, e := i.resolve(ctx)
	if e != nil {
		t.Fatal(PublicError(e))
	}
	b, e := r.read(ctx, readCommand{serverVersion, socket.Endpoint, ""})
	if e != nil {
		t.Fatal(PublicError(e))
	}
	var v struct {
		Version string `json:"version"`
		OS      string `json:"os"`
		Arch    string `json:"arch"`
	}
	if json.Unmarshal(b, &v) != nil {
		t.Fatal("version decode")
	}
	b, e = r.read(ctx, readCommand{serverIdentity, socket.Endpoint, ""})
	var id string
	if e != nil || jsonstrictString(b, &id) != nil {
		t.Fatal("identity unavailable")
	}
	snap := testSnapshot(t)
	snap.Socket = socket
	snap.DaemonID = id
	snap.Runtime.DockerVersion = v.Version
	snap.Runtime.OS = v.OS
	snap.Runtime.Arch = v.Arch
	p, e := buildPlan(ctx, descriptor(t), snap, fixedEntropy())
	if e != nil {
		t.Fatal("synthetic plan construction")
	}
	if _, e = i.runtimeWitness(ctx, p); e != nil {
		t.Fatal("read-only binding did not remain consistent")
	}
	p.PrivateBinding.SocketInode++
	raw, _ := json.Marshal(p.payload)
	p.AuthorizationSHA256 = hash(raw)
	if _, e = i.runtimeWitness(ctx, p); e == nil {
		t.Fatal("changed socket binding accepted")
	}
	t.Logf("actual read-only runtime: Docker %s %s/%s; no images/resources queried; NOT runtime-qualified", v.Version, v.OS, v.Arch)
}
