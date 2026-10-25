// Package provenance records allowlisted facts, never ambient machine identity.
package provenance

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/raimbekalish/faultline/internal/jsonstrict"
	"io"
	"regexp"
	"runtime"
)

const Methodology = "serial-http-nearest-rank-v1"

type Image struct {
	Component string `json:"component"`
	ImageID   string `json:"image_id"`
	Reference string `json:"reference,omitempty"`
}
type Launch struct {
	SchemaVersion       int     `json:"schema_version"`
	DockerServerVersion string  `json:"docker_server_version"`
	Images              []Image `json:"images"`
}
type Record struct {
	SchemaVersion  int     `json:"schema_version"`
	SpecHash       string  `json:"spec_hash"`
	Revision       string  `json:"faultline_revision"`
	GoVersion      string  `json:"go_version"`
	RunnerOS       string  `json:"runner_os"`
	RunnerArch     string  `json:"runner_arch"`
	Methodology    string  `json:"measurement_methodology"`
	TracingEnabled *bool   `json:"tracing_enabled"`
	Launch         *Launch `json:"container_runtime,omitempty"`
}

var hex64 = regexp.MustCompile(`^[a-f0-9]{64}$`)
var revision = regexp.MustCompile(`^(development|integration|test|[a-f0-9]{7,40}(-dirty)?)$`)
var goVersion = regexp.MustCompile(`^go[0-9]+\.[0-9]+(\.[0-9]+)?$`)
var version = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+([+.-][a-zA-Z0-9.-]+)?$`)
var platform = regexp.MustCompile(`^[a-z0-9]{2,16}$`)
var toxRef = regexp.MustCompile(`^ghcr.io/shopify/toxiproxy:[0-9]+\.[0-9]+\.[0-9]+@sha256:[a-f0-9]{64}$`)
var jaegerRef = regexp.MustCompile(`^cr.jaegertracing.io/jaegertracing/jaeger:[0-9]+\.[0-9]+\.[0-9]+@sha256:[a-f0-9]{64}$`)

func Capture(hash, rev, goVer string, tracing bool, launch *Launch) *Record {
	r := &Record{1, hash, rev, goVer, runtime.GOOS, runtime.GOARCH, Methodology, &tracing, nil}
	if launch != nil {
		copy := *launch
		copy.Images = append([]Image(nil), launch.Images...)
		r.Launch = &copy
	}
	return r
}
func (r Record) Validate() error {
	if r.TracingEnabled == nil || r.SchemaVersion != 1 || !hex64.MatchString(r.SpecHash) || !revision.MatchString(r.Revision) || !goVersion.MatchString(r.GoVersion) || !platform.MatchString(r.RunnerOS) || !platform.MatchString(r.RunnerArch) || r.Methodology != Methodology {
		return fmt.Errorf("invalid environment provenance")
	}
	if r.Launch != nil {
		return r.Launch.Validate()
	}
	return nil
}
func (l Launch) Validate() error {
	if l.SchemaVersion != 1 || !version.MatchString(l.DockerServerVersion) || len(l.Images) < 2 || len(l.Images) > 3 {
		return fmt.Errorf("invalid container provenance")
	}
	seen := map[string]bool{}
	for _, im := range l.Images {
		if seen[im.Component] || len(im.ImageID) != 71 || !bytes.HasPrefix([]byte(im.ImageID), []byte("sha256:")) || !hex64.MatchString(im.ImageID[7:]) {
			return fmt.Errorf("invalid image provenance")
		}
		seen[im.Component] = true
		switch im.Component {
		case "sample":
			if im.Reference != "" {
				return fmt.Errorf("local sample tags are not recorded")
			}
		case "toxiproxy":
			if !toxRef.MatchString(im.Reference) {
				return fmt.Errorf("invalid Toxiproxy reference")
			}
		case "jaeger":
			if !jaegerRef.MatchString(im.Reference) {
				return fmt.Errorf("invalid Jaeger reference")
			}
		default:
			return fmt.Errorf("unknown component")
		}
	}
	if !seen["sample"] || !seen["toxiproxy"] {
		return fmt.Errorf("missing component provenance")
	}
	return nil
}
func DecodeLaunch(reader io.Reader) (*Launch, error) {
	b, err := io.ReadAll(io.LimitReader(reader, 8193))
	if err != nil {
		return nil, err
	}
	if len(b) > 8192 {
		return nil, fmt.Errorf("launch metadata exceeds 8 KiB")
	}
	if err := jsonstrict.Validate(b); err != nil {
		return nil, err
	}
	var l Launch
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err = d.Decode(&l); err != nil {
		return nil, err
	}
	if err = d.Decode(new(any)); err != io.EOF {
		return nil, fmt.Errorf("trailing launch metadata")
	}
	if err = l.Validate(); err != nil {
		return nil, err
	}
	return &l, nil
}

// UnmarshalJSON keeps versioned provenance strict even though historical report
// schema 1 permits other optional fields. Absence of a tracing fact is not false.
func (r *Record) UnmarshalJSON(b []byte) error {
	if err := jsonstrict.Validate(b); err != nil {
		return err
	}
	type plain Record
	var p plain
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err := d.Decode(&p); err != nil {
		return err
	}
	result := Record(p)
	if err := result.Validate(); err != nil {
		return err
	}
	*r = result
	return nil
}
