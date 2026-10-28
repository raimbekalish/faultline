package lab

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"regexp"

	"github.com/raimbekalish/faultline/internal/jsonstrict"
)

const MaxJournalBytes = 64 << 10
const MaxJournalEvents = 96

var ErrJournal = errors.New("invalid or unresolved lab journal")
var ErrAuthorization = errors.New("lab authorization or runtime binding mismatch")
var ErrReplay = errors.New("plan identity already consumed; RUN replay forbidden")
var resourceID = regexp.MustCompile(`^[a-f0-9]{64}$`)

// Authorization is an internal contract, not an execution capability. Binding
// facts must come from future trusted inspection, never user labels or a hash.
type Authorization struct {
	PlanSHA256 string
	Identity   Identity
	Binding    privateBinding
}

func authorizationFor(p Plan) Authorization {
	return Authorization{p.AuthorizationSHA256, p.Identity, p.PrivateBinding}
}
func (a Authorization) validate(p Plan) error {
	if p.Validate() != nil || a.PlanSHA256 != p.AuthorizationSHA256 || a.Identity != p.Identity || a.Binding != p.PrivateBinding {
		return ErrAuthorization
	}
	return nil
}

type Operation string

const (
	Prepare            Operation = "prepare"
	CreateIntent       Operation = "create_intent"
	Created            Operation = "creation_observed"
	CreateAbsent       Operation = "creation_confirmed_absent"
	CreateUnknown      Operation = "creation_unknown"
	Abort              Operation = "work_aborted"
	WorkerIntent       Operation = "worker_intent"
	WorkerRunning      Operation = "worker_running"
	WorkerExited       Operation = "worker_exited"
	WorkerUnknown      Operation = "worker_unknown"
	ProxyCleanupIntent Operation = "proxy_cleanup_intent"
	ProxyAbsent        Operation = "proxy_absence_confirmed"
	ProxyUnconfirmed   Operation = "proxy_cleanup_unconfirmed"
	CleanupBegin       Operation = "lab_cleanup_intent"
	DeleteIntent       Operation = "resource_cleanup_intent"
	DeleteAbsent       Operation = "resource_absence_confirmed"
	DeleteUnknown      Operation = "resource_cleanup_unconfirmed"
	CleanupUnconfirmed Operation = "lab_cleanup_unconfirmed"
	CleanupComplete    Operation = "lab_absence_confirmed"
)

// Proof is a future transport's verified observation of one exact ID. Its
// profile digest must be computed from complete allowlisted configuration, not
// copied from a label. This package has no production observation/delete driver.
type Proof struct {
	ID                  string `json:"id"`
	Name                string `json:"name"`
	OwnerNonce          string `json:"owner_nonce"`
	ConfigurationSHA256 string `json:"configuration_sha256"`
	NetworkEmpty        bool   `json:"network_empty"`
}
type Event struct {
	Operation Operation   `json:"operation"`
	Key       ResourceKey `json:"key"`
	Proof     Proof       `json:"proof"`
	Reason    string      `json:"reason"`
}

// Journal has its own namespace. Every event is replay-validated; no stored
// success flags can bypass transitions. No timestamps, arbitrary diagnostics or
// current-machine capture enter this private deterministic record.
type Journal struct {
	SchemaVersion int     `json:"schema_version"`
	Kind          string  `json:"kind"`
	Boundary      string  `json:"execution_boundary"`
	Plan          Plan    `json:"plan"`
	GraphSHA256   string  `json:"graph_sha256"`
	Events        []Event `json:"events"`
}
type ResourceState struct {
	Key   ResourceKey
	State string
	Proof Proof
}
type JournalState struct {
	Prepared     bool
	Aborted      bool
	Worker       string
	ProxyCleanup string
	LabCleanup   string
	Resources    [8]ResourceState
}

