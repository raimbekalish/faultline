package lab

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"
)

var ErrCertification = errors.New("uncertified, stale or unavailable Docker operation result")
var ErrContinuity = errors.New("qualified runtime continuity evidence unavailable")

const MaxTransportAttempts = 32
const ResultWindow = 45 * time.Second

type epochReader interface {
	epoch(context.Context, runtimeWitness) (string, error)
}

// No production epochReader exists. Daemon ID/socket/bridge metadata do not prove
// uninterrupted Docker lifetime. A future qualified provider requires review.
type certifier struct {
	i          inspector
	continuity epochReader
	now        func() time.Time
}

func systemCertifier() (certifier, error) {
	r, e := newCLIReader()
	if e != nil {
		return certifier{}, e
	}
	return certifier{inspector{r, selectionFromEnvironment(), inspectSocket}, nil, time.Now}, nil
}

type attemptState struct {
	lease        *TrustedLease
	command      CommandPlan
	serial       int
	token        string
	recorded     bool
	authorizedAt time.Time
	dispatchedAt time.Time
	epoch        string
	used         bool
	committed    bool
	activated    bool
}
type PreparedAttempt struct{ state *attemptState }

func (*PreparedAttempt) UnmarshalJSON([]byte) error { return ErrCertification }
func (t PreparedAttempt) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Kind               string `json:"kind"`
		ExecutionAvailable bool   `json:"execution_available"`
	}{"faultline-prepared-attempt", false})
}

type attemptRecord struct {
	SchemaVersion int          `json:"schema_version"`
	Kind          string       `json:"kind"`
	PlanSHA256    string       `json:"plan_sha256"`
	JournalSHA256 string       `json:"journal_sha256"`
	RuntimeKey    string       `json:"runtime_key"`
	CommandSHA256 string       `json:"command_sha256"`
	Token         string       `json:"token"`
	Operation     MutationKind `json:"operation"`
	Resource      ResourceKey  `json:"resource"`
	ID            string       `json:"id"`
}

// Response is deliberately private: only a separately reviewed trusted future
// transport may capture it. There is no production invocation/response adapter.
type mutationResponse struct {
	code        int
	stdout      []byte
	interrupted bool
}
type ResultStatus string

const (
	CertifiedCreation  ResultStatus = "CERTIFIED_CREATION"
	CertifiedAbsence   ResultStatus = "CERTIFIED_ABSENCE"
	CertifiedLifecycle ResultStatus = "CERTIFIED_CONTAINER_STATE"
	StillPresent       ResultStatus = "PRESENT"
	UnknownResult      ResultStatus = "UNKNOWN"
)

type CertificationSummary struct {
	SchemaVersion      int          `json:"schema_version"`
	Status             ResultStatus `json:"status"`
	Reason             string       `json:"reason"`
	Key                ResourceKey  `json:"key"`
	RuntimeQualified   bool         `json:"runtime_qualified"`
	RecordingConfirmed bool         `json:"recording_confirmed"`
}
type ResultCertificate struct {
	attempt    *attemptState
	sourceHash string
	summary    CertificationSummary
	proof      Proof
	state      string
	guard      func(context.Context) error
}

func (c ResultCertificate) Summary() CertificationSummary { return c.summary }
func (c ResultCertificate) MarshalJSON() ([]byte, error)  { return json.Marshal(c.summary) }
func (*ResultCertificate) UnmarshalJSON([]byte) error     { return ErrCertification }

