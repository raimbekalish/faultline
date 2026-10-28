package lab

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

var ErrMutationContract = errors.New("untrusted or unavailable lab mutation intent")

type MutationKind string

const (
	CreateNetwork   MutationKind = "create_network"
	CreateContainer MutationKind = "create_container"
	StartContainer  MutationKind = "start_container"
	StopContainer   MutationKind = "stop_container"
	RemoveContainer MutationKind = "remove_container"
	RemoveNetwork   MutationKind = "remove_network"
)

// MutationIntent has no argv/URL/mount/plugin fields. It is inspectable intended
// work only. A concrete executable transport does not exist in this milestone.
type MutationDescription struct {
	Kind               MutationKind      `json:"kind"`
	Key                ResourceKey       `json:"key"`
	ID                 string            `json:"id"`
	Network            *PlannedNetwork   `json:"network,omitempty"`
	Container          *PlannedContainer `json:"container,omitempty"`
	ExecutionAvailable bool              `json:"execution_available"`
	JournalRecording   string            `json:"journal_recording"`
}

// Only a lease factory can construct a useful intent. A caller may inspect a
// detached description, but cannot inject argv/images/mounts back into it.
type MutationIntent struct {
	description MutationDescription
	lease       *TrustedLease
}

func (m MutationIntent) MarshalJSON() ([]byte, error) { return json.Marshal(m.description) }
func (m *MutationIntent) UnmarshalJSON([]byte) error  { return ErrMutationContract }
func (m MutationIntent) Description() MutationDescription {
	b, _ := json.Marshal(m.description)
	var copy MutationDescription
	_ = json.Unmarshal(b, &copy)
	return copy
}
func (m MutationIntent) valid() bool {
	if m.lease == nil {
		return false
	}
	m.lease.mu.Lock()
	defer m.lease.mu.Unlock()
	return !m.lease.closed && !m.lease.imported && m.lease.authority != nil && !m.lease.authority.closed.Load() && !m.description.ExecutionAvailable && resourceIndex(m.description.Key) >= 0
}

// ClosedMutationTransport is a future typed boundary. Only recording fakes in
// tests implement it. Nothing invokes it from CLI or production orchestration.
type ClosedMutationTransport interface {
	Apply(context.Context, MutationIntent) error
}

