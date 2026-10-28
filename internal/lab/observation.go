package lab

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/raimbekalish/faultline/internal/jsonstrict"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"
)

var ErrObservation = errors.New("recorded resource or runtime inspection mismatch; no ownership inferred")

// These templates project only intentional ownership/configuration fields.
// No IP addresses, arbitrary environment/label values, credentials or logs.
const ownerProjection = `"label_count":{{len .Config.Labels}},"launch":{{json (index .Config.Labels "com.faultline.launch")}},"role":{{json (index .Config.Labels "com.faultline.role")}},"nonce":{{json (index .Config.Labels "com.faultline.owner")}}`
const containerProjection = `{"id":{{json .Id}},"name":{{json .Name}},"image":{{json .Image}},"configured_image":{{json .Config.Image}},` + ownerProjection + `,"path":{{json .Path}},"args":{{if .Args}}{{json .Args}}{{else}}[]{{end}},"entrypoint":{{json .Config.Entrypoint}},"command":{{if .Config.Cmd}}{{json .Config.Cmd}}{{else}}[]{{end}},"user":{{json .Config.User}},"workdir":{{json .Config.WorkingDir}},"stop_signal":{{json .Config.StopSignal}},"env_count":{{len .Config.Env}},"environment_ok":ENV_CHECK,"healthcheck":{{if ne (index .Config "Healthcheck") nil}}true{{else}}false{{end}},"memory":{{.HostConfig.Memory}},"swap":{{.HostConfig.MemorySwap}},"nano_cpus":{{.HostConfig.NanoCpus}},"pids":{{json .HostConfig.PidsLimit}},"nofile_ok":{{if eq (len .HostConfig.Ulimits) 1}}{{$n := index .HostConfig.Ulimits 0}}{{and (eq $n.Name "nofile") (eq $n.Soft 256) (eq $n.Hard 256)}}{{else}}false{{end}},"security_ok":{{and (not .HostConfig.Privileged) .HostConfig.ReadonlyRootfs (eq (len .HostConfig.CapAdd) 0) (eq (len .HostConfig.CapDrop) 1) (eq (index .HostConfig.CapDrop 0) "ALL") (eq (len .HostConfig.SecurityOpt) 1) (eq (index .HostConfig.SecurityOpt 0) "no-new-privileges") (eq .HostConfig.PidMode "") (eq .HostConfig.IpcMode "private") (eq .HostConfig.UTSMode "") (eq .HostConfig.UsernsMode "") (eq .HostConfig.CgroupnsMode "private")}},"extras_ok":{{and (not .HostConfig.PublishAllPorts) (eq (len .HostConfig.PortBindings) 0) (eq (len .HostConfig.Binds) 0) (eq (len .HostConfig.VolumesFrom) 0) (eq (len .HostConfig.Devices) 0) (eq (len .HostConfig.DeviceRequests) 0) (eq (len .HostConfig.ExtraHosts) 0) (eq (len .HostConfig.Links) 0) (eq (len .HostConfig.Sysctls) 0) (eq (len .HostConfig.DnsOptions) 0) (eq .HostConfig.RestartPolicy.Name "no") (eq .HostConfig.RestartPolicy.MaximumRetryCount 0) (eq .HostConfig.LogConfig.Type "none") (eq (len .HostConfig.LogConfig.Config) 0)}},"ports_clear":{{$ok := true}}{{range .NetworkSettings.Ports}}{{if .}}{{$ok = false}}{{end}}{{end}}{{$ok}},"dns":{{json .HostConfig.Dns}},"dns_search":{{json .HostConfig.DnsSearch}},"network_mode":{{json .HostConfig.NetworkMode}},"tmpfs":{{if .HostConfig.Tmpfs}}{{json .HostConfig.Tmpfs}}{{else}}{}{{end}},"mounts":[{{$sep := ""}}{{range .Mounts}}{{$sep}}{"type":{{json .Type}},"destination":{{json .Destination}},"rw":{{json .RW}}}{{$sep = ","}}{{end}}],"networks":[{{$sep := ""}}{{range $name,$n := .NetworkSettings.Networks}}{{$sep}}{"name":{{json $name}},"id":{{json $n.NetworkID}},"aliases":{{json $n.Aliases}},"links":{{len $n.Links}},"ipam_configured":{{if $n.IPAMConfig}}true{{else}}false{{end}},"driver_options":{{len $n.DriverOpts}},"gateway_priority":{{$n.GwPriority}}}{{$sep = ","}}{{end}}]}`
const appEnvCheck = `{{$d := false}}{{$t := false}}{{range .Config.Env}}{{if eq . "FAULTLINE_DEPENDENCY_URL=http://toxiproxy:8666/value"}}{{$d = true}}{{end}}{{if eq . "FAULTLINE_DEPENDENCY_TIMEOUT_MS=250"}}{{$t = true}}{{end}}{{end}}{{and $d $t}}`
const proxyEnvCheck = `{{if eq (len .Config.Env) 1}}{{eq (index .Config.Env 0) "PATH="}}{{else}}false{{end}}`
const noEnvCheck = `{{eq (len .Config.Env) 0}}`
const networkProjection = `{"id":{{json .Id}},"name":{{json .Name}},"label_count":{{len .Labels}},"launch":{{json (index .Labels "com.faultline.launch")}},"role":{{json (index .Labels "com.faultline.role")}},"nonce":{{json (index .Labels "com.faultline.owner")}},"driver":{{json .Driver}},"scope":{{json .Scope}},"internal":{{.Internal}},"ipv4":{{.EnableIPv4}},"attachable":{{.Attachable}},"ingress":{{.Ingress}},"config_only":{{.ConfigOnly}},"config_from":{{with .ConfigFrom}}{{if .Network}}true{{else}}false{{end}}{{else}}false{{end}},"ipv6":{{.EnableIPv6}},"options":{{json .Options}},"ipam_ok":{{and (eq .IPAM.Driver "default") (eq (len .IPAM.Options) 0) (eq (len .IPAM.Config) 1)}},"aux_clear":{{$ok := true}}{{range .IPAM.Config}}{{if index . "AuxiliaryAddresses"}}{{$ok = false}}{{end}}{{end}}{{$ok}},"containers":[{{$sep := ""}}{{range $id,$c := .Containers}}{{$sep}}{{json $id}}{{$sep = ","}}{{end}}]}`