func attemptDirectory(l *TrustedLease) (*os.Root, error) {
	name := "attempts-" + l.journal.Plan.Identity.LaunchID
	r := l.authority.store.root
	e := r.Mkdir(name, 0700)
	if e != nil && !errors.Is(e, os.ErrExist) {
		return nil, ErrStateIO
	}
	info, e := r.Lstat(name)
	if e != nil || !info.IsDir() || info.Mode().Perm() != 0700 || !ownedDirectory(info) {
		return nil, ErrStateIO
	}
	root, e := r.OpenRoot(name)
	if e != nil {
		return nil, ErrStateIO
	}
	current, e := root.Stat(".")
	if e != nil || !os.SameFile(info, current) {
		root.Close()
		return nil, ErrStateIO
	}
	return root, nil
}
func (l *TrustedLease) PrepareAttempt(ctx context.Context, p CommandPlan) (PreparedAttempt, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.prepareAttempt(ctx, p)
}
func (l *TrustedLease) prepareAttempt(ctx context.Context, p CommandPlan) (PreparedAttempt, error) {
	if p.lease != l || l.closed || l.imported || p.runtime != l.witness {
		return PreparedAttempt{}, ErrCertification
	}
	j, e := l.operationJournal(ctx, p.mutation.Kind)
	if e != nil {
		return PreparedAttempt{}, e
	}
	h, _ := j.Hash()
	args, e := commandArguments(j, p.mutation, l.cleanupOnly || l.recordingUnconfirmed)
	b, _ := json.Marshal(args)
	if e != nil || h != p.revision || hash(b) != p.digest || !equalArguments(args, p.argv) {
		return PreparedAttempt{}, ErrCertification
	}
	s, _ := j.State()
	idx := resourceIndex(p.mutation.Key)
	if p.mutation.Kind == RemoveContainer || p.mutation.Kind == RemoveNetwork {
		if s.Resources[idx].State != "delete_intent" && !l.recordingUnconfirmed {
			return PreparedAttempt{}, ErrCertification
		}
	}
	if l.transportAttempts == nil {
		l.transportAttempts = map[string]*attemptState{}
	}
	for _, a := range l.transportAttempts {
		if a.command.mutation.Key == p.mutation.Key && !a.committed {
			return PreparedAttempt{}, ErrCertification
		}
	}
	if len(l.transportAttempts) >= MaxTransportAttempts {
		if !cleanupKind(p.mutation.Kind) {
			return PreparedAttempt{}, ErrCertification
		}
		// Durable slots are never recycled. Under exhaustion, discard only
		// completed in-memory tickets so exact-owned cleanup can still be
		// prepared, with recording explicitly unconfirmed and no receipt.
		l.recordingUnconfirmed = true
		for token, a := range l.transportAttempts {
			if a.committed {
				delete(l.transportAttempts, token)
			}
		}
		if len(l.transportAttempts) >= MaxTransportAttempts {
			return PreparedAttempt{}, ErrCertification
		}
	}
	var token [32]byte
	if _, e = rand.Read(token[:]); e != nil {
		return PreparedAttempt{}, ErrCertification
	}
	t := &attemptState{lease: l, command: p, serial: -1, token: hex.EncodeToString(token[:])}
	root, e := attemptDirectory(l)
	if e == nil {
		defer root.Close()
		t.serial, e = nextAttemptSlot(ctx, root, p)
		if e == nil {
			rec := attemptRecord{1, "faultline-mutation-attempt", j.Plan.AuthorizationSHA256, h, l.witness.key, p.digest, t.token, p.mutation.Kind, p.mutation.Key, p.mutation.ID}
			raw, _ := json.Marshal(rec)
			e = publish(ctx, root, fmt.Sprintf("%02d.prepared.json", t.serial), raw)
		}
	}
	if e != nil {
		l.recordingUnconfirmed = true
		if !cleanupKind(p.mutation.Kind) {
			return PreparedAttempt{}, ErrStateIO
		}
	} else {
		t.recorded = !l.recordingUnconfirmed
	}
	if ctx.Err() != nil {
		return PreparedAttempt{}, ctx.Err()
	}
	l.transportAttempts[t.token] = t
	return PreparedAttempt{t}, nil
}
func equalArguments(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for n := range a {
		if a[n] != b[n] {
			return false
		}
	}
	return true
}
func (l *TrustedLease) knownAttempt(t PreparedAttempt) bool {
	return t.state != nil && t.state.lease == l && l.transportAttempts[t.state.token] == t.state && !l.closed && !l.imported
}
func (l *TrustedLease) activateAttempt(ctx context.Context, t PreparedAttempt, c certifier) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.knownAttempt(t) || t.state.used || t.state.activated || c.now == nil || c.continuity == nil {
		return ErrContinuity
	}
	a := t.state
	j, e := l.operationJournal(ctx, a.command.mutation.Kind)
	if e != nil {
		return e
	}
	h, _ := j.Hash()
	if h != a.command.revision || a.command.runtime != l.witness {
		return ErrCertification
	}
	if (l.cleanupOnly || l.recordingUnconfirmed) && !cleanupKind(a.command.mutation.Kind) {
		return ErrCertification
	}
	w, e := c.i.runtimeWitness(ctx, j.Plan)
	if e != nil || w != l.witness {
		return ErrCertification
	}
	epoch, e := c.continuity.epoch(ctx, w)
	if e != nil || !digest.MatchString(epoch) {
		return ErrContinuity
	}
	s, _ := j.State()
	idx := resourceIndex(a.command.mutation.Key)
	switch a.command.mutation.Kind {
	case CreateContainer:
		g, _ := GenerateGraph(j.Plan)
		for _, m := range g.Containers[idx-4].Memberships {
			if _, e = observeRecorded(ctx, c.i, j, m.Network); e != nil {
				return e
			}
		}
		b, err := c.i.r.read(ctx, readCommand{applicationImage, j.Plan.PrivateBinding.Endpoint, g.Containers[idx-4].ImageID})
		image, errMeta := metadata(b)
		if err != nil || errMeta != nil || image.ID != g.Containers[idx-4].ImageID {
			return ErrCertification
		}
	case StartContainer, StopContainer, RemoveContainer, RemoveNetwork:
		v, err := observeRecorded(ctx, c.i, j, a.command.mutation.Key)
		if err != nil {
			return err
		}
		if a.command.mutation.Kind == RemoveContainer || a.command.mutation.Kind == RemoveNetwork {
			g, _ := GenerateGraph(j.Plan)
			if !cleanupProof(s, g, v.key, v.proof) {
				return ErrCertification
			}
		}
	}
	again, e := c.i.runtimeWitness(ctx, j.Plan)
	last, err := c.continuity.epoch(ctx, again)
	if e != nil || err != nil || again != w || last != epoch || ctx.Err() != nil {
		return ErrContinuity
	}
	a.epoch = epoch
	a.authorizedAt = c.now()
	a.activated = true
	return nil
}