func NewJournal(p Plan, a Authorization, prior *Journal) (Journal, error) {
	if a.validate(p) != nil {
		return Journal{}, ErrAuthorization
	}
	if prior != nil {
		return Journal{}, ErrReplay
	}
	g, e := GenerateGraph(p)
	if e != nil {
		return Journal{}, e
	}
	h, _ := g.Hash(p)
	j := Journal{1, "faultline-lab-journal", "CONTRACTS_ONLY_NOT_RUNTIME_QUALIFIED", p, h, []Event{}}
	return j, j.Validate()
}
func (j Journal) State() (JournalState, error) {
	s := JournalState{Worker: "not_started", ProxyCleanup: "not_created", LabCleanup: "not_started"}
	if j.SchemaVersion != 1 || j.Kind != "faultline-lab-journal" || j.Boundary != "CONTRACTS_ONLY_NOT_RUNTIME_QUALIFIED" || j.Events == nil || len(j.Events) > MaxJournalEvents {
		return s, ErrJournal
	}
	g, err := GenerateGraph(j.Plan)
	if err != nil {
		return s, ErrJournal
	}
	h, _ := g.Hash(j.Plan)
	if h != j.GraphSHA256 {
		return s, ErrJournal
	}
	for i, k := range resourceKeys {
		s.Resources[i] = ResourceState{Key: k, State: "uncreated"}
	}
	for _, e := range j.Events {
		if applyEvent(&s, g, e) != nil {
			return JournalState{}, ErrJournal
		}
	}
	return s, nil
}
func (j Journal) Validate() error { _, e := j.State(); return e }
func (j Journal) Advance(a Authorization, e Event) (Journal, error) {
	if a.validate(j.Plan) != nil {
		return Journal{}, ErrAuthorization
	}
	if j.Validate() != nil || len(j.Events) >= MaxJournalEvents {
		return Journal{}, ErrJournal
	}
	next := j
	next.Events = append(append([]Event{}, j.Events...), e)
	if err := next.Validate(); err != nil {
		return Journal{}, err
	}
	if err := admitEvent(next, e); err != nil {
		return Journal{}, err
	}
	return next, nil
}
func (j Journal) Encode() ([]byte, error) {
	if j.Validate() != nil {
		return nil, ErrJournal
	}
	b, e := json.MarshalIndent(j, "", "  ")
	if e != nil || len(b)+1 > MaxJournalBytes {
		return nil, ErrJournal
	}
	return append(b, '\n'), nil
}
func (j Journal) Hash() (string, error) {
	if j.Validate() != nil {
		return "", ErrJournal
	}
	b, e := json.Marshal(j)
	if e != nil {
		return "", ErrJournal
	}
	return hash(b), nil
}

