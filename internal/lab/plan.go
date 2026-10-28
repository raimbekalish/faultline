package lab

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/raimbekalish/faultline/internal/jsonstrict"
	"io"
	"reflect"
	"regexp"
)

var ErrPlan = errors.New("invalid or oversized planning-only plan")
var launchID = regexp.MustCompile(`^faultline-lab-[a-f0-9]{32}$`)
var runID = regexp.MustCompile(`^faultline-[a-f0-9]{32}$`)

type Identity struct {
	LaunchID           string `json:"launch_id"`
	RunID              string `json:"run_id"`
	AuthorizationNonce string `json:"authorization_nonce"`
}
type privateBinding struct {
	Endpoint          string `json:"endpoint"`
	SocketDevice      uint64 `json:"socket_device"`
	SocketInode       uint64 `json:"socket_inode"`
	Salt              string `json:"salt"`
	DaemonFingerprint string `json:"daemon_fingerprint"`
}
type Component struct {
	Role             string `json:"role"`
	ImageID          string `json:"image_id"`
	Reference        string `json:"reference,omitempty"`
	DeclaredRevision string `json:"declared_revision,omitempty"`
}
type payload struct {
	SchemaVersion            int            `json:"schema_version"`
	Kind                     string         `json:"kind"`
	Status                   string         `json:"status"`
	RuntimeQualification     string         `json:"runtime_qualification"`
	ApplicationQualification string         `json:"application_qualification"`
	ExecutionAvailable       bool           `json:"execution_available"`
	Descriptor               Descriptor     `json:"descriptor"`
	ConfigurationSHA256      string         `json:"configuration_sha256"`
	Catalog                  Definition     `json:"catalog"`
	CatalogSHA256            string         `json:"catalog_sha256"`
	Components               [4]Component   `json:"components"`
	Runtime                  Runtime        `json:"runtime"`
	Identity                 Identity       `json:"identity"`
	PrivateBinding           privateBinding `json:"private_binding"`
	RunAuthorization         string         `json:"run_authorization"`
	CleanupAuthorization     string         `json:"cleanup_authorization"`
}
type Plan struct {
	payload
	AuthorizationSHA256 string `json:"authorization_sha256"`
}

// Plans are generated canonical records, unlike human-authored descriptors.
// Reject unknown/case-aliased fields, ignored array elements and alternate wire
// spellings rather than silently dropping material covered by authorization.
// This codec grants no RUN/CLEANUP capability and performs no I/O.
func (p *Plan) UnmarshalJSON(b []byte) error {
	if len(b) > MaxPlanBytes || jsonstrict.Validate(b) != nil {
		return ErrPlan
	}
	type wire Plan
	var decoded wire
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if d.Decode(&decoded) != nil {
		return ErrPlan
	}
	canonical, err := json.Marshal(decoded)
	if err != nil {
		return ErrPlan
	}
	var compact bytes.Buffer
	if json.Compact(&compact, b) != nil || !bytes.Equal(compact.Bytes(), canonical) {
		return ErrPlan
	}
	result := Plan(decoded)
	if err := result.Validate(); err != nil {
		return err
	}
	*p = result
	return nil
}

type Summary struct {
	SchemaVersion            int          `json:"schema_version"`
	Kind                     string       `json:"kind"`
	Status                   string       `json:"status"`
	RuntimeQualification     string       `json:"runtime_qualification"`
	ApplicationQualification string       `json:"application_qualification"`
	ExecutionAvailable       bool         `json:"execution_available"`
	LaunchID                 string       `json:"launch_id"`
	RunID                    string       `json:"run_id"`
	AuthorizationSHA256      string       `json:"authorization_sha256"`
	ConfigurationSHA256      string       `json:"configuration_sha256"`
	CatalogSHA256            string       `json:"catalog_sha256"`
	Platform                 string       `json:"platform"`
	Components               [4]Component `json:"components"`
	Notes                    []string     `json:"notes"`
}

