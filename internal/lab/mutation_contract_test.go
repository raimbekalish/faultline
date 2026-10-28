package lab

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
)

// Explicit synthetic accepted-creation history. The production lease cannot
// certify these outcomes: that future trusted transport is intentionally absent.
func syntheticOwnedLease(t *testing.T) *TrustedLease {
	t.Helper()
	a, _ := authorityFixture(t)
	p := syntheticPlan(t)
	l, e := a.begin(context.Background(), p, fakeWitness(p), false)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(l.Close)
	j := createAll(t, l.journal, l.witness.authorization)
	for n := 1; n <= len(j.Events); n++ {
		x := j
		x.Events = append([]Event{}, j.Events[:n]...)
		b, e := x.Encode()
		if e != nil {
			t.Fatal(e)
		}
		if publish(context.Background(), a.store.root, journalName(p.Identity.LaunchID, n), b) != nil {
			t.Fatal("fixture publication")
		}
	}
	l.journal = j
	return l
}
func verifiedFixture(t *testing.T, l *TrustedLease, k ResourceKey) VerifiedResource {
	t.Helper()
	r := &observationReader{j: l.journal}
	v, e := observeRecorded(context.Background(), recordingObserver(r), l.journal, k)
	if e != nil {
		t.Fatal(e)
	}
	return v
}

type mutationRecorder struct{ calls []MutationDescription }

func (r *mutationRecorder) Apply(ctx context.Context, m MutationIntent) error {
	if e := ctx.Err(); e != nil {
		return e
	}
	if !m.valid() {
		return ErrMutationContract
	}
	r.calls = append(r.calls, m.Description())
	return nil
}

var _ ClosedMutationTransport = (*mutationRecorder)(nil)