// This is a one-use handoff contract, not a Docker invocation. No production
// code calls it. A future driver must hand off immediately after this check.
func (l *TrustedLease) dispatchPermit(ctx context.Context, t PreparedAttempt, now time.Time) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if !l.knownAttempt(t) || !t.state.activated || !t.state.dispatchedAt.IsZero() || t.state.used {
		return ErrCertification
	}
	a := t.state
	if now.Before(a.authorizedAt) || now.Sub(a.authorizedAt) > ActionWindow {
		return ErrCertification
	}
	j, e := l.operationJournal(ctx, a.command.mutation.Kind)
	if e != nil {
		return e
	}
	h, _ := j.Hash()
	if h != a.command.revision || ((l.cleanupOnly || l.recordingUnconfirmed) && !cleanupKind(a.command.mutation.Kind)) {
		return ErrCertification
	}
	a.dispatchedAt = now
	return nil
}
func responseID(r mutationResponse) (string, bool) {
	if r.interrupted || r.code != 0 || len(r.stdout) > 65 {
		return "", false
	}
	b := r.stdout
	if len(b) == 65 && b[64] == '\n' {
		b = b[:64]
	}
	s := string(b)
	return s, resourceID.MatchString(s)
}
func (l *TrustedLease) certifyResult(ctx context.Context, t PreparedAttempt, r mutationResponse, c certifier) ResultCertificate {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	l.mu.Lock()
	defer l.mu.Unlock()
	unknown := func(reason string) ResultCertificate {
		return ResultCertificate{attempt: t.state, summary: CertificationSummary{1, UnknownResult, reason, func() ResourceKey {
			if t.state != nil {
				return t.state.command.mutation.Key
			}
			return ""
		}(), false, false}}
	}
	if !l.knownAttempt(t) || t.state.used || t.state.dispatchedAt.IsZero() || c.now == nil {
		return unknown("unbound_attempt")
	}
	a := t.state
	a.used = true
	if c.now().Before(a.dispatchedAt) || c.now().Sub(a.dispatchedAt) > ResultWindow || ctx.Err() != nil {
		return unknown("interrupted_or_expired")
	}
	j, e := l.operationJournal(ctx, a.command.mutation.Kind)
	if e != nil {
		return unknown("journal_unavailable")
	}
	h, _ := j.Hash()
	if h != a.command.revision {
		return unknown("stale_revision")
	}
	id, ok := responseID(r)
	if !ok {
		return unknown("ambiguous_response")
	}
	if a.command.mutation.ID != "" && id != a.command.mutation.ID {
		return unknown("response_id_mismatch")
	}
	if c.continuity == nil {
		return unknown("continuity_unavailable")
	}
	w, e := c.i.runtimeWitness(ctx, j.Plan)
	epoch, err := c.continuity.epoch(ctx, w)
	if e != nil || err != nil || w != l.witness || epoch != a.epoch {
		return unknown("runtime_changed_or_unknown")
	}
	key := a.command.mutation.Key
	var proof Proof
	var state string
	status := UnknownResult
	switch a.command.mutation.Kind {
	case CreateNetwork, CreateContainer:
		g, _ := GenerateGraph(j.Plan)
		name, cfg, _ := g.resource(key)
		claimed := Proof{id, name, g.OwnerNonce, cfg, false}
		candidate, e := j.Advance(l.witness.authorization, Event{Operation: Created, Key: key, Proof: claimed})
		if e != nil {
			return unknown("missing_creation_intent")
		}
		v, e := observeRecorded(ctx, c.i, candidate, key)
		if e != nil {
			return unknown("creation_inspection_mismatch")
		}
		if resourceIndex(key) < 4 {
			if !v.proof.NetworkEmpty {
				return unknown("unexpected_attachment")
			}
		} else if v.state != "created" {
			return unknown("container_not_initially_stopped")
		}
		proof = v.proof
		proof.NetworkEmpty = false
		status = CertifiedCreation
	case RemoveContainer, RemoveNetwork:
		if !resourceID.MatchString(id) {
			return unknown("missing_recorded_id")
		}
		if a.command.mutation.Kind == RemoveNetwork {
			s, _ := j.State()
			for _, v := range s.Resources[4:] {
				if v.State != "absent" && v.State != "uncreated" {
					return unknown("ambiguous_container_attachment")
				}
			}
		}
		presence, e := scopedPresence(ctx, c.i, j, key, id)
		if e != nil {
			return unknown("presence_query_failed")
		}
		second, e := scopedPresence(ctx, c.i, j, key, id)
		if e != nil || presence != second {
			return unknown("inconsistent_presence")
		}
		if presence {
			v, e := observeRecorded(ctx, c.i, j, key)
			if e != nil {
				return unknown("present_profile_mismatch")
			}
			proof = v.proof
			status = StillPresent
		} else {
			s, _ := j.State()
			proof = s.Resources[resourceIndex(key)].Proof
			status = CertifiedAbsence
		}
	case StartContainer, StopContainer:
		v, e := observeRecorded(ctx, c.i, j, key)
		if e != nil {
			return unknown("state_inspection_failed")
		}
		state = v.state
		if (a.command.mutation.Kind == StartContainer && state != "running") || (a.command.mutation.Kind == StopContainer && state != "exited") {
			return unknown("state_not_confirmed")
		}
		proof = v.proof
		status = CertifiedLifecycle
	default:
		return unknown("unsupported_operation")
	}
	after, e := c.i.runtimeWitness(ctx, j.Plan)
	last, err := c.continuity.epoch(ctx, after)
	if e != nil || err != nil || after != w || last != epoch || ctx.Err() != nil {
		return unknown("runtime_changed_or_unknown")
	}
	result := ResultCertificate{attempt: a, sourceHash: h, summary: CertificationSummary{1, status, "matched_scoped_observations", key, false, a.recorded}, proof: proof, state: state}
	certifiedAt := c.now()
	result.guard = func(ctx context.Context) error {
		if c.now().Before(certifiedAt) || c.now().Sub(certifiedAt) > 10*time.Second {
			return ErrCertification
		}
		current, err := c.i.runtimeWitness(ctx, j.Plan)
		epoch, epochErr := c.continuity.epoch(ctx, current)
		if err != nil || epochErr != nil || current != w || epoch != a.epoch {
			return ErrContinuity
		}
		return ctx.Err()
	}
	return result
}
func scopedPresence(ctx context.Context, i inspector, j Journal, key ResourceKey, id string) (bool, error) {
	s, e := j.State()
	idx := resourceIndex(key)
	if e != nil || idx < 0 || s.Resources[idx].Proof.ID != id || !resourceID.MatchString(id) {
		return false, ErrCertification
	}
	op := containerPresence
	if idx < 4 {
		op = networkPresence
	}
	b, e := i.r.read(ctx, readCommand{op, j.Plan.PrivateBinding.Endpoint, id})
	if e != nil || len(b) > maxInspectionBytes {
		return false, ErrInspection
	}
	if len(b) == 0 {
		return false, nil
	}
	b = bytes.TrimSuffix(b, []byte{'\n'})
	var actual string
	if jsonstrictString(b, &actual) != nil || actual != id {
		return false, ErrInspection
	}
	return true, nil
}

