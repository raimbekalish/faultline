package lab

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/raimbekalish/faultline/internal/jsonstrict"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const maxInspectionBytes = 32 << 10

var ErrInspection = errors.New("read-only Docker inspection failed; no build or pull is attempted")
var ErrEndpoint = errors.New("selected Docker endpoint must be a trusted local Unix socket")
var ErrMetadata = errors.New("Docker metadata does not match the closed linux/arm64 catalog")
var contextName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,63}$`)
var versionString = regexp.MustCompile(`^v?[0-9]+\.[0-9]+\.[0-9]+([+.-][a-zA-Z0-9.-]{1,48})?$`)
var daemonID = regexp.MustCompile(`^[a-zA-Z0-9:-]{1,128}$`)

// There is intentionally no generic Docker command API or mutation operation.
type operation int

const (
	contextEndpoint operation = iota + 1
	serverVersion
	composeVersion
	serverIdentity
	applicationImage
	labImage
	proxyImage
	networkActual
	appActual
	runnerActual
	fixtureActual
	proxyActual
)

type readCommand struct {
	op              operation
	endpoint, value string
}
type reader interface {
	read(context.Context, readCommand) ([]byte, error)
}

// Docker inspect formats decoded maps; empty optional fields may be omitted.
// Count/presence checks must handle both absent and explicit null values.
const imageFormat = `{"id":{{json .Id}},"os":{{json .Os}},"arch":{{json .Architecture}},"env_count":{{with index .Config "Env"}}{{len .}}{{else}}0{{end}},"volume_count":{{with index .Config "Volumes"}}{{len .}}{{else}}0{{end}},"healthcheck":{{if ne (index .Config "Healthcheck") nil}}true{{else}}false{{end}},"onbuild_count":{{with index .Config "OnBuild"}}{{len .}}{{else}}0{{end}}`
const appFormat = imageFormat + `}`
const labFormat = imageFormat + `,"contract":{{with index .Config "Labels"}}{{json (index . "com.faultline.lab.contract")}}{{else}}null{{end}},"revision":{{with index .Config "Labels"}}{{json (index . "org.opencontainers.image.revision")}}{{else}}null{{end}}}`
const proxyFormat = imageFormat + `,"pinned_digest":{{$matched := false}}{{range index . "RepoDigests"}}{{if eq . "` + proxyRepoDigest + `"}}{{$matched = true}}{{end}}{{end}}{{$matched}}}`
const runtimeFormat = `{"version":{{json .Server.Version}},"os":{{json .Server.Os}},"arch":{{json .Server.Arch}}}`

func (c readCommand) args() ([]string, error) {
	if c.op == contextEndpoint {
		if c.endpoint != "" || (c.value != "" && !contextName.MatchString(c.value)) {
			return nil, ErrEndpoint
		}
		a := []string{"context", "inspect"}
		if c.value != "" {
			a = append(a, c.value)
		}
		return append(a, "--format", `{{json .Endpoints.docker.Host}}`), nil
	}
	if _, err := unixPath(c.endpoint); err != nil {
		return nil, err
	}
	a := []string{"--host", c.endpoint}
	switch c.op {
	case serverVersion:
		if c.value != "" {
			return nil, ErrInspection
		}
		return append(a, "version", "--format", runtimeFormat), nil
	case composeVersion:
		if c.value != "" {
			return nil, ErrInspection
		}
		return append(a, "compose", "version", "--format", "json"), nil
	case serverIdentity:
		if c.value != "" {
			return nil, ErrInspection
		}
		return append(a, "info", "--format", `{{json .ID}}`), nil
	case applicationImage:
		if !imageID.MatchString(c.value) {
			return nil, ErrInspection
		}
		return append(a, "image", "inspect", "--format", appFormat, c.value), nil
	case labImage:
		if c.value != LabReference {
			return nil, ErrInspection
		}
		return append(a, "image", "inspect", "--format", labFormat, c.value), nil
	case networkActual, appActual, runnerActual, fixtureActual, proxyActual:
		if !resourceID.MatchString(c.value) {
			return nil, ErrObservation
		}
		kind, format := "container", containerProjection
		switch c.op {
		case networkActual:
			kind, format = "network", networkProjection
		case appActual:
			format = strings.ReplaceAll(format, "ENV_CHECK", appEnvCheck)
		case proxyActual:
			format = strings.ReplaceAll(format, "ENV_CHECK", proxyEnvCheck)
		default:
			format = strings.ReplaceAll(format, "ENV_CHECK", noEnvCheck)
		}
		if c.op != networkActual {
			keys := ownLabelKeys
			if c.op == runnerActual || c.op == fixtureActual {
				keys = labLabelKeys
			}
			if c.op == proxyActual {
				keys = proxyLabelKeys
			}
			format = strings.ReplaceAll(format, "LABEL_CHECK", labelKeyProjection(keys))
		}
		return append(a, kind, "inspect", "--format", format, c.value), nil
	case proxyImage:
		if c.value != ProxyReference {
			return nil, ErrInspection
		}
		return append(a, "image", "inspect", "--format", proxyFormat, c.value), nil
	default:
		return nil, ErrInspection
	}
}

type cliReader struct {
	path string
	env  []string
}

func newCLIReader() (*cliReader, error) {
	path, err := exec.LookPath("docker")
	if err != nil {
		return nil, ErrInspection
	}
	path, err = filepath.EvalSymlinks(path)
	if err != nil {
		return nil, ErrInspection
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return nil, ErrInspection
	}
	// Host CLI/configuration is trusted. No Docker target, Compose, credential,
	// proxy or OTel variables reach commands or any container. Faultline does not
	// request/copy credentials; the trusted CLI may read its own configuration.
	var env []string
	for _, key := range []string{"HOME", "PATH", "DOCKER_CONFIG"} {
		if v, ok := os.LookupEnv(key); ok {
			env = append(env, key+"="+v)
		}
	}
	return &cliReader{path, env}, nil
}

type cappedOutput struct {
	b       bytes.Buffer
	limit   int
	discard bool
	cancel  context.CancelFunc
}

func (w *cappedOutput) Write(b []byte) (int, error) {
	if len(b) > w.limit {
		w.cancel()
		return 0, ErrInspection
	}
	w.limit -= len(b)
	if w.discard {
		return len(b), nil
	}
	return w.b.Write(b)
}

func (r *cliReader) read(parent context.Context, c readCommand) ([]byte, error) {
	args, err := c.args()
	if err != nil {
		return nil, err
	}
	if err = parent.Err(); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(parent, 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, r.path, args...)
	cmd.Env = append([]string(nil), r.env...)
	configureProcess(cmd)
	cmd.WaitDelay = 250 * time.Millisecond
	out := &cappedOutput{limit: maxInspectionBytes, cancel: cancel}
	stderr := &cappedOutput{limit: 4096, discard: true, cancel: cancel}
	cmd.Stdout, cmd.Stderr = out, stderr
	err = cmd.Run()
	if e := parent.Err(); e != nil {
		return nil, e
	}
	if err != nil || ctx.Err() != nil {
		return nil, ErrInspection
	}
	return out.b.Bytes(), nil
}

func unixPath(endpoint string) (string, error) {
	if len(endpoint) > 4096 || strings.ContainsAny(endpoint, "\x00\r\n\t") || !strings.HasPrefix(endpoint, "unix:///") {
		return "", ErrEndpoint
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "unix" || u.Host != "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.RawPath != "" || u.Opaque != "" || strings.ContainsAny(u.Path, "\x00\r\n\t") || u.Path == "/" || !filepath.IsAbs(u.Path) || filepath.Clean(u.Path) != u.Path {
		return "", ErrEndpoint
	}
	return u.Path, nil
}

type socketIdentity struct {
	Endpoint      string
	Device, Inode uint64
}

func inspectSocket(endpoint string) (socketIdentity, error) {
	path, err := unixPath(endpoint)
	if err != nil {
		return socketIdentity{}, err
	}
	path, err = filepath.EvalSymlinks(path)
	if err != nil {
		return socketIdentity{}, ErrEndpoint
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode()&os.ModeSocket == 0 {
		return socketIdentity{}, ErrEndpoint
	}
	device, inode, err := socketNumbers(info)
	if err != nil {
		return socketIdentity{}, err
	}
	endpoint = (&url.URL{Scheme: "unix", Path: path}).String()
	if _, err = unixPath(endpoint); err != nil {
		return socketIdentity{}, err
	}
	return socketIdentity{endpoint, device, inode}, nil
}

type selection struct{ Context, Host string }
type inspector struct {
	r        reader
	selected selection
	socket   func(string) (socketIdentity, error)
}

func (i inspector) resolve(ctx context.Context) (socketIdentity, error) {
	endpoint := i.selected.Host
	if i.selected.Context != "" || endpoint == "" {
		command := readCommand{op: contextEndpoint, value: i.selected.Context}
		if _, err := command.args(); err != nil {
			return socketIdentity{}, err
		}
		b, err := i.r.read(ctx, command)
		if err != nil {
			return socketIdentity{}, err
		}
		if len(b) > maxInspectionBytes || jsonstrict.Validate(b) != nil || json.Unmarshal(b, &endpoint) != nil {
			return socketIdentity{}, ErrEndpoint
		}
	}
	if _, err := unixPath(endpoint); err != nil {
		return socketIdentity{}, err
	}
	return i.socket(endpoint)
}

type Runtime struct {
	DockerVersion  string `json:"docker_version"`
	ComposeVersion string `json:"compose_version"`
	OS             string `json:"os"`
	Arch           string `json:"arch"`
}
type imageMetadata struct {
	ID           string `json:"id"`
	OS           string `json:"os"`
	Arch         string `json:"arch"`
	EnvCount     int    `json:"env_count"`
	VolumeCount  int    `json:"volume_count"`
	Healthcheck  bool   `json:"healthcheck"`
	OnBuildCount int    `json:"onbuild_count"`
	Contract     string `json:"contract"`
	Revision     string `json:"revision"`
	PinnedDigest bool   `json:"pinned_digest"`
}
type inspected struct {
	Runtime                 Runtime
	Socket                  socketIdentity
	DaemonID                string
	Application, Lab, Proxy imageMetadata
}

func (s inspected) validate(d Descriptor) error {
	if d.Validate() != nil || !versionString.MatchString(s.Runtime.DockerVersion) || !versionString.MatchString(s.Runtime.ComposeVersion) || s.Runtime.OS != "linux" || s.Runtime.Arch != "arm64" || !daemonID.MatchString(s.DaemonID) || s.Application.ID != d.ImageID {
		return ErrMetadata
	}
	if _, err := unixPath(s.Socket.Endpoint); err != nil {
		return ErrMetadata
	}
	for _, m := range []imageMetadata{s.Application, s.Lab, s.Proxy} {
		if !imageID.MatchString(m.ID) || m.OS != "linux" || m.Arch != "arm64" || m.VolumeCount != 0 || m.Healthcheck || m.OnBuildCount != 0 {
			return ErrMetadata
		}
	}
	if s.Application.EnvCount != 0 || s.Lab.EnvCount != 0 || s.Proxy.EnvCount != 1 || s.Lab.Contract != "http-work-v1" || !revisionID.MatchString(s.Lab.Revision) || !s.Proxy.PinnedDigest || s.Application.ID == s.Lab.ID || s.Application.ID == s.Proxy.ID || s.Lab.ID == s.Proxy.ID {
		return ErrMetadata
	}
	return nil
}

func metadata(b []byte, extra ...string) (imageMetadata, error) {
	var m imageMetadata
	fields := append([]string{"id", "os", "arch", "env_count", "volume_count", "healthcheck", "onbuild_count"}, extra...)
	if len(b) > maxInspectionBytes || exactObject(b, fields...) != nil || json.Unmarshal(b, &m) != nil {
		return m, ErrMetadata
	}
	if !imageID.MatchString(m.ID) || m.OS != "linux" || m.Arch != "arm64" || m.VolumeCount != 0 || m.Healthcheck || m.OnBuildCount != 0 || m.EnvCount < 0 || m.EnvCount > 1 {
		return m, ErrMetadata
	}
	return m, nil
}

func (i inspector) inspect(ctx context.Context, d Descriptor) (inspected, error) {
	var s inspected
	if err := d.Validate(); err != nil {
		return s, err
	}
	sock, err := i.resolve(ctx)
	if err != nil {
		return s, err
	}
	s.Socket = sock
	read := func(op operation, value string) ([]byte, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		b, err := i.r.read(ctx, readCommand{op, sock.Endpoint, value})
		if err != nil {
			return nil, err
		}
		if len(b) > maxInspectionBytes {
			return nil, ErrMetadata
		}
		return b, nil
	}
	b, err := read(serverVersion, "")
	if err != nil {
		return s, err
	}
	var v struct {
		Version string `json:"version"`
		OS      string `json:"os"`
		Arch    string `json:"arch"`
	}
	if exactObject(b, "version", "os", "arch") != nil || json.Unmarshal(b, &v) != nil || !versionString.MatchString(v.Version) || v.OS != "linux" || v.Arch != "arm64" {
		return s, ErrMetadata
	}
	s.Runtime = Runtime{DockerVersion: v.Version, OS: v.OS, Arch: v.Arch}
	b, err = read(composeVersion, "")
	if err != nil {
		return s, err
	}
	var cv struct {
		Version string `json:"version"`
	}
	if exactObject(b, "version") != nil || json.Unmarshal(b, &cv) != nil || !versionString.MatchString(cv.Version) {
		return s, ErrMetadata
	}
	s.Runtime.ComposeVersion = cv.Version
	b, err = read(serverIdentity, "")
	if err != nil {
		return s, err
	}
	if jsonstrict.Validate(b) != nil || json.Unmarshal(b, &s.DaemonID) != nil || !daemonID.MatchString(s.DaemonID) {
		return s, ErrMetadata
	}
	b, err = read(applicationImage, d.ImageID)
	if err != nil {
		return s, err
	}
	s.Application, err = metadata(b)
	if err != nil || s.Application.ID != d.ImageID || s.Application.EnvCount != 0 {
		return s, ErrMetadata
	}
	b, err = read(labImage, LabReference)
	if err != nil {
		return s, err
	}
	s.Lab, err = metadata(b, "contract", "revision")
	if err != nil || s.Lab.EnvCount != 0 || s.Lab.Contract != "http-work-v1" || !revisionID.MatchString(s.Lab.Revision) {
		return s, ErrMetadata
	}
	b, err = read(proxyImage, ProxyReference)
	if err != nil {
		return s, err
	}
	s.Proxy, err = metadata(b, "pinned_digest")
	if err != nil || s.Proxy.EnvCount != 1 || !s.Proxy.PinnedDigest {
		return s, ErrMetadata
	}
	if s.Application.ID == s.Lab.ID || s.Application.ID == s.Proxy.ID || s.Lab.ID == s.Proxy.ID {
		return s, ErrMetadata
	}
	b, err = read(serverIdentity, "")
	if err != nil {
		return s, err
	}
	var again string
	if jsonstrict.Validate(b) != nil || json.Unmarshal(b, &again) != nil || again != s.DaemonID {
		return s, ErrMetadata
	}
	current, err := i.socket(sock.Endpoint)
	if err != nil || current != sock {
		return s, ErrEndpoint
	}
	if err := s.validate(d); err != nil {
		return s, err
	}
	return s, ctx.Err()
}

// Keep interface assertions local: no generic exec method is exported.
var _ io.Writer = (*cappedOutput)(nil)
