package lab

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func consumed(t *testing.T) (Journal, Authorization) {
	t.Helper()
	p := syntheticPlan(t)
	a := authorizationFor(p)
	j, e := NewJournal(p, a, nil)
	if e != nil {
		t.Fatal(e)
	}
	return j, a
}
func advance(t *testing.T, j Journal, a Authorization, e Event) Journal {
	t.Helper()
	n, err := j.Advance(a, e)
	if err != nil {
		t.Fatalf("%s %s: %v", e.Operation, e.Key, err)
	}
	return n
}
func syntheticProof(t *testing.T, j Journal, k ResourceKey) Proof {
	t.Helper()
	g, e := GenerateGraph(j.Plan)
	if e != nil {
		t.Fatal(e)
	}
	n, h, e := g.resource(k)
	if e != nil {
		t.Fatal(e)
	}
	return Proof{ID: fmt.Sprintf("%064x", resourceIndex(k)+1), Name: n, OwnerNonce: j.Plan.Identity.AuthorizationNonce, ConfigurationSHA256: h}
}
func createAll(t *testing.T, j Journal, a Authorization) Journal {
	t.Helper()
	j = advance(t, j, a, Event{Operation: Prepare})
	for _, k := range resourceKeys {
		j = advance(t, j, a, Event{Operation: CreateIntent, Key: k})
		j = advance(t, j, a, Event{Operation: Created, Key: k, Proof: syntheticProof(t, j, k)})
	}
	return j
}
func cleanupKnown(t *testing.T, j Journal, a Authorization) Journal {
	t.Helper()
	s, _ := j.State()
	if s.LabCleanup != "active" {
		j = advance(t, j, a, Event{Operation: CleanupBegin})
	}
	for _, i := range []int{5, 4, 7, 6, 3, 2, 1, 0} {
		s, _ = j.State()
		r := s.Resources[i]
		if r.Proof.ID == "" || r.State == "absent" {
			continue
		}
		p := r.Proof
		p.NetworkEmpty = i < 4
		if !j.CleanupEligible(a, r.Key, p) {
			blocked := false
			for _, c := range s.Resources[4:] {
				if c.State == "creation_unknown" || c.State == "create_intent" {
					blocked = true
				}
			}
			if i < 4 && blocked {
				continue
			} // preserve networks with ambiguous attachments
			t.Fatal("not eligible", r.Key)
		}
		j = advance(t, j, a, Event{Operation: DeleteIntent, Key: r.Key, Proof: p})
		j = advance(t, j, a, Event{Operation: DeleteAbsent, Key: r.Key, Proof: p})
	}
	return j
}
func TestJournalNormalLifecycleAndReceipt(t *testing.T) {
	j, a := consumed(t)
	j = createAll(t, j, a)
	for _, op := range []Operation{WorkerIntent, WorkerRunning, ProxyCleanupIntent, ProxyAbsent, WorkerExited} {
		j = advance(t, j, a, Event{Operation: op})
	}
	j = cleanupKnown(t, j, a)
	j = advance(t, j, a, Event{Operation: CleanupComplete})
	s, _ := j.State()
	if s.Worker != "exited" || s.ProxyCleanup != "complete" || s.LabCleanup != "complete" {
		t.Fatal(s)
	}
	r, e := j.Receipt(a)
	if e != nil || r.Qualification != "NOT_PERFORMED" {
		t.Fatal(r, e)
	}
	b, e := j.Encode()
	if e != nil {
		t.Fatal(e)
	}
	x, e := DecodeJournal(context.Background(), bytes.NewReader(b))
	if e != nil || !journalsEqual(x, j) {
		t.Fatal(e)
	}
	again, _ := x.Encode()
	if !bytes.Equal(b, again) {
		t.Fatal("nondeterminism")
	}
	if _, e := NewJournal(j.Plan, a, &j); !errors.Is(e, ErrReplay) {
		t.Fatal("completed replay", e)
	}
	if _, e := j.Advance(a, Event{Operation: CleanupBegin}); e == nil {
		t.Fatal("terminal state reopened")
	}
}
func TestJournalAuthorizationAndCleanupScope(t *testing.T) {
	j, a := consumed(t)
	j = createAll(t, j, a)
	j = advance(t, j, a, Event{Operation: CleanupBegin})
	for _, change := range []func(*Authorization){func(a *Authorization) { a.PlanSHA256 = strings.Repeat("0", 64) }, func(a *Authorization) { a.Identity.AuthorizationNonce = strings.Repeat("0", 64) }, func(a *Authorization) { a.Binding.Endpoint = "unix:///foreign.sock" }, func(a *Authorization) { a.Binding.SocketInode++ }, func(a *Authorization) { a.Binding.DaemonFingerprint = strings.Repeat("0", 64) }, func(a *Authorization) { a.Binding.Salt = strings.Repeat("0", 64) }} {
		bad := a
		change(&bad)
		if j.CleanupEligible(bad, Application, syntheticProof(t, j, Application)) {
			t.Fatal("binding mismatch allowed")
		}
		if _, e := j.Advance(bad, Event{Operation: CleanupUnconfirmed, Reason: "transport_failure"}); e == nil {
			t.Fatal("bad authorization")
		}
	}
	proof := syntheticProof(t, j, Application)
	for _, change := range []func(*Proof){func(p *Proof) { p.ID = strings.Repeat("f", 64) }, func(p *Proof) { p.Name += "-other" }, func(p *Proof) { p.OwnerNonce = strings.Repeat("f", 64) }, func(p *Proof) { p.ConfigurationSHA256 = strings.Repeat("f", 64) }, func(p *Proof) { p.NetworkEmpty = true }} {
		bad := proof
		change(&bad)
		if j.CleanupEligible(a, Application, bad) {
			t.Fatal("foreign resource eligible")
		}
	}
	if j.CleanupEligible(a, "foreign", proof) {
		t.Fatal("unknown key")
	}
	net := syntheticProof(t, j, Edge)
	net.NetworkEmpty = true
	if j.CleanupEligible(a, Edge, net) {
		t.Fatal("network before containers")
	}
	// An initial/create-intent journal is consumption, not proof of a resource ID.
	initial, _ := NewJournal(j.Plan, a, nil)
	initial = advance(t, initial, a, Event{Operation: Prepare})
	initial = advance(t, initial, a, Event{Operation: CreateIntent, Key: Edge})
	initial = advance(t, initial, a, Event{Operation: CleanupBegin})
	if initial.CleanupEligible(a, Application, proof) {
		t.Fatal("adopted by name/label")
	}
}
func TestUnknownCreationCannotBeAdoptedOrConfirmedClean(t *testing.T) {
	j, a := consumed(t)
	j = advance(t, j, a, Event{Operation: Prepare})
	j = advance(t, j, a, Event{Operation: CreateIntent, Key: Edge})
	j = advance(t, j, a, Event{Operation: Created, Key: Edge, Proof: syntheticProof(t, j, Edge)})
	j = advance(t, j, a, Event{Operation: CreateIntent, Key: Data})
	j = advance(t, j, a, Event{Operation: CreateUnknown, Key: Data, Reason: "transport_failure"})
	if _, e := j.Advance(a, Event{Operation: Created, Key: Data, Proof: syntheticProof(t, j, Data)}); e == nil {
		t.Fatal("unknown ID adopted")
	}
	if _, e := j.Advance(a, Event{Operation: CreateIntent, Key: Edge}); e == nil {
		t.Fatal("continued provisioning after ambiguity")
	}
	j = cleanupKnown(t, j, a)
	if _, e := j.Advance(a, Event{Operation: CleanupComplete}); e == nil {
		t.Fatal("unknown creation called clean")
	}
	if _, e := j.Receipt(a); e == nil {
		t.Fatal("unknown receipt")
	}
	j = advance(t, j, a, Event{Operation: CleanupUnconfirmed, Reason: "supervisor_interrupted"})
	s, _ := j.State()
	if s.Resources[1].State != "creation_unknown" {
		t.Fatal("ambiguity lost")
	}
}
func TestCleanupRetryAndWorkerInterruption(t *testing.T) {
	j, a := consumed(t)
	j = createAll(t, j, a)
	j = advance(t, j, a, Event{Operation: WorkerIntent})
	j = advance(t, j, a, Event{Operation: WorkerUnknown, Reason: "supervisor_interrupted"})
	j = advance(t, j, a, Event{Operation: ProxyCleanupIntent})
	j = advance(t, j, a, Event{Operation: ProxyUnconfirmed, Reason: "transport_failure"})
	j = advance(t, j, a, Event{Operation: CleanupBegin})
	p := syntheticProof(t, j, Application)
	j = advance(t, j, a, Event{Operation: DeleteIntent, Key: Application, Proof: p})
	j = advance(t, j, a, Event{Operation: DeleteUnknown, Key: Application, Proof: p, Reason: "transport_failure"})
	j = advance(t, j, a, Event{Operation: CleanupUnconfirmed, Reason: "transport_failure"})
	j = cleanupKnown(t, j, a)
	j = advance(t, j, a, Event{Operation: CleanupComplete})
	r, e := j.Receipt(a)
	if e != nil || r.ProxyCleanup != "unconfirmed" {
		t.Fatal("proxy failure hidden", r, e)
	}
	s, _ := j.State()
	if s.Worker != "unknown" {
		t.Fatal("invented worker success")
	}
}
func TestJournalInvalidTransitionsAndParser(t *testing.T) {
	j, a := consumed(t)
	for _, e := range []Event{{Operation: Created, Key: Application, Proof: syntheticProof(t, j, Application)}, {Operation: WorkerRunning}, {Operation: ProxyAbsent}, {Operation: CleanupComplete}, {Operation: Prepare, Reason: "unexpected"}, {Operation: Prepare, Key: Edge}, {Operation: "arbitrary"}} {
		if _, err := j.Advance(a, e); err == nil {
			t.Fatal("invalid accepted", e)
		}
	}
	j = createAll(t, j, a)
	// Duplicate ID even with the intended name/owner/config is not ownership.
	x, _ := NewJournal(j.Plan, a, nil)
	x = advance(t, x, a, Event{Operation: Prepare})
	for _, k := range []ResourceKey{Edge, Data} {
		x = advance(t, x, a, Event{Operation: CreateIntent, Key: k})
		p := syntheticProof(t, x, k)
		p.ID = strings.Repeat("a", 64)
		n, e := x.Advance(a, Event{Operation: Created, Key: k, Proof: p})
		if k == Edge {
			if e != nil {
				t.Fatal(e)
			}
			x = n
		} else if e == nil {
			t.Fatal("duplicate ID accepted")
		}
	}
	b, _ := j.Encode()
	bad := [][]byte{[]byte(`{}`), []byte(`{"schema_version":1,"schema_version":1}`), bytes.Replace(b, []byte(`"schema_version"`), []byte(`"SCHEMA_VERSION"`), 1), bytes.Replace(b, []byte(`"events": [`), []byte(`"events": null, "unused": [`), 1), bytes.Replace(b, []byte(`"prepare"`), []byte(`"creation_observed"`), 1), append(b, []byte(`{}`)...), bytes.Repeat([]byte(" "), MaxJournalBytes+1), []byte("\xff"), []byte(`{"x":"\ud800"}`)}
	for _, v := range bad {
		if _, e := DecodeJournal(context.Background(), bytes.NewReader(v)); e == nil {
			t.Fatal("malformed accepted")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := DecodeJournal(ctx, bytes.NewReader(b)); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	overflow := j
	overflow.Events = make([]Event, MaxJournalEvents+1)
	if overflow.Validate() == nil {
		t.Fatal("event bound")
	}
	var generic map[string]any
	if json.Unmarshal(b, &generic) != nil {
		t.Fatal("invalid JSON")
	}
}

// Isolated synthetic transport: no exec/network/Docker client and no real
// qualification. The harness persists intent before calling these fake methods.
type syntheticTransport struct {
	calls   []string
	failAt  int
	unknown bool
	count   int
}

func (f *syntheticTransport) create(k ResourceKey) (bool, error) {
	f.calls = append(f.calls, "create:"+string(k))
	f.count++
	if f.count == f.failAt {
		return f.unknown, errors.New("synthetic failure")
	}
	return false, nil
}
func TestSyntheticPartialCreationAtEveryStage(t *testing.T) {
	for stop := 1; stop <= 8; stop++ {
		for _, unknown := range []bool{false, true} {
			t.Run(fmt.Sprintf("%d/%v", stop, unknown), func(t *testing.T) {
				j, a := consumed(t)
				j = advance(t, j, a, Event{Operation: Prepare})
				f := syntheticTransport{failAt: stop, unknown: unknown}
				for _, k := range resourceKeys {
					j = advance(t, j, a, Event{Operation: CreateIntent, Key: k})
					ambiguous, e := f.create(k)
					if e != nil {
						op := CreateAbsent
						if ambiguous {
							op = CreateUnknown
						}
						j = advance(t, j, a, Event{Operation: op, Key: k, Reason: "create_failure"})
						break
					}
					j = advance(t, j, a, Event{Operation: Created, Key: k, Proof: syntheticProof(t, j, k)})
				}
				if len(f.calls) != stop {
					t.Fatal("continued after error")
				}
				j = cleanupKnown(t, j, a)
				_, e := j.Advance(a, Event{Operation: CleanupComplete})
				if (e != nil) != unknown {
					t.Fatal("wrong cleanup outcome", e)
				}
			})
		}
	}
}
func FuzzJournalDecode(f *testing.F) {
	// Include a valid generated consumption record, not only malformed seeds.
	d, _ := DecodeDescriptor(context.Background(), strings.NewReader(goodDescriptor))
	snap, _ := testInspector(&recordingReader{}).inspect(context.Background(), d)
	p, _ := buildPlan(context.Background(), d, snap, fixedEntropy())
	seed, _ := NewJournal(p, authorizationFor(p), nil)
	valid, _ := seed.Encode()
	f.Add(valid)
	f.Add([]byte(`{}`))
	f.Add([]byte(`{"schema_version":1}`))
	f.Fuzz(func(t *testing.T, b []byte) {
		if len(b) > MaxJournalBytes+1 {
			return
		}
		j, e := DecodeJournal(context.Background(), bytes.NewReader(b))
		if e == nil {
			out, e := j.Encode()
			if e != nil {
				t.Fatal(e)
			}
			if _, e = DecodeJournal(context.Background(), bytes.NewReader(out)); e != nil {
				t.Fatal(e)
			}
		}
	})
}

func TestSyntheticCancellationAndInterruptionAtEveryIntent(t *testing.T) {
	for stop := 0; stop < 8; stop++ {
		t.Run(fmt.Sprintf("resource-%d", stop), func(t *testing.T) {
			j, a := consumed(t)
			j = advance(t, j, a, Event{Operation: Prepare})
			f := syntheticTransport{}
			for i, k := range resourceKeys {
				j = advance(t, j, a, Event{Operation: CreateIntent, Key: k})
				ctx, cancel := context.WithCancel(context.Background())
				if i == stop {
					cancel()
				}
				if ctx.Err() != nil {
					j = advance(t, j, a, Event{Operation: CreateUnknown, Key: k, Reason: "canceled"})
					cancel()
					break
				}
				_, _ = f.create(k)
				j = advance(t, j, a, Event{Operation: Created, Key: k, Proof: syntheticProof(t, j, k)})
				cancel()
			}
			if len(f.calls) != stop {
				t.Fatal("fake continued after cancellation")
			}
			j = cleanupKnown(t, j, a)
			if _, e := j.Advance(a, Event{Operation: CleanupComplete}); e == nil {
				t.Fatal("canceled ambiguous intent called clean")
			}
		})
	}
	for _, op := range []Operation{WorkerIntent, WorkerRunning} {
		t.Run(string(op), func(t *testing.T) {
			j, a := consumed(t)
			j = createAll(t, j, a)
			j = advance(t, j, a, Event{Operation: WorkerIntent})
			if op == WorkerRunning {
				j = advance(t, j, a, Event{Operation: WorkerRunning})
			}
			j = advance(t, j, a, Event{Operation: WorkerUnknown, Reason: "canceled"})
			j = cleanupKnown(t, j, a)
			j = advance(t, j, a, Event{Operation: CleanupComplete})
			r, e := j.Receipt(a)
			if e != nil || r.ProxyCleanup != "unconfirmed" {
				t.Fatal("unknown proxy hidden", e)
			}
		})
	}
}
func TestCreationProofAndDependenciesFailClosed(t *testing.T) {
	j, a := consumed(t)
	j = advance(t, j, a, Event{Operation: Prepare})
	if _, e := j.Advance(a, Event{Operation: CreateIntent, Key: Application}); e == nil {
		t.Fatal("container before owned networks")
	}
	for _, k := range resourceKeys[:4] {
		j = advance(t, j, a, Event{Operation: CreateIntent, Key: k})
		j = advance(t, j, a, Event{Operation: Created, Key: k, Proof: syntheticProof(t, j, k)})
	}
	j = advance(t, j, a, Event{Operation: CreateIntent, Key: Application})
	for _, change := range []func(*Proof){func(p *Proof) { p.Name = "reused-existing-name" }, func(p *Proof) { p.OwnerNonce = strings.Repeat("e", 64) }, func(p *Proof) { p.ConfigurationSHA256 = strings.Repeat("e", 64) }, func(p *Proof) { p.ID = "short-id" }} {
		p := syntheticProof(t, j, Application)
		change(&p)
		if _, e := j.Advance(a, Event{Operation: Created, Key: Application, Proof: p}); e == nil {
			t.Fatal("foreign observation accepted")
		}
	}
}