type endpointObservation struct {
	Name            string   `json:"name"`
	ID              string   `json:"id"`
	Aliases         []string `json:"aliases"`
	Links           int      `json:"links"`
	IPAMConfigured  bool     `json:"ipam_configured"`
	GatewayPriority int      `json:"gateway_priority"`
	DriverOptions   int      `json:"driver_options"`
}
type mountObservation struct {
	Type        string `json:"type"`
	Destination string `json:"destination"`
	RW          bool   `json:"rw"`
}
type containerObservation struct {
	Path string   `json:"path"`
	Args []string `json:"args"`

	ID              string                `json:"id"`
	Name            string                `json:"name"`
	Image           string                `json:"image"`
	ConfiguredImage string                `json:"configured_image"`
	LabelCount      int                   `json:"label_count"`
	Launch          string                `json:"launch"`
	Role            string                `json:"role"`
	Nonce           string                `json:"nonce"`
	Entrypoint      []string              `json:"entrypoint"`
	Command         []string              `json:"command"`
	User            string                `json:"user"`
	Workdir         string                `json:"workdir"`
	StopSignal      string                `json:"stop_signal"`
	EnvCount        int                   `json:"env_count"`
	EnvironmentOK   bool                  `json:"environment_ok"`
	Healthcheck     bool                  `json:"healthcheck"`
	Memory          int64                 `json:"memory"`
	Swap            int64                 `json:"swap"`
	NanoCPUs        int64                 `json:"nano_cpus"`
	PIDs            int                   `json:"pids"`
	NoFileOK        bool                  `json:"nofile_ok"`
	SecurityOK      bool                  `json:"security_ok"`
	ExtrasOK        bool                  `json:"extras_ok"`
	PortsClear      bool                  `json:"ports_clear"`
	DNS             []string              `json:"dns"`
	DNSSearch       []string              `json:"dns_search"`
	NetworkMode     string                `json:"network_mode"`
	Tmpfs           map[string]string     `json:"tmpfs"`
	Mounts          []mountObservation    `json:"mounts"`
	Networks        []endpointObservation `json:"networks"`
}
type networkObservation struct {
	IPv4       bool `json:"ipv4"`
	Attachable bool `json:"attachable"`
	Ingress    bool `json:"ingress"`
	ConfigOnly bool `json:"config_only"`
	ConfigFrom bool `json:"config_from"`

	ID         string            `json:"id"`
	Name       string            `json:"name"`
	LabelCount int               `json:"label_count"`
	Launch     string            `json:"launch"`
	Role       string            `json:"role"`
	Nonce      string            `json:"nonce"`
	Driver     string            `json:"driver"`
	Scope      string            `json:"scope"`
	Internal   bool              `json:"internal"`
	IPv6       bool              `json:"ipv6"`
	Options    map[string]string `json:"options"`
	IPAMOK     bool              `json:"ipam_ok"`
	AuxClear   bool              `json:"aux_clear"`
	Containers []string          `json:"containers"`
}