// Generated journals accept only their canonical typed spelling, ignoring
// whitespace. This rejects nulls, aliases, unknown/duplicate keys and silently
// truncated fixed arrays without weakening the existing plan wire format.
func DecodeJournal(ctx context.Context, r io.Reader) (Journal, error) {
	b, e := io.ReadAll(io.LimitReader(ctxReader{ctx, r}, MaxJournalBytes+1))
	if e != nil {
		return Journal{}, e
	}
	var j Journal
	if len(b) > MaxJournalBytes || jsonstrict.Validate(b) != nil || json.Unmarshal(b, &j) != nil {
		return Journal{}, ErrJournal
	}
	c, e := json.Marshal(j)
	var compact bytes.Buffer
	if e != nil || json.Compact(&compact, b) != nil || !bytes.Equal(c, compact.Bytes()) || j.Validate() != nil {
		return Journal{}, ErrJournal
	}
	return j, ctx.Err()
}
func validReason(s string) bool {
	switch s {
	case "canceled", "transport_failure", "supervisor_interrupted", "persistence_failure", "create_failure":
		return true
	}
	return false
}
func proofMatches(g Graph, k ResourceKey, p Proof) bool {
	n, h, e := g.resource(k)
	return e == nil && resourceID.MatchString(p.ID) && p.Name == n && p.OwnerNonce == g.OwnerNonce && p.ConfigurationSHA256 == h
}
func applyEvent(s *JournalState, g Graph, e Event) error {
	i := resourceIndex(e.Key)
	resourceOp := e.Operation == CreateIntent || e.Operation == Created || e.Operation == CreateAbsent || e.Operation == CreateUnknown || e.Operation == DeleteIntent || e.Operation == DeleteAbsent || e.Operation == DeleteUnknown
	if resourceOp != (i >= 0) || (!resourceOp && e.Key != "") {
		return ErrJournal
	}
	needsProof := e.Operation == Created || e.Operation == DeleteIntent || e.Operation == DeleteAbsent || e.Operation == DeleteUnknown
	if !needsProof && e.Proof != (Proof{}) {
		return ErrJournal
	}
	needsReason := e.Operation == Abort || e.Operation == CreateAbsent || e.Operation == CreateUnknown || e.Operation == WorkerUnknown || e.Operation == ProxyUnconfirmed || e.Operation == DeleteUnknown || e.Operation == CleanupUnconfirmed
	if needsReason && !validReason(e.Reason) || !needsReason && e.Reason != "" {
		return ErrJournal
	}
	if s.LabCleanup == "complete" {
		return ErrJournal
	}
	if resourceOp {
		r := &s.Resources[i]
		switch e.Operation {
		case CreateIntent:
			if !s.Prepared || s.Aborted || s.LabCleanup != "not_started" || r.State != "uncreated" {
				return ErrJournal
			}
			if i >= 4 {
				for _, m := range g.Containers[i-4].Memberships {
					n := resourceIndex(m.Network)
					if n < 0 || s.Resources[n].State != "observed" {
						return ErrJournal
					}
				}
			}
			r.State = "create_intent"
		case Created:
			if r.State != "create_intent" || !proofMatches(g, e.Key, e.Proof) || e.Proof.NetworkEmpty {
				return ErrJournal
			}
			for n, x := range s.Resources {
				if n != i && x.Proof.ID == e.Proof.ID {
					return ErrJournal
				}
			}
			r.Proof = e.Proof
			r.State = "observed"
		case CreateAbsent, CreateUnknown:
			if r.State != "create_intent" {
				return ErrJournal
			}
			r.State = "absent"
			if e.Operation == CreateUnknown {
				r.State = "creation_unknown"
			}
			s.Aborted = true
		case DeleteIntent:
			if s.LabCleanup != "active" || !cleanupProof(*s, g, e.Key, e.Proof) {
				return ErrJournal
			}
			r.State = "delete_intent"
		case DeleteAbsent, DeleteUnknown:
			if s.LabCleanup != "active" || r.State != "delete_intent" || !proofMatches(g, e.Key, e.Proof) || !sameProof(r.Proof, e.Proof) {
				return ErrJournal
			}
			r.State = "absent"
			if e.Operation == DeleteUnknown {
				r.State = "delete_unknown"
			}
		}
		return nil
	}
	switch e.Operation {
	case Prepare:
		if s.Prepared || s.Aborted || s.LabCleanup != "not_started" {
			return ErrJournal
		}
		s.Prepared = true
	case Abort:
		if s.Aborted || s.LabCleanup != "not_started" {
			return ErrJournal
		}
		s.Aborted = true
	case WorkerIntent:
		if !s.Prepared || s.Aborted || s.LabCleanup != "not_started" || s.Worker != "not_started" {
			return ErrJournal
		}
		for _, r := range s.Resources {
			if r.State != "observed" {
				return ErrJournal
			}
		}
		s.Worker = "intent"
		s.ProxyCleanup = "unconfirmed"
	case WorkerRunning:
		if s.Worker != "intent" || s.Aborted || s.LabCleanup != "not_started" {
			return ErrJournal
		}
		s.Worker = "running"
		s.ProxyCleanup = "unconfirmed"
	case WorkerExited:
		if s.Worker != "intent" && s.Worker != "running" && s.Worker != "unknown" {
			return ErrJournal
		}
		s.Worker = "exited"
	case WorkerUnknown:
		if s.Worker != "intent" && s.Worker != "running" {
			return ErrJournal
		}
		s.Worker = "unknown"
		s.Aborted = true
		s.ProxyCleanup = "unconfirmed"
	case ProxyCleanupIntent:
		if s.ProxyCleanup != "unconfirmed" || s.Resources[7].Proof.ID == "" {
			return ErrJournal
		}
		s.ProxyCleanup = "intent"
	case ProxyAbsent, ProxyUnconfirmed:
		if s.ProxyCleanup != "intent" {
			return ErrJournal
		}
		s.ProxyCleanup = "complete"
		if e.Operation == ProxyUnconfirmed {
			s.ProxyCleanup = "unconfirmed"
		}
	case CleanupBegin:
		if s.LabCleanup != "not_started" && s.LabCleanup != "unconfirmed" {
			return ErrJournal
		}
		s.LabCleanup = "active"
		s.Aborted = true
	case CleanupUnconfirmed:
		if s.LabCleanup != "active" {
			return ErrJournal
		}
		s.LabCleanup = "unconfirmed"
	case CleanupComplete:
		if s.LabCleanup != "active" {
			return ErrJournal
		}
		for _, r := range s.Resources {
			if r.State != "uncreated" && r.State != "absent" {
				return ErrJournal
			}
		}
		s.LabCleanup = "complete"
	default:
		return ErrJournal
	}
	return nil
}
func sameProof(a, b Proof) bool { a.NetworkEmpty = false; b.NetworkEmpty = false; return a == b }
func cleanupProof(s JournalState, g Graph, k ResourceKey, p Proof) bool {
	i := resourceIndex(k)
	if i < 0 || !proofMatches(g, k, p) {
		return false
	}
	r := s.Resources[i]
	if r.State != "observed" && r.State != "delete_intent" && r.State != "delete_unknown" {
		return false
	}
	if !sameProof(r.Proof, p) {
		return false
	}
	if i < 4 {
		if !p.NetworkEmpty {
			return false
		}
		for _, c := range s.Resources[4:] {
			if c.State != "absent" && c.State != "uncreated" {
				return false
			}
		}
	} else if p.NetworkEmpty {
		return false
	}
	return true
}