func TestClosedMutationPlansAndNoUncertifiedSuccess(t *testing.T) {
	a, _ := authorityFixture(t)
	p := syntheticPlan(t)
	ctx := context.Background()
	l, e := a.begin(ctx, p, fakeWitness(p), false)
	if e != nil {
		t.Fatal(e)
	}
	defer l.Close()
	if l.Append(ctx, Event{Operation: Prepare}) != nil || l.Append(ctx, Event{Operation: CreateIntent, Key: Edge}) != nil {
		t.Fatal("prepare")
	}
	m, e := l.PlanCreation(ctx, Edge)
	if e != nil || !m.valid() {
		t.Fatal(e)
	}
	v := m.Description()
	if v.Kind != CreateNetwork || v.Network == nil || v.Network.Profile.GatewayMode != "isolated" || v.ExecutionAvailable {
		t.Fatal(v)
	}
	v.Network.Profile.Internal = false
	if !m.Description().Network.Profile.Internal {
		t.Fatal("mutable description changed intent")
	}
	recorder := &mutationRecorder{}
	if recorder.Apply(ctx, m) != nil || len(recorder.calls) != 1 {
		t.Fatal("recording only")
	}
	var forged MutationIntent
	if json.Unmarshal([]byte(`{"kind":"create_container","execution_available":true}`), &forged) == nil {
		t.Fatal("accepted serialized mutation authority")
	}
	if recorder.Apply(ctx, forged) == nil {
		t.Fatal("forged execution")
	}
	if l.Append(ctx, Event{Operation: Created, Key: Edge, Proof: syntheticProof(t, l.journal, Edge)}) == nil {
		t.Fatal("caller assertion certified creation")
	}
	if _, e = l.PlanCreation(ctx, "foreign"); e == nil {
		t.Fatal("foreign creation")
	}
}
func TestCleanupPlansSurviveStateIOFailureWithoutReceipt(t *testing.T) {
	l := syntheticOwnedLease(t)
	ctx := context.Background()
	v := verifiedFixture(t, l, Application)
	m, e := l.PlanCleanup(ctx, v)
	if e != nil || m.Description().ID != v.proof.ID || m.Description().ExecutionAvailable {
		t.Fatal(e)
	}
	l.authority.store.save = func(context.Context, *os.Root, string, []byte) error { return errors.New("synthetic disk failure") }
	if l.Append(ctx, Event{Operation: CleanupBegin}) == nil {
		t.Fatal("write failure hidden")
	}
	m, e = l.PlanCleanup(ctx, v)
	if e != nil || m.Description().JournalRecording != "cleanup_result_not_confirmed" {
		t.Fatal("cleanup suppressed by disk failure", e)
	}
	if l.ConfirmCleanup(ctx) == nil {
		t.Fatal("uncertainty released reservation")
	}
	if _, e = l.PlanCreation(ctx, Edge); e == nil {
		t.Fatal("uncertainty enabled provisioning")
	}
	bad := v
	bad.proof.ID = strings.Repeat("f", 64)
	if _, e = l.PlanCleanup(ctx, bad); e == nil {
		t.Fatal("foreign ID")
	}
	l.Close()
	if _, e = l.PlanCleanup(ctx, v); e == nil {
		t.Fatal("closed lease")
	}
}
func TestLegacyBudgetExhaustionStillPermitsUnconfirmedScopedCleanup(t *testing.T) {
	l := syntheticOwnedLease(t)
	j := l.journal
	a := l.witness.authorization
	// Bypass new admission only to model a valid legacy pre-budget history.
	j.Events = append(j.Events, Event{Operation: WorkerIntent}, Event{Operation: WorkerRunning})
	for len(j.Events) < MaxJournalEvents-1 {
		j.Events = append(j.Events, Event{Operation: ProxyCleanupIntent}, Event{Operation: ProxyUnconfirmed, Reason: "transport_failure"})
	}
	if len(j.Events) < MaxJournalEvents {
		j.Events = append(j.Events, Event{Operation: CleanupBegin})
	}
	if j.Validate() != nil || len(j.Events) != MaxJournalEvents {
		t.Fatal("invalid legacy fixture")
	}
	for n := len(l.journal.Events) + 1; n <= len(j.Events); n++ {
		prefix := j
		prefix.Events = append([]Event{}, j.Events[:n]...)
		b, _ := prefix.Encode()
		if publish(context.Background(), l.authority.store.root, journalName(j.Plan.Identity.LaunchID, n), b) != nil {
			t.Fatal("fixture")
		}
	}
	l.journal = j
	v := verifiedFixture(t, l, Application)
	if _, e := j.Advance(a, Event{Operation: DeleteIntent, Key: Application, Proof: v.proof}); e == nil {
		t.Fatal("invented event capacity")
	}
	if _, e := l.PlanCleanup(context.Background(), v); e != nil {
		t.Fatal("exhaustion suppressed cleanup planning", e)
	}
	if l.ConfirmCleanup(context.Background()) == nil {
		t.Fatal("fabricated final cleanup")
	}
}
func TestImportedHistoryNeverAdoptsForeignResources(t *testing.T) {
	a, _ := authorityFixture(t)
	j := fullJournal(t)
	l, e := a.importVerified(context.Background(), j, fakeWitness(j.Plan))
	if e != nil {
		t.Fatal(e)
	}
	defer l.Close()
	v := verifiedFixture(t, l, Application)
	if _, e = l.PlanCleanup(context.Background(), v); e == nil {
		t.Fatal("import conferred ownership")
	}
	if _, e = a.begin(context.Background(), j.Plan, fakeWitness(j.Plan), true); e == nil {
		t.Fatal("import converted to recovery authority")
	}
}

func TestRuntimeChangesInvalidatePreparedBoundaries(t *testing.T) {
	l := syntheticOwnedLease(t)
	v := verifiedFixture(t, l, Application)
	l.authority.probeRuntime = func(_ context.Context, p Plan) (runtimeWitness, error) {
		w := fakeWitness(p)
		w.key = strings.Repeat("f", 64)
		return w, nil
	}
	if _, e := l.PlanCleanup(context.Background(), v); !errors.Is(e, ErrAuthorization) {
		t.Fatal("changed runtime accepted", e)
	}
}
