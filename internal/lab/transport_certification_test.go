package lab

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

type fakeEpoch struct {
	value string
	err   error
}

func (e *fakeEpoch) epoch(context.Context, runtimeWitness) (string, error) { return e.value, e.err }

// Recording world; not Docker, a qualified epoch or measured application data.
type certificationPeer struct {
	model     Journal
	calls     []readCommand
	resources map[string][]byte
	present   map[string]bool
	change    func(readCommand, []byte) ([]byte, error)
}

func newCertificationPeer(t *testing.T, j Journal) *certificationPeer {
	t.Helper()
	p := &certificationPeer{model: j, resources: map[string][]byte{}, present: map[string]bool{}}
	s, _ := j.State()
	for _, r := range s.Resources {
		if r.Proof.ID != "" {
			p.install(t, j, r.Key, r.Proof.ID)
		}
	}
	return p
}
func (p *certificationPeer) install(t *testing.T, j Journal, k ResourceKey, id string) {
	t.Helper()
	s, _ := j.State()
	model := j
	if s.Resources[resourceIndex(k)].Proof.ID == "" {
		g, _ := GenerateGraph(j.Plan)
		name, cfg, _ := g.resource(k)
		var e error
		model, e = j.Advance(authorizationFor(j.Plan), Event{Operation: Created, Key: k, Proof: Proof{id, name, g.OwnerNonce, cfg, false}})
		if e != nil {
			t.Fatal(e)
		}
	}
	var actual any
	if resourceIndex(k) < 4 {
		actual = projectedNetworkNoTest(model, k)
	} else {
		actual = projectedContainerNoTest(model, k)
	}
	p.resources[id], _ = json.Marshal(actual)
	p.present[id] = true
}
func (p *certificationPeer) read(ctx context.Context, c readCommand) ([]byte, error) {
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	if _, e := c.args(); e != nil {
		return nil, e
	}
	p.calls = append(p.calls, c)
	var b []byte
	var err error
	switch c.op {
	case containerPresence, networkPresence:
		if p.present[c.value] {
			b, _ = json.Marshal(c.value)
			b = append(b, '\n')
		}
	case networkActual, appActual, runnerActual, fixtureActual, proxyActual:
		var ok bool
		b, ok = p.resources[c.value]
		if !ok {
			err = ErrInspection
		}
	default:
		b, err = (&recordingReader{}).read(ctx, c)
	}
	if p.change != nil {
		return p.change(c, b)
	}
	return b, err
}
func fakeCertifier(p *certificationPeer, clock *time.Time, epoch *fakeEpoch) certifier {
	return certifier{inspector{p, selection{}, func(e string) (socketIdentity, error) { return socketIdentity{e, 7, 9}, nil }}, epoch, func() time.Time { return *clock }}
}
func creationLease(t *testing.T, key ResourceKey) *TrustedLease {
	t.Helper()
	a, _ := authorityFixture(t)
	p := syntheticPlan(t)
	l, e := a.begin(context.Background(), p, fakeWitness(p), false)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(l.Close)
	if l.Append(context.Background(), Event{Operation: Prepare}) != nil || l.Append(context.Background(), Event{Operation: CreateIntent, Key: key}) != nil {
		t.Fatal("intent")
	}
	return l
}
func prepareCreation(t *testing.T, l *TrustedLease, key ResourceKey) (CommandPlan, PreparedAttempt) {
	t.Helper()
	m, e := l.PlanCreation(context.Background(), key)
	if e != nil {
		t.Fatal(e)
	}
	command, e := l.PrepareCommand(context.Background(), m)
	if e != nil {
		t.Fatal(e)
	}
	ticket, e := l.PrepareAttempt(context.Background(), command)
	if e != nil {
		t.Fatal(e)
	}
	return command, ticket
}
func activateFake(t *testing.T, l *TrustedLease, ticket PreparedAttempt, c certifier) {
	t.Helper()
	if e := l.activateAttempt(context.Background(), ticket, c); e != nil {
		t.Fatal(e)
	}
	if e := l.dispatchPermit(context.Background(), ticket, c.now()); e != nil {
		t.Fatal(e)
	}
}
func removalSetup(t *testing.T) (*TrustedLease, CommandPlan, PreparedAttempt, *certificationPeer, certifier) {
	t.Helper()
	l := syntheticOwnedLease(t)
	ctx := context.Background()
	if l.Append(ctx, Event{Operation: CleanupBegin}) != nil {
		t.Fatal("cleanup")
	}
	v := verifiedFixture(t, l, Application)
	if l.Append(ctx, Event{Operation: DeleteIntent, Key: Application, Proof: v.proof}) != nil {
		t.Fatal("remove intent")
	}
	v = verifiedFixture(t, l, Application)
	m, e := l.PlanCleanup(ctx, v)
	if e != nil {
		t.Fatal(e)
	}
	command, e := l.PrepareCommand(ctx, m)
	if e != nil {
		t.Fatal(e)
	}
	ticket, e := l.PrepareAttempt(ctx, command)
	if e != nil {
		t.Fatal(e)
	}
	peer := newCertificationPeer(t, l.journal)
	clock := time.Now()
	c := fakeCertifier(peer, &clock, &fakeEpoch{value: strings.Repeat("e", 64)})
	return l, command, ticket, peer, c
}
func TestClosedCommandDeterminismAndTamperRefusal(t *testing.T) {
	l := creationLease(t, Edge)
	m, _ := l.PlanCreation(context.Background(), Edge)
	a, e := l.PrepareCommand(context.Background(), m)
	if e != nil {
		t.Fatal(e)
	}
	b, e := l.PrepareCommand(context.Background(), m)
	if e != nil || !equalArguments(a.argv, b.argv) || a.digest != b.digest {
		t.Fatal("nondeterministic")
	}
	if !strings.Contains(strings.Join(a.argv, " "), "network create --driver=bridge --internal") {
		t.Fatal(a.argv)
	}
	var decoded CommandPlan
	if json.Unmarshal([]byte(`{"argv":["run","--privileged"]}`), &decoded) == nil {
		t.Fatal("forged command")
	}
	a.argv = append(append([]string{}, a.argv...), "--privileged")
	if _, e = l.PrepareAttempt(context.Background(), a); e == nil {
		t.Fatal("tampered argv")
	}
	bad := m
	bad.description.Network = &PlannedNetwork{Key: Edge, Name: "foreign"}
	if _, e = l.PrepareCommand(context.Background(), bad); e == nil {
		t.Fatal("modified network")
	}
	if l.Append(context.Background(), Event{Operation: CleanupBegin}) != nil {
		t.Fatal("transition")
	}
	if _, e = l.PrepareCommand(context.Background(), m); e == nil {
		t.Fatal("stale mutation")
	}
	for _, args := range [][]byte{[]byte(`{}`), []byte(`{"kind":"create_network"}`)} {
		var ticket PreparedAttempt
		var certificate ResultCertificate
		if json.Unmarshal(args, &ticket) == nil || json.Unmarshal(args, &certificate) == nil {
			t.Fatal("manufactured authority")
		}
	}
}
func TestCertifiedCreationRequiresResponseAndFreshMetadata(t *testing.T) {
	l := creationLease(t, Edge)
	_, ticket := prepareCreation(t, l, Edge)
	peer := newCertificationPeer(t, l.journal)
	clock := time.Now()
	c := fakeCertifier(peer, &clock, &fakeEpoch{value: strings.Repeat("e", 64)})
	activateFake(t, l, ticket, c)
	id := strings.Repeat("a", 64)
	peer.install(t, l.journal, Edge, id)
	result := l.certifyResult(context.Background(), ticket, mutationResponse{0, []byte(id + "\n"), false}, c)
	if result.Summary().Status != CertifiedCreation {
		t.Fatal(result.Summary())
	}
	if l.CommitCertificate(context.Background(), result) != nil {
		t.Fatal("commit")
	}
	s, _ := l.journal.State()
	if s.Resources[0].State != "observed" || s.Resources[0].Proof.ID != id {
		t.Fatal("identity not recorded")
	}
	if l.CommitCertificate(context.Background(), result) == nil {
		t.Fatal("certificate replay")
	}
	for _, call := range peer.calls {
		if _, e := call.args(); e != nil {
			t.Fatal(e)
		}
	}
}
func TestCreationFailureResponsesNeverBecomeSuccess(t *testing.T) {
	cases := []struct {
		name     string
		response mutationResponse
		change   func(*certificationPeer, string)
	}{
		{"lost", mutationResponse{0, nil, true}, nil}, {"nonzero", mutationResponse{1, []byte(strings.Repeat("a", 64)), false}, nil}, {"malformed", mutationResponse{0, []byte("not-an-id\n"), false}, nil}, {"multiple", mutationResponse{0, bytes.Repeat([]byte("a"), 130), false}, nil},
		{"wrong-id", mutationResponse{0, []byte(strings.Repeat("a", 64)), false}, func(p *certificationPeer, id string) { p.resources[id] = []byte(`{}`) }},
		{"foreign-name", mutationResponse{0, []byte(strings.Repeat("a", 64)), false}, func(p *certificationPeer, id string) {
			var v networkObservation
			_ = json.Unmarshal(p.resources[id], &v)
			v.Name = "foreign"
			p.resources[id], _ = json.Marshal(v)
		}},
		{"attached", mutationResponse{0, []byte(strings.Repeat("a", 64)), false}, func(p *certificationPeer, id string) {
			var v networkObservation
			_ = json.Unmarshal(p.resources[id], &v)
			v.Containers = []string{strings.Repeat("b", 64)}
			p.resources[id], _ = json.Marshal(v)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			l := creationLease(t, Edge)
			_, ticket := prepareCreation(t, l, Edge)
			peer := newCertificationPeer(t, l.journal)
			clock := time.Now()
			c := fakeCertifier(peer, &clock, &fakeEpoch{value: strings.Repeat("e", 64)})
			activateFake(t, l, ticket, c)
			id := strings.Repeat("a", 64)
			peer.install(t, l.journal, Edge, id)
			if tc.change != nil {
				tc.change(peer, id)
			}
			result := l.certifyResult(context.Background(), ticket, tc.response, c)
			if result.Summary().Status != UnknownResult {
				t.Fatal(result.Summary())
			}
			if l.CommitCertificate(context.Background(), result) != nil {
				t.Fatal("unknown commit")
			}
			s, _ := l.journal.State()
			if s.Resources[0].State != "creation_unknown" || s.Resources[0].Proof.ID != "" {
				t.Fatal("adopted unknown resource")
			}
		})
	}
}
func TestCertifiedAbsenceIsNotExitCodeOrInspectFailure(t *testing.T) {
	for _, mode := range []string{"absent", "present", "failed-response", "lost-response", "inconsistent", "daemon-unavailable", "restart", "missing-continuity"} {
		t.Run(mode, func(t *testing.T) {
			l, _, ticket, peer, c := removalSetup(t)
			activateFake(t, l, ticket, c)
			id := l.journalResourceProof(Application).ID
			reply := mutationResponse{0, []byte(id + "\n"), false}
			peer.present[id] = false
			switch mode {
			case "present":
				peer.present[id] = true
			case "failed-response":
				reply.code = 1
			case "lost-response":
				reply.interrupted = true
			case "inconsistent":
				n := 0
				peer.change = func(cmd readCommand, b []byte) ([]byte, error) {
					if cmd.op == containerPresence {
						n++
						if n == 2 {
							return json.Marshal(id)
						}
					}
					return b, nil
				}
			case "daemon-unavailable":
				peer.change = func(cmd readCommand, b []byte) ([]byte, error) {
					if cmd.op == containerPresence {
						return nil, ErrInspection
					}
					return b, nil
				}
			case "restart":
				c.continuity.(*fakeEpoch).value = strings.Repeat("f", 64)
			case "missing-continuity":
				c.continuity = nil
			}
			result := l.certifyResult(context.Background(), ticket, reply, c)
			expected := UnknownResult
			if mode == "absent" {
				expected = CertifiedAbsence
			}
			if mode == "present" {
				expected = StillPresent
			}
			if result.Summary().Status != expected {
				t.Fatal(result.Summary())
			}
			if e := l.CommitCertificate(context.Background(), result); e != nil {
				t.Fatal(e)
			}
			s, _ := l.journal.State()
			if (s.Resources[4].State == "absent") != (mode == "absent") {
				t.Fatal("uncertified absence")
			}
		})
	}
}
func TestAttemptWindowCancellationLeaseAndRevision(t *testing.T) {
	for _, mode := range []string{"expired", "canceled", "closed", "revision", "cleanup-only", "epoch-unavailable"} {
		t.Run(mode, func(t *testing.T) {
			l := creationLease(t, Edge)
			_, ticket := prepareCreation(t, l, Edge)
			peer := newCertificationPeer(t, l.journal)
			clock := time.Now()
			c := fakeCertifier(peer, &clock, &fakeEpoch{value: strings.Repeat("e", 64)})
			ctx := context.Background()
			if mode == "epoch-unavailable" {
				c.continuity = nil
				if l.activateAttempt(ctx, ticket, c) == nil {
					t.Fatal("unqualified continuity")
				}
				return
			}
			if e := l.activateAttempt(ctx, ticket, c); e != nil {
				t.Fatal(e)
			}
			switch mode {
			case "expired":
				clock = clock.Add(ActionWindow + time.Nanosecond)
			case "canceled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "closed":
				l.Close()
			case "revision":
				if l.Append(ctx, Event{Operation: CleanupBegin}) != nil {
					t.Fatal("cleanup")
				}
			case "cleanup-only":
				l.cleanupOnly = true
			}
			if l.dispatchPermit(ctx, ticket, clock) == nil {
				t.Fatal("invalid dispatch permit")
			}
		})
	}
}
func TestCreationWriteFailureRetainsOnlyProvenCleanupScope(t *testing.T) {
	l := creationLease(t, Edge)
	_, ticket := prepareCreation(t, l, Edge)
	peer := newCertificationPeer(t, l.journal)
	clock := time.Now()
	c := fakeCertifier(peer, &clock, &fakeEpoch{value: strings.Repeat("e", 64)})
	activateFake(t, l, ticket, c)
	id := strings.Repeat("a", 64)
	peer.install(t, l.journal, Edge, id)
	result := l.certifyResult(context.Background(), ticket, mutationResponse{0, []byte(id), false}, c)
	if result.Summary().Status != CertifiedCreation {
		t.Fatal(result.Summary())
	}
	l.authority.store.save = func(context.Context, *os.Root, string, []byte) error { return errors.New("synthetic disk full") }
	if e := l.CommitCertificate(context.Background(), result); e == nil {
		t.Fatal("write failure hidden")
	}
	j, e := l.cleanupSnapshot(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	s, _ := j.State()
	if s.Resources[0].Proof.ID != id || !l.recordingUnconfirmed {
		t.Fatal("lost proven ID")
	}
	v, e := observeRecorded(context.Background(), c.i, j, Edge)
	if e != nil {
		t.Fatal(e)
	}
	m, e := l.PlanCleanup(context.Background(), v)
	if e != nil {
		t.Fatal("cleanup suppressed", e)
	}
	if _, e = l.PrepareCommand(context.Background(), m); e != nil {
		t.Fatal(e)
	}
	if l.ConfirmCleanup(context.Background()) == nil {
		t.Fatal("unconfirmed success")
	}
}

// Only this fake records a would-be mutation. It never imports os/exec or
// connects to Docker; the concrete production invocation path remains absent.
type recordingMutationWire struct {
	calls [][]string
	reply mutationResponse
}

func (f *recordingMutationWire) exchange(command CommandPlan) mutationResponse {
	f.calls = append(f.calls, append([]string{}, command.argv...))
	return f.reply
}
func TestFullSyntheticCleanupReleaseRequiresAllCertificates(t *testing.T) {
	l := syntheticOwnedLease(t)
	ctx := context.Background()
	if l.Append(ctx, Event{Operation: CleanupBegin}) != nil {
		t.Fatal("cleanup")
	}
	peer := newCertificationPeer(t, l.journal)
	clock := time.Now()
	c := fakeCertifier(peer, &clock, &fakeEpoch{value: strings.Repeat("e", 64)})
	for _, idx := range []int{5, 4, 7, 6, 3, 2, 1, 0} {
		key := resourceKeys[idx]
		v, e := observeRecorded(ctx, c.i, l.journal, key)
		if e != nil {
			t.Fatal(e)
		}
		if l.Append(ctx, Event{Operation: DeleteIntent, Key: key, Proof: v.proof}) != nil {
			t.Fatal("intent")
		}
		v, e = observeRecorded(ctx, c.i, l.journal, key)
		if e != nil {
			t.Fatal(e)
		}
		m, e := l.PlanCleanup(ctx, v)
		if e != nil {
			t.Fatal(e)
		}
		command, e := l.PrepareCommand(ctx, m)
		if e != nil {
			t.Fatal(e)
		}
		ticket, e := l.PrepareAttempt(ctx, command)
		if e != nil {
			t.Fatal(e)
		}
		activateFake(t, l, ticket, c)
		id := l.journalResourceProof(key).ID
		fake := recordingMutationWire{reply: mutationResponse{0, []byte(id + "\n"), false}}
		reply := fake.exchange(command)
		if len(fake.calls) != 1 {
			t.Fatal("fake invocation")
		}
		peer.present[id] = false
		result := l.certifyResult(ctx, ticket, reply, c)
		if result.Summary().Status != CertifiedAbsence {
			t.Fatal(result.Summary())
		}
		if e = l.CommitCertificate(ctx, result); e != nil {
			t.Fatal(e)
		}
	}
	if l.Append(ctx, Event{Operation: CleanupComplete}) != nil {
		t.Fatal("complete")
	}
	if l.ConfirmCleanup(ctx) == nil {
		t.Fatal("default metadata provider claimed continuity")
	}
	saved := l.confirmedAbsent[Edge]
	delete(l.confirmedAbsent, Edge)
	if l.confirmCleanupLocked(ctx, &c) == nil {
		t.Fatal("released without every certificate")
	}
	l.confirmedAbsent[Edge] = saved
	if e := l.confirmCleanupLocked(ctx, &c); e != nil {
		t.Fatal(e)
	}
	l.Close()
	p := l.journal.Plan
	if _, e := l.authority.begin(ctx, p, fakeWitness(p), false); !errors.Is(e, ErrReplay) {
		t.Fatal("RUN replay", e)
	}
}
func TestAllClosedContainerAndLifecycleCommands(t *testing.T) {
	l := syntheticOwnedLease(t)
	j := l.journal
	g, _ := GenerateGraph(j.Plan)
	s, _ := j.State()
	for _, key := range resourceKeys[4:] {
		idx := resourceIndex(key)
		p := j
		p.Events = p.Events[:1]
		for _, n := range resourceKeys[:4] {
			p = advance(t, p, authorizationFor(p.Plan), Event{Operation: CreateIntent, Key: n})
			p = advance(t, p, authorizationFor(p.Plan), Event{Operation: Created, Key: n, Proof: s.Resources[resourceIndex(n)].Proof})
		}
		p = advance(t, p, authorizationFor(p.Plan), Event{Operation: CreateIntent, Key: key})
		container := g.Containers[idx-4]
		d := MutationDescription{Kind: CreateContainer, Key: key, Container: &container}
		args, e := commandArguments(p, d, false)
		if e != nil {
			t.Fatal(e)
		}
		joined := strings.Join(args, " ")
		for _, required := range []string{"container create", "--pull=never", "--platform=linux/arm64", "--cap-drop=ALL", "--read-only", "--security-opt=no-new-privileges", container.ImageID} {
			if !strings.Contains(joined, required) {
				t.Fatal(required)
			}
		}
		container.Role.Entrypoint = []string{"/bin/sh"}
		if _, e = commandArguments(p, d, false); e == nil {
			t.Fatal("argv injection")
		}
		for _, kind := range []MutationKind{StartContainer, StopContainer, RemoveContainer} {
			args, e := commandArguments(j, MutationDescription{Kind: kind, Key: key, ID: s.Resources[idx].Proof.ID}, false)
			if e != nil || len(args) < 5 {
				t.Fatal(kind, e)
			}
		}
		if _, e := commandArguments(j, MutationDescription{Kind: StopContainer, Key: key, ID: strings.Repeat("f", 64)}, false); e == nil {
			t.Fatal("foreign id")
		}
	}
	n := g.Networks[0]
	if _, e := commandArguments(j, MutationDescription{Kind: RemoveNetwork, Key: Edge, ID: s.Resources[0].Proof.ID}, true); e == nil {
		t.Fatal("removed network with possible attachments")
	}
	_ = n
}

func TestContainerCreationAndLifecycleAreNotWorkerSuccess(t *testing.T) {
	for _, state := range []string{"created", "running"} {
		t.Run(state, func(t *testing.T) {
			a, _ := authorityFixture(t)
			p := syntheticPlan(t)
			ctx := context.Background()
			l, e := a.begin(ctx, p, fakeWitness(p), false)
			if e != nil {
				t.Fatal(e)
			}
			t.Cleanup(l.Close)
			j := advance(t, l.journal, l.witness.authorization, Event{Operation: Prepare})
			for _, key := range resourceKeys[:4] {
				j = advance(t, j, l.witness.authorization, Event{Operation: CreateIntent, Key: key})
				j = advance(t, j, l.witness.authorization, Event{Operation: Created, Key: key, Proof: syntheticProof(t, j, key)})
			}
			j = advance(t, j, l.witness.authorization, Event{Operation: CreateIntent, Key: Application})
			for n := 1; n <= len(j.Events); n++ {
				x := j
				x.Events = append([]Event{}, j.Events[:n]...)
				b, _ := x.Encode()
				if publish(ctx, a.store.root, journalName(p.Identity.LaunchID, n), b) != nil {
					t.Fatal("fixture")
				}
			}
			l.journal = j
			_, ticket := prepareCreation(t, l, Application)
			peer := newCertificationPeer(t, j)
			clock := time.Now()
			c := fakeCertifier(peer, &clock, &fakeEpoch{value: strings.Repeat("e", 64)})
			activateFake(t, l, ticket, c)
			id := strings.Repeat("a", 64)
			peer.install(t, j, Application, id)
			var metadata containerObservation
			_ = json.Unmarshal(peer.resources[id], &metadata)
			metadata.State = state
			peer.resources[id], _ = json.Marshal(metadata)
			result := l.certifyResult(ctx, ticket, mutationResponse{0, []byte(id), false}, c)
			if state == "running" {
				if result.Summary().Status != UnknownResult {
					t.Fatal(result.Summary())
				}
				return
			}
			if result.Summary().Status != CertifiedCreation || l.CommitCertificate(ctx, result) != nil {
				t.Fatal(result.Summary())
			}
			for _, kind := range []MutationKind{StartContainer, StopContainer} {
				v, e := observeRecorded(ctx, c.i, l.journal, Application)
				if e != nil {
					t.Fatal(e)
				}
				m, e := l.planLifecycle(ctx, v, kind)
				if e != nil {
					t.Fatal(e)
				}
				command, e := l.PrepareCommand(ctx, m)
				if e != nil {
					t.Fatal(e)
				}
				next, e := l.PrepareAttempt(ctx, command)
				if e != nil {
					t.Fatal(e)
				}
				activateFake(t, l, next, c)
				if kind == StartContainer {
					metadata.State = "running"
				} else {
					metadata.State = "exited"
				}
				peer.resources[id], _ = json.Marshal(metadata)
				result = l.certifyResult(ctx, next, mutationResponse{0, []byte(id), false}, c)
				if result.Summary().Status != CertifiedLifecycle || l.CommitCertificate(ctx, result) == nil {
					t.Fatal("container state claimed worker success", result.Summary())
				}
			}
		})
	}
}

func TestLateResultAndCommitRefused(t *testing.T) {
	for _, stage := range []string{"result", "commit"} {
		t.Run(stage, func(t *testing.T) {
			l := creationLease(t, Edge)
			_, ticket := prepareCreation(t, l, Edge)
			peer := newCertificationPeer(t, l.journal)
			clock := time.Now()
			c := fakeCertifier(peer, &clock, &fakeEpoch{value: strings.Repeat("e", 64)})
			activateFake(t, l, ticket, c)
			id := strings.Repeat("a", 64)
			peer.install(t, l.journal, Edge, id)
			if stage == "result" {
				clock = clock.Add(ResultWindow + time.Nanosecond)
			}
			result := l.certifyResult(context.Background(), ticket, mutationResponse{0, []byte(id), false}, c)
			if stage == "result" {
				if result.Summary().Status != UnknownResult {
					t.Fatal(result.Summary())
				}
			} else {
				clock = clock.Add(10*time.Second + time.Nanosecond)
				if l.CommitCertificate(context.Background(), result) == nil {
					t.Fatal("expired certificate")
				}
			}
		})
	}
}

func TestAttemptExhaustionDoesNotSuppressExactCleanup(t *testing.T) {
	l, command, _, _, _ := removalSetup(t)
	l.transportAttempts = map[string]*attemptState{}
	for n := 0; n < MaxTransportAttempts; n++ {
		token := strings.Repeat("a", n+1)
		l.transportAttempts[token] = &attemptState{lease: l, token: token, committed: true}
	}
	ticket, e := l.PrepareAttempt(context.Background(), command)
	if e != nil || ticket.state.recorded || !l.recordingUnconfirmed {
		t.Fatal("cleanup suppressed or exhaustion hidden", e)
	}
	if l.ConfirmCleanup(context.Background()) == nil {
		t.Fatal("unrecorded cleanup released reservation")
	}
}

func TestAttemptRecordRejectsCorruptionAndReplay(t *testing.T) {
	l := creationLease(t, Edge)
	command, ticket := prepareCreation(t, l, Edge)
	root, e := attemptDirectory(l)
	if e != nil {
		t.Fatal(e)
	}
	defer root.Close()
	if _, e = nextAttemptSlot(context.Background(), root, command); !errors.Is(e, ErrCertification) {
		t.Fatal("forgotten create was replayable", e)
	}
	raw, e := readPrivate(context.Background(), root, "00.prepared.json", 4096)
	if e != nil {
		t.Fatal(e)
	}
	var rec attemptRecord
	if decodeCanonical(raw, &rec) != nil || !validAttemptRecord(rec) {
		t.Fatal("record")
	}
	for _, alter := range []func(*attemptRecord){func(r *attemptRecord) { r.Operation = "run" }, func(r *attemptRecord) { r.ID = strings.Repeat("f", 64) }, func(r *attemptRecord) { r.Resource = Application }, func(r *attemptRecord) { r.CommandSHA256 = "bad" }} {
		bad := rec
		alter(&bad)
		if validAttemptRecord(bad) {
			t.Fatal("accepted malformed attempt")
		}
	}
	ticket.state.committed = true
	if e = root.Remove("00.prepared.json"); e != nil {
		t.Fatal(e)
	}
	if e = root.Symlink("missing", "00.prepared.json"); e != nil {
		t.Fatal(e)
	}
	if _, e = nextAttemptSlot(context.Background(), root, command); e == nil {
		t.Fatal("symlinked attempt accepted")
	}
}