// CleanupEligible never discovers/adopts by name, prefix or labels. A future
// driver must inspect this exact recorded ID again before each destructive call.
func (j Journal) CleanupEligible(a Authorization, k ResourceKey, p Proof) bool {
	if a.validate(j.Plan) != nil {
		return false
	}
	s, e := j.State()
	if e != nil || s.LabCleanup != "active" {
		return false
	}
	g, _ := GenerateGraph(j.Plan)
	return cleanupProof(s, g, k, p)
}

// Receipt confirms modeled resource absence only; it is not measurement success
// or runtime qualification. Earlier unconfirmed proxy cleanup remains visible.
type CleanupReceipt struct {
	SchemaVersion int    `json:"schema_version"`
	Kind          string `json:"kind"`
	LaunchID      string `json:"launch_id"`
	PlanSHA256    string `json:"plan_sha256"`
	JournalSHA256 string `json:"journal_sha256"`
	LabCleanup    string `json:"lab_cleanup"`
	ProxyCleanup  string `json:"proxy_cleanup"`
	Qualification string `json:"qualification"`
}

func (j Journal) Receipt(a Authorization) (CleanupReceipt, error) {
	if a.validate(j.Plan) != nil {
		return CleanupReceipt{}, ErrAuthorization
	}
	s, e := j.State()
	if e != nil || s.LabCleanup != "complete" {
		return CleanupReceipt{}, ErrJournal
	}
	h, _ := j.Hash()
	return CleanupReceipt{1, "faultline-lab-cleanup-receipt", j.Plan.Identity.LaunchID, j.Plan.AuthorizationSHA256, h, "complete", s.ProxyCleanup, "NOT_PERFORMED"}, nil
}
func journalsEqual(a, b Journal) bool { return reflect.DeepEqual(a, b) }