func (l *TrustedLease) live(ctx context.Context) (Journal, error) {
	if l == nil || l.authority == nil || l.closed || l.imported || l.unlock == nil {
		return Journal{}, ErrLease
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	unlock, e := l.authority.store.lock(ctx)
	if e != nil {
		return Journal{}, e
	}
	defer unlock()
	_, records, e := l.authority.registry(ctx)
	r, ok := records[l.journal.Plan.Identity.LaunchID]
	if e != nil || !ok || r.closed || r.record.PlanSHA256 != l.journal.Plan.AuthorizationSHA256 || r.record.RuntimeKey != l.witness.key {
		return Journal{}, ErrAuthority
	}
	j, e := l.authority.store.load(ctx, l.journal.Plan.Identity.LaunchID)
	if e != nil || !journalsEqual(j, l.journal) {
		return Journal{}, ErrAuthority
	}
	return j, nil
}
func (l *TrustedLease) Observe(ctx context.Context, key ResourceKey) (VerifiedResource, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	j, e := l.cleanupSnapshot(ctx)
	if e != nil {
		return VerifiedResource{}, e
	}
	r, e := newCLIReader()
	if e != nil {
		return VerifiedResource{}, e
	}
	return observeRecorded(ctx, inspector{r, selectionFromEnvironment(), inspectSocket}, j, key)
}
func (l *TrustedLease) PlanCreation(ctx context.Context, key ResourceKey) (MutationIntent, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	j, e := l.live(ctx)
	if e != nil || l.cleanupOnly || l.recordingUnconfirmed {
		return MutationIntent{}, ErrMutationContract
	}
	s, _ := j.State()
	idx := resourceIndex(key)
	if idx < 0 || s.Resources[idx].State != "create_intent" {
		return MutationIntent{}, ErrMutationContract
	}
	if e := l.checkRuntime(ctx); e != nil {
		return MutationIntent{}, e
	}
	g, _ := GenerateGraph(j.Plan)
	m := MutationDescription{Key: key, JournalRecording: "intent_durable"}
	if idx < 4 {
		m.Kind = CreateNetwork
		n := g.Networks[idx]
		m.Network = &n
	} else {
		m.Kind = CreateContainer
		c := g.Containers[idx-4]
		m.Container = &c
	}
	return MutationIntent{m, l}, nil
}

// PlanCleanup uses only previously proven exact identities and a fresh opaque
// verified observation. It remains available after journal exhaustion/write
// failure; it does not fabricate a durable success/receipt or release a lease.
func (l *TrustedLease) PlanCleanup(ctx context.Context, v VerifiedResource) (MutationIntent, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	j, e := l.cleanupSnapshot(ctx)
	if e != nil {
		return MutationIntent{}, e
	}
	if v.runtime != l.witness || v.journalSHA == "" {
		return MutationIntent{}, ErrMutationContract
	}
	h, _ := j.Hash()
	if v.journalSHA != h {
		return MutationIntent{}, ErrMutationContract
	}
	s, e := j.State()
	g, _ := GenerateGraph(j.Plan)
	if e != nil || !cleanupProof(s, g, v.key, v.proof) {
		return MutationIntent{}, ErrMutationContract
	}
	if e := l.checkRuntime(ctx); e != nil {
		return MutationIntent{}, e
	}
	kind := RemoveContainer
	if resourceIndex(v.key) < 4 {
		kind = RemoveNetwork
	}
	return MutationIntent{MutationDescription{Kind: kind, Key: v.key, ID: v.proof.ID, ExecutionAvailable: false, JournalRecording: "cleanup_result_not_confirmed"}, l}, nil
}
func (l *TrustedLease) planLifecycle(ctx context.Context, v VerifiedResource, kind MutationKind) (MutationIntent, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	j, e := l.live(ctx)
	if e != nil || resourceIndex(v.key) < 4 || v.runtime != l.witness {
		return MutationIntent{}, ErrMutationContract
	}
	h, _ := j.Hash()
	if v.journalSHA != h {
		return MutationIntent{}, ErrMutationContract
	}
	s, _ := j.State()
	if !sameProof(s.Resources[resourceIndex(v.key)].Proof, v.proof) {
		return MutationIntent{}, ErrMutationContract
	}
	if kind == StartContainer {
		if l.cleanupOnly || l.recordingUnconfirmed || s.Aborted || s.LabCleanup != "not_started" || s.Resources[resourceIndex(v.key)].State != "observed" {
			return MutationIntent{}, ErrMutationContract
		}
	} else if kind != StopContainer {
		return MutationIntent{}, ErrMutationContract
	}
	if e := l.checkRuntime(ctx); e != nil {
		return MutationIntent{}, e
	}
	return MutationIntent{MutationDescription{Kind: kind, Key: v.key, ID: v.proof.ID, ExecutionAvailable: false, JournalRecording: "lifecycle_result_not_confirmed"}, l}, nil
}

// A living, still-held lease retains its already durable consumed identity and
// last proven IDs when later state I/O fails. This grants cleanup planning only,
// never new provisioning, a final receipt or successful durable recording.
func (l *TrustedLease) cleanupSnapshot(ctx context.Context) (Journal, error) {
	if l == nil || l.authority == nil || l.closed || l.imported || l.unlock == nil || l.journal.Validate() != nil {
		return Journal{}, ErrLease
	}
	if !l.recordingUnconfirmed {
		if j, e := l.live(ctx); e == nil {
			return j, nil
		}
		l.recordingUnconfirmed = true
	}
	if ctx.Err() != nil {
		return Journal{}, ctx.Err()
	}
	return l.journal, nil
}