// No transport is invoked here. An opaque, successfully inspected certificate
// may update only the matching current private journal. Raw exit codes cannot.
func (l *TrustedLease) CommitCertificate(ctx context.Context, c ResultCertificate) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	a := c.attempt
	if a == nil || a.lease != l || l.closed || l.imported || !a.used || a.committed || l.transportAttempts[a.token] != a {
		return ErrCertification
	}
	if c.summary.Key != a.command.mutation.Key {
		return ErrCertification
	}
	if c.summary.Status == CertifiedCreation || c.summary.Status == CertifiedAbsence || c.summary.Status == CertifiedLifecycle {
		if c.guard == nil || c.guard(ctx) != nil {
			return ErrCertification
		}
	}
	a.committed = true
	if !a.recorded {
		l.recordingUnconfirmed = true
		return ErrStateIO
	}
	// Preserve newly proven cleanup scope even if reading durable history
	// fails next. This does not repair history or grant further provisioning.
	if c.summary.Status == CertifiedCreation && a.recorded {
		h, _ := l.journal.Hash()
		if h == a.command.revision && c.sourceHash == h {
			next, err := l.journal.Advance(l.witness.authorization, Event{Operation: Created, Key: c.summary.Key, Proof: c.proof})
			if err == nil {
				l.volatileCleanup = &next
			}
		}
	}
	j, e := l.live(ctx)
	if e != nil {
		l.recordingUnconfirmed = true
		return e
	}
	h, _ := j.Hash()
	if h != a.command.revision {
		return ErrCertification
	}
	event := Event{}
	switch c.summary.Status {
	case CertifiedCreation:
		if c.sourceHash != h || !a.recorded {
			return ErrCertification
		}
		event = Event{Operation: Created, Key: c.summary.Key, Proof: c.proof}
	case CertifiedAbsence:
		if c.sourceHash != h || !a.recorded {
			return ErrCertification
		}
		event = Event{Operation: DeleteAbsent, Key: c.summary.Key, Proof: c.proof}
	case UnknownResult, StillPresent:
		op := a.command.mutation.Kind
		if op == CreateNetwork || op == CreateContainer {
			event = Event{Operation: CreateUnknown, Key: a.command.mutation.Key, Reason: "transport_failure"}
		} else if op == RemoveContainer || op == RemoveNetwork {
			event = Event{Operation: DeleteUnknown, Key: a.command.mutation.Key, Proof: l.journalResourceProof(a.command.mutation.Key), Reason: "transport_failure"}
		} else {
			return ErrCertification
		}
	default:
		return ErrCertification // container state is not worker/report success
	}
	next, e := j.Advance(l.witness.authorization, event)
	if e != nil {
		l.recordingUnconfirmed = true
		return e
	}
	// Keep newly proven IDs in memory before recording. A write failure never
	// erases cleanup scope, but cannot authorize further provisioning or release.
	if c.summary.Status == CertifiedCreation {
		l.volatileCleanup = &next
	}
	root, e := attemptDirectory(l)
	if e != nil {
		l.recordingUnconfirmed = true
		return e
	}
	defer root.Close()
	raw, _ := json.Marshal(c.summary)
	if publish(ctx, root, fmt.Sprintf("%02d.result.json", a.serial), raw) != nil {
		l.recordingUnconfirmed = true
		return ErrStateIO
	}
	stored, e := l.authority.store.Append(ctx, j, l.witness.authorization, event)
	if e != nil {
		l.recordingUnconfirmed = true
		return e
	}
	l.journal = stored
	l.volatileCleanup = nil
	if c.summary.Status == CertifiedAbsence {
		if l.confirmedAbsent == nil {
			l.confirmedAbsent = map[ResourceKey]Proof{}
		}
		l.confirmedAbsent[c.summary.Key] = c.proof
	}
	return nil
}
func (l *TrustedLease) journalResourceProof(key ResourceKey) Proof {
	s, _ := l.journal.State()
	idx := resourceIndex(key)
	if idx < 0 {
		return Proof{}
	}
	return s.Resources[idx].Proof
}