func buildPlan(ctx context.Context, d Descriptor, s inspected, entropy io.Reader) (Plan, error) {
	var p Plan
	if err := ctx.Err(); err != nil {
		return p, err
	}
	if s.validate(d) != nil {
		return p, ErrPlan
	}
	if _, err := unixPath(s.Socket.Endpoint); err != nil {
		return p, ErrPlan
	}
	var random [96]byte
	if _, err := io.ReadFull(ctxReader{ctx, entropy}, random[:]); err != nil {
		return p, err
	}
	// ReadFull can discard a reader error when it also received all 96 bytes.
	if err := ctx.Err(); err != nil {
		return p, err
	}
	p.payload = payload{
		SchemaVersion: 1, Kind: "faultline-lab-plan", Status: "PLANNED", RuntimeQualification: "NOT_PERFORMED", ApplicationQualification: "NOT_PERFORMED", ExecutionAvailable: false,
		Descriptor: d, Catalog: Catalog(), Runtime: s.Runtime,
		Identity:         Identity{"faultline-lab-" + hex.EncodeToString(random[:16]), "faultline-" + hex.EncodeToString(random[16:32]), hex.EncodeToString(random[32:64])},
		PrivateBinding:   privateBinding{s.Socket.Endpoint, s.Socket.Device, s.Socket.Inode, hex.EncodeToString(random[64:]), hash(append(append([]byte(nil), random[64:]...), []byte("\x00"+s.DaemonID)...))},
		Components:       [4]Component{{"application", s.Application.ID, "", ""}, {"runner", s.Lab.ID, LabReference, s.Lab.Revision}, {"fixture", s.Lab.ID, LabReference, s.Lab.Revision}, {"toxiproxy", s.Proxy.ID, ProxyReference, ""}},
		RunAuthorization: "single_use_requires_future_consumption_record", CleanupAuthorization: "consumed_identity_and_verified_journal_only",
	}
	p.ConfigurationSHA256, _ = d.Hash()
	c, _ := json.Marshal(p.Catalog)
	p.CatalogSHA256 = hash(c)
	b, _ := json.Marshal(p.payload)
	p.AuthorizationSHA256 = hash(b)
	return p, p.Validate()
}

func (p Plan) Validate() error {
	if p.SchemaVersion != 1 || p.Kind != "faultline-lab-plan" || p.Status != "PLANNED" || p.RuntimeQualification != "NOT_PERFORMED" || p.ApplicationQualification != "NOT_PERFORMED" || p.ExecutionAvailable || p.Descriptor.Validate() != nil || !reflect.DeepEqual(p.Catalog, Catalog()) || p.RunAuthorization != "single_use_requires_future_consumption_record" || p.CleanupAuthorization != "consumed_identity_and_verified_journal_only" {
		return ErrPlan
	}
	conf, _ := p.Descriptor.Hash()
	catalog, _ := json.Marshal(p.Catalog)
	if p.ConfigurationSHA256 != conf || p.CatalogSHA256 != hash(catalog) || !launchID.MatchString(p.Identity.LaunchID) || !runID.MatchString(p.Identity.RunID) || !digest.MatchString(p.Identity.AuthorizationNonce) {
		return ErrPlan
	}
	if _, err := unixPath(p.PrivateBinding.Endpoint); err != nil {
		return ErrPlan
	}
	if !digest.MatchString(p.PrivateBinding.Salt) || !digest.MatchString(p.PrivateBinding.DaemonFingerprint) || !versionString.MatchString(p.Runtime.DockerVersion) || !versionString.MatchString(p.Runtime.ComposeVersion) || p.Runtime.OS != "linux" || p.Runtime.Arch != "arm64" {
		return ErrPlan
	}
	a, r, f, t := p.Components[0], p.Components[1], p.Components[2], p.Components[3]
	if a != (Component{"application", p.Descriptor.ImageID, "", ""}) || r.Role != "runner" || !imageID.MatchString(r.ImageID) || r.Reference != LabReference || !revisionID.MatchString(r.DeclaredRevision) || f != (Component{"fixture", r.ImageID, LabReference, r.DeclaredRevision}) || t.Role != "toxiproxy" || !imageID.MatchString(t.ImageID) || t.Reference != ProxyReference || t.DeclaredRevision != "" || a.ImageID == r.ImageID || a.ImageID == t.ImageID || r.ImageID == t.ImageID {
		return ErrPlan
	}
	b, err := json.Marshal(p.payload)
	if err != nil || p.AuthorizationSHA256 != hash(b) {
		return ErrPlan
	}
	return nil
}

func (p Plan) Encode() ([]byte, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return nil, ErrPlan
	}
	b = append(b, '\n')
	if len(b) > MaxPlanBytes {
		return nil, ErrPlan
	}
	return b, nil
}

func (p Plan) summary() Summary {
	return Summary{1, "faultline-lab-plan-summary", p.Status, p.RuntimeQualification, p.ApplicationQualification, false, p.Identity.LaunchID, p.Identity.RunID, p.AuthorizationSHA256, p.ConfigurationSHA256, p.CatalogSHA256, p.Descriptor.Platform, p.Components, []string{
		"Planning inspected metadata only; no container, network or application was created or executed.",
		"Runtime isolation, image behavior and declared worker protocol/build labels are not runtime-qualified or authenticated.",
		"RUN and CLEANUP are not implemented. Future RUN consumes this identity once; cleanup requires its verified consumed journal and resource IDs, never a prefix.",
		"The plan file contains private local runtime binding data; do not publish it. Hashes provide content consistency, not publisher authentication.",
	}}
}

func (s Summary) Write(out io.Writer) error {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return ErrPlan
	}
	if len(b)+1 > MaxSummaryBytes {
		return ErrPlan
	}
	return writeSummary(out, append(b, '\n'))
}