type runtimeWitness struct {
	key           string
	authorization Authorization
}

// Runtime witness is fresh inspection, not a serialized/caller-provided binding.
func inspectRuntime(ctx context.Context, p Plan) (runtimeWitness, error) {
	if e := ctx.Err(); e != nil {
		return runtimeWitness{}, e
	}
	r, e := newCLIReader()
	if e != nil {
		return runtimeWitness{}, e
	}
	i := inspector{r, selectionFromEnvironment(), inspectSocket}
	return i.runtimeWitness(ctx, p)
}
func (i inspector) runtimeWitness(ctx context.Context, p Plan) (result runtimeWitness, err error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	defer func() {
		if e := ctx.Err(); e != nil {
			result = runtimeWitness{}
			err = e
		}
	}()
	if p.Validate() != nil {
		return runtimeWitness{}, ErrPlan
	}
	socket, e := i.resolve(ctx)
	if e != nil {
		return runtimeWitness{}, e
	}
	if socket.Endpoint != p.PrivateBinding.Endpoint || socket.Device != p.PrivateBinding.SocketDevice || socket.Inode != p.PrivateBinding.SocketInode {
		return runtimeWitness{}, ErrAuthorization
	}
	b, e := i.r.read(ctx, readCommand{serverIdentity, socket.Endpoint, ""})
	if e != nil {
		return runtimeWitness{}, e
	}
	var id string
	if len(b) > maxInspectionBytes || jsonstrictString(b, &id) != nil || !daemonID.MatchString(id) {
		return runtimeWitness{}, ErrObservation
	}
	salt, e := hexSalt(p.PrivateBinding.Salt)
	if e != nil || hash(append(append([]byte{}, salt...), []byte("\x00"+id)...)) != p.PrivateBinding.DaemonFingerprint {
		return runtimeWitness{}, ErrAuthorization
	}
	b, e = i.r.read(ctx, readCommand{serverVersion, socket.Endpoint, ""})
	if e != nil {
		return runtimeWitness{}, e
	}
	var v struct {
		Version string `json:"version"`
		OS      string `json:"os"`
		Arch    string `json:"arch"`
	}
	if len(b) > maxInspectionBytes || exactObject(b, "version", "os", "arch") != nil || json.Unmarshal(b, &v) != nil || v.Version != p.Runtime.DockerVersion || v.OS != p.Runtime.OS || v.Arch != p.Runtime.Arch {
		return runtimeWitness{}, ErrAuthorization
	}
	major, err := strconv.Atoi(strings.Split(strings.TrimPrefix(v.Version, "v"), ".")[0])
	if err != nil || major < 28 {
		return runtimeWitness{}, ErrAuthorization
	}
	b, e = i.r.read(ctx, readCommand{serverIdentity, socket.Endpoint, ""})
	var again string
	if e != nil || len(b) > maxInspectionBytes || jsonstrictString(b, &again) != nil || again != id {
		return runtimeWitness{}, ErrAuthorization
	}
	last, e := i.socket(socket.Endpoint)
	if e != nil || last != socket {
		return runtimeWitness{}, ErrAuthorization
	}
	return runtimeWitness{hash([]byte("faultline-runtime-v1\x00" + id)), authorizationFor(p)}, ctx.Err()
}
func jsonstrictString(b []byte, v *string) error {
	if jsonstrict.Validate(b) != nil || json.Unmarshal(b, v) != nil {
		return ErrObservation
	}
	return nil
}
func hexSalt(s string) ([]byte, error) { return hex.DecodeString(s) }
func selectionFromEnvironment() selection {
	return selection{os.Getenv("DOCKER_CONTEXT"), os.Getenv("DOCKER_HOST")}
}

// VerifiedResource cannot be constructed with useful fields outside this package.
// It is a bounded inspection result, NOT a network isolation qualification.
type VerifiedResource struct {
	proof      Proof
	key        ResourceKey
	journalSHA string
	runtime    runtimeWitness
}