func nextAttemptSlot(ctx context.Context, r *os.Root, p CommandPlan) (int, error) {
	first := -1
	for n := 0; n < MaxTransportAttempts; n++ {
		if ctx.Err() != nil {
			return -1, ctx.Err()
		}
		name := fmt.Sprintf("%02d.prepared.json", n)
		info, err := r.Lstat(name)
		if errors.Is(err, os.ErrNotExist) {
			if _, e := r.Lstat(fmt.Sprintf("%02d.result.json", n)); !errors.Is(e, os.ErrNotExist) {
				return -1, ErrStateIO
			}
			if first < 0 {
				first = n
			}
			continue
		}
		if err != nil || !privateRegular(info) {
			return -1, ErrStateIO
		}
		b, e := readPrivate(ctx, r, name, 4096)
		var old attemptRecord
		if e != nil || decodeCanonical(b, &old) != nil || !validAttemptRecord(old) || old.PlanSHA256 != p.lease.journal.Plan.AuthorizationSHA256 || old.RuntimeKey != p.runtime.key {
			return -1, ErrStateIO
		}
		// An earlier create may have executed even if its reply/commit was lost.
		// Never retry/adopt it merely because this process has an empty ticket map.
		if !cleanupKind(p.mutation.Kind) && old.Resource == p.mutation.Key && old.JournalSHA256 == p.revision {
			return -1, ErrCertification
		}
	}
	if first < 0 {
		return -1, ErrStateIO
	}
	return first, nil
}

func validAttemptRecord(r attemptRecord) bool {
	idx := resourceIndex(r.Resource)
	if r.SchemaVersion != 1 || r.Kind != "faultline-mutation-attempt" || !digest.MatchString(r.PlanSHA256) || !digest.MatchString(r.RuntimeKey) || !digest.MatchString(r.Token) || !digest.MatchString(r.CommandSHA256) || !digest.MatchString(r.JournalSHA256) || idx < 0 {
		return false
	}
	switch r.Operation {
	case CreateNetwork:
		return idx < 4 && r.ID == ""
	case CreateContainer:
		return idx >= 4 && r.ID == ""
	case StartContainer, StopContainer, RemoveContainer:
		return idx >= 4 && resourceID.MatchString(r.ID)
	case RemoveNetwork:
		return idx < 4 && resourceID.MatchString(r.ID)
	default:
		return false
	}
}

// Certification is not release authority by itself. All known resources must
// have durable certified absence plus a new bounded absence/continuity check.
// The default provider deliberately fails: metadata is not an epoch attestation.
func (l *TrustedLease) verifyFinalAbsence(ctx context.Context, c certifier) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if l.recordingUnconfirmed || c.continuity == nil {
		return ErrContinuity
	}
	s, e := l.journal.State()
	if e != nil {
		return e
	}
	w, e := c.i.runtimeWitness(ctx, l.journal.Plan)
	epoch, err := c.continuity.epoch(ctx, w)
	if e != nil || err != nil || w != l.witness || !digest.MatchString(epoch) {
		return ErrContinuity
	}
	for _, r := range s.Resources {
		if r.State == "uncreated" {
			continue
		}
		proof, ok := l.confirmedAbsent[r.Key]
		if r.State != "absent" || r.Proof.ID == "" || !ok || !sameProof(r.Proof, proof) {
			return ErrCertification
		}
		for n := 0; n < 2; n++ {
			present, e := scopedPresence(ctx, c.i, l.journal, r.Key, r.Proof.ID)
			if e != nil || present {
				return ErrCertification
			}
		}
	}
	after, e := c.i.runtimeWitness(ctx, l.journal.Plan)
	last, err := c.continuity.epoch(ctx, after)
	if e != nil || err != nil || after != w || last != epoch {
		return ErrContinuity
	}
	return ctx.Err()
}