func observeRecorded(ctx context.Context, i inspector, j Journal, key ResourceKey) (result VerifiedResource, err error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	defer func() {
		if e := ctx.Err(); e != nil {
			result = VerifiedResource{}
			err = e
		}
	}()
	var out VerifiedResource
	s, e := j.State()
	n := resourceIndex(key)
	if e != nil || n < 0 || s.Resources[n].Proof.ID == "" {
		return out, ErrObservation
	}
	before, e := i.runtimeWitness(ctx, j.Plan)
	if e != nil {
		return out, e
	}
	id := s.Resources[n].Proof.ID
	op := networkActual
	if n >= 4 {
		op = []operation{appActual, runnerActual, fixtureActual, proxyActual}[n-4]
	}
	cmd := readCommand{op, j.Plan.PrivateBinding.Endpoint, id}
	raw, e := i.r.read(ctx, cmd)
	if e != nil {
		return out, e
	}
	if len(raw) > maxInspectionBytes {
		return out, ErrObservation
	}
	proof, e := verifyResource(raw, j, key)
	if e != nil {
		return out, e
	}
	// Repeat exact target and binding: reject changes; this still is not an atomic
	// daemon transaction and cannot eliminate later TOCTOU or hidden lifecycle changes.
	again, e := i.r.read(ctx, cmd)
	if e != nil || !bytes.Equal(raw, again) {
		return out, ErrObservation
	}
	after, e := i.runtimeWitness(ctx, j.Plan)
	if e != nil || before != after {
		return out, ErrObservation
	}
	h, _ := j.Hash()
	return VerifiedResource{proof, key, h, after}, ctx.Err()
}
func verifyResource(raw []byte, j Journal, key ResourceKey) (Proof, error) {
	bad := func() (Proof, error) { return Proof{}, ErrObservation }
	s, e := j.State()
	idx := resourceIndex(key)
	if e != nil || idx < 0 {
		return bad()
	}
	g, _ := GenerateGraph(j.Plan)
	expected := s.Resources[idx].Proof
	if expected.ID == "" {
		return bad()
	}
	if idx < 4 {
		var actual networkObservation
		if strictProjection(raw, &actual) != nil {
			return bad()
		}
		n := g.Networks[idx]
		if actual.ID != expected.ID || actual.Name != n.Name || actual.LabelCount != 3 || actual.Launch != g.LaunchID || actual.Role != string(key) || actual.Nonce != g.OwnerNonce || actual.Driver != "bridge" || actual.Scope != "local" || !actual.Internal || actual.IPv6 || !actual.IPv4 || actual.Attachable || actual.Ingress || actual.ConfigOnly || actual.ConfigFrom || !actual.IPAMOK || !actual.AuxClear || !reflect.DeepEqual(actual.Options, map[string]string{"com.docker.network.bridge.gateway_mode_ipv4": "isolated"}) || actual.Containers == nil || len(actual.Containers) > 4 {
			return bad()
		}
		seen := map[string]bool{}
		for _, id := range actual.Containers {
			if !resourceID.MatchString(id) || seen[id] {
				return bad()
			}
			seen[id] = true
			found := false
			for c := 4; c < 8; c++ {
				if s.Resources[c].Proof.ID == id {
					for _, m := range g.Containers[c-4].Memberships {
						if m.Network == key {
							found = true
						}
					}
				}
			}
			if !found {
				return bad()
			}
		}
		// Recompute the profile digest from verified selected values, not any label.
		normalized := g
		normalized.Networks[idx].Name = actual.Name
		normalized.Networks[idx].Driver = actual.Driver
		normalized.Networks[idx].Profile.Internal = actual.Internal
		normalized.Networks[idx].Profile.IPv6 = actual.IPv6
		normalized.Networks[idx].Profile.GatewayMode = actual.Options["com.docker.network.bridge.gateway_mode_ipv4"]
		_, h, _ := normalized.resource(key)
		p := Proof{actual.ID, actual.Name, actual.Nonce, h, len(actual.Containers) == 0}
		if !sameProof(p, expected) {
			return bad()
		}
		return p, nil
	}
	var actual containerObservation
	if strictProjection(raw, &actual) != nil {
		return bad()
	}
	c := g.Containers[idx-4]
	limits := c.Role.Limits
	if actual.ID != expected.ID || actual.Name != "/"+c.Name || actual.Image != c.ImageID || actual.ConfiguredImage != c.ImageID || actual.LabelCount != 3 || actual.Launch != g.LaunchID || actual.Role != string(key) || actual.Nonce != g.OwnerNonce || !reflect.DeepEqual(actual.Entrypoint, c.Role.Entrypoint) || !reflect.DeepEqual(actual.Command, c.Role.Command) || actual.User != "65532:65532" || actual.Workdir != "/" || actual.StopSignal != "SIGTERM" || !actual.EnvironmentOK || actual.Healthcheck || actual.Path != c.Role.Entrypoint[0] || !reflect.DeepEqual(actual.Args, c.Role.Command) || actual.Memory != int64(limits.MemoryMiB)<<20 || actual.Swap != actual.Memory || actual.NanoCPUs != int64(limits.CPUMillicores)*1000000 || actual.PIDs != limits.PIDs || !actual.NoFileOK || !actual.SecurityOK || !actual.ExtrasOK || !actual.PortsClear || !reflect.DeepEqual(actual.DNS, []string{"127.0.0.1"}) || !reflect.DeepEqual(actual.DNSSearch, []string{"."}) || len(actual.Networks) != len(c.Memberships) || actual.Mounts == nil || len(actual.Mounts) > 1 {
		return bad()
	}
	envCount := 0
	if key == Application {
		envCount = 2
	}
	if key == Proxy {
		envCount = 1
	}
	if actual.EnvCount != envCount {
		return bad()
	}
	tmpfs := map[string]string{}
	if limits.TmpfsMiB != 0 {
		tmpfs["/tmp"] = "rw,nosuid,nodev,noexec,size=33554432,mode=700"
	}
	if !reflect.DeepEqual(actual.Tmpfs, tmpfs) {
		return bad()
	}
	for _, m := range actual.Mounts {
		if key != Application || m.Type != "tmpfs" || m.Destination != "/tmp" || !m.RW {
			return bad()
		}
	}
	firstName := g.LaunchID + "-" + strings.TrimPrefix(string(c.Memberships[0].Network), "network.")
	if actual.NetworkMode != firstName {
		return bad()
	}
	seen := map[string]bool{}
	normalized := g
	normalized.Containers[idx-4].Memberships = []Membership{}
	for _, m := range c.Memberships {
		name := g.LaunchID + "-" + strings.TrimPrefix(string(m.Network), "network.")
		found := false
		for _, n := range actual.Networks {
			if n.Name != name {
				continue
			}
			if found || seen[n.Name] || n.ID != s.Resources[resourceIndex(m.Network)].Proof.ID || n.Links != 0 || n.IPAMConfigured || n.GatewayPriority != 0 || n.DriverOptions != 0 || len(n.Aliases) < 1 || len(n.Aliases) > 3 {
				return bad()
			}
			found = true
			seen[n.Name] = true
			aliases := map[string]bool{}
			for _, v := range n.Aliases {
				if aliases[v] || (v != m.Alias && v != c.Name && v != actual.ID[:12]) {
					return bad()
				}
				aliases[v] = true
			}
			if !aliases[m.Alias] {
				return bad()
			}
			normalized.Containers[idx-4].Memberships = append(normalized.Containers[idx-4].Memberships, Membership{m.Network, m.Alias})
		}
		if !found {
			return bad()
		}
	}
	normalized.Containers[idx-4].Name = strings.TrimPrefix(actual.Name, "/")
	normalized.Containers[idx-4].ImageID = actual.Image
	normalized.Containers[idx-4].Role.Entrypoint = actual.Entrypoint
	normalized.Containers[idx-4].Role.Command = actual.Command
	normalized.Containers[idx-4].Role.Limits.MemoryMiB = int(actual.Memory >> 20)
	normalized.Containers[idx-4].Role.Limits.CPUMillicores = int(actual.NanoCPUs / 1000000)
	normalized.Containers[idx-4].Role.Limits.PIDs = actual.PIDs
	_, h, _ := normalized.resource(key)
	p := Proof{actual.ID, strings.TrimPrefix(actual.Name, "/"), actual.Nonce, h, false}
	if !sameProof(p, expected) {
		return bad()
	}
	return p, nil
}

// Object key order is irrelevant, but exact spelling/types/required keys and
// normalized typed JSON must match; null/ignored fields never become defaults.
func strictProjection(b []byte, v any) error {
	if len(b) > maxInspectionBytes || jsonstrict.Validate(b) != nil || json.Unmarshal(b, v) != nil {
		return ErrObservation
	}
	canonical, e := json.Marshal(v)
	var left, right any
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	if e != nil || d.Decode(&left) != nil {
		return ErrObservation
	}
	d = json.NewDecoder(bytes.NewReader(canonical))
	d.UseNumber()
	if d.Decode(&right) != nil || !reflect.DeepEqual(left, right) {
		return ErrObservation
	}
	return nil
}
