package lab

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strconv"
	"time"
)

var ErrCommandPlan = errors.New("stale or invalid closed Docker command plan")

const MaxCommandBytes = 16 << 10
const ActionWindow = 2 * time.Second

// CommandPlan has no executable, invocation method or user-settable fields.
// argv includes a PRIVATE Unix endpoint; only a redacted view is serializable.
type CommandPlan struct {
	lease    *TrustedLease
	mutation MutationDescription
	revision string
	runtime  runtimeWitness
	argv     []string
	digest   string
	issued   time.Time
}
type CommandSummary struct {
	SchemaVersion      int          `json:"schema_version"`
	Kind               MutationKind `json:"kind"`
	Key                ResourceKey  `json:"key"`
	ExecutionAvailable bool         `json:"execution_available"`
	ValidityMS         int64        `json:"validity_ms"`
}

func (p CommandPlan) MarshalJSON() ([]byte, error) {
	return json.Marshal(CommandSummary{1, p.mutation.Kind, p.mutation.Key, false, ActionWindow.Milliseconds()})
}
func (*CommandPlan) UnmarshalJSON([]byte) error { return ErrCommandPlan }
func (l *TrustedLease) PrepareCommand(ctx context.Context, m MutationIntent) (CommandPlan, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.prepareCommand(ctx, m, time.Now())
}
func (l *TrustedLease) prepareCommand(ctx context.Context, m MutationIntent, now time.Time) (CommandPlan, error) {
	if m.lease != l || l.closed || l.imported || m.description.ExecutionAvailable {
		return CommandPlan{}, ErrCommandPlan
	}
	j, e := l.operationJournal(ctx, m.description.Kind)
	if e != nil {
		return CommandPlan{}, e
	}
	h, _ := j.Hash()
	if h != m.revision {
		return CommandPlan{}, ErrCommandPlan
	}
	if e = l.checkRuntime(ctx); e != nil {
		return CommandPlan{}, e
	}
	args, e := commandArguments(j, m.description, l.cleanupOnly || l.recordingUnconfirmed)
	if e != nil {
		return CommandPlan{}, e
	}
	b, _ := json.Marshal(args)
	return CommandPlan{l, m.description, h, l.witness, args, hash(b), now}, nil
}
func cleanupKind(k MutationKind) bool {
	return k == RemoveContainer || k == RemoveNetwork || k == StopContainer
}
func commandArguments(j Journal, d MutationDescription, cleanupOnly bool) ([]string, error) {
	s, e := j.State()
	idx := resourceIndex(d.Key)
	if e != nil || idx < 0 || d.ExecutionAvailable {
		return nil, ErrCommandPlan
	}
	g, _ := GenerateGraph(j.Plan)
	if _, e = unixPath(j.Plan.PrivateBinding.Endpoint); e != nil {
		return nil, e
	}
	args := []string{"--host", j.Plan.PrivateBinding.Endpoint}
	switch d.Kind {
	case CreateNetwork:
		if cleanupOnly || idx >= 4 || s.Aborted || s.LabCleanup != "not_started" || s.Resources[idx].State != "create_intent" || d.ID != "" || d.Container != nil || d.Network == nil || !reflect.DeepEqual(*d.Network, g.Networks[idx]) {
			return nil, ErrCommandPlan
		}
		args = append(args, "network", "create", "--driver=bridge", "--internal", "--ipv4=true", "--ipv6=false", "--opt=com.docker.network.bridge.gateway_mode_ipv4=isolated")
		args = append(args, ownershipArgs(g, d.Key)...)
		args = append(args, g.Networks[idx].Name)
	case CreateContainer:
		if cleanupOnly || idx < 4 || s.Aborted || s.LabCleanup != "not_started" || s.Resources[idx].State != "create_intent" || d.ID != "" || d.Network != nil || d.Container == nil || !reflect.DeepEqual(*d.Container, g.Containers[idx-4]) {
			return nil, ErrCommandPlan
		}
		c := g.Containers[idx-4]
		lim := c.Role.Limits
		args = append(args, "container", "create", "--pull=never", "--platform=linux/arm64", "--name="+c.Name, "--hostname="+c.Name, "--user=65532:65532", "--workdir=/", "--read-only", "--cap-drop=ALL", "--security-opt=no-new-privileges", "--ipc=private", "--cgroupns=private", "--pids-limit="+strconv.Itoa(lim.PIDs), "--memory="+strconv.Itoa(lim.MemoryMiB)+"m", "--memory-swap="+strconv.Itoa(lim.MemoryMiB)+"m", "--cpus=0.5", "--ulimit=nofile=256:256", "--restart=no", "--log-driver=none", "--stop-signal=SIGTERM", "--stop-timeout=12", "--dns=127.0.0.1", "--dns-search=.")
		args = append(args, ownershipArgs(g, d.Key)...)
		for _, m := range c.Memberships {
			n := resourceIndex(m.Network)
			if n < 0 || s.Resources[n].State != "observed" || !resourceID.MatchString(s.Resources[n].Proof.ID) {
				return nil, ErrCommandPlan
			}
			args = append(args, "--network=name="+s.Resources[n].Proof.ID+",alias="+m.Alias+",gw-priority=0")
		}
		if d.Key == Application {
			for _, v := range g.Profile.ApplicationEnvironment {
				args = append(args, "--env="+v.Name+"="+v.Value)
			}
			args = append(args, "--tmpfs=/tmp:rw,nosuid,nodev,noexec,size=33554432,mode=700")
		}
		if d.Key == Proxy {
			args = append(args, "--env=PATH=")
		}
		args = append(args, "--entrypoint="+c.Role.Entrypoint[0], c.ImageID)
		args = append(args, c.Role.Command...)
	case StartContainer, StopContainer, RemoveContainer, RemoveNetwork:
		if !resourceID.MatchString(d.ID) || s.Resources[idx].Proof.ID != d.ID || d.Network != nil || d.Container != nil {
			return nil, ErrCommandPlan
		}
		if d.Kind == StartContainer {
			if cleanupOnly || idx < 4 || s.Aborted || s.LabCleanup != "not_started" || s.Resources[idx].State != "observed" {
				return nil, ErrCommandPlan
			}
			args = append(args, "container", "start", d.ID)
		}
		if d.Kind == StopContainer {
			if idx < 4 || s.Resources[idx].State == "absent" {
				return nil, ErrCommandPlan
			}
			args = append(args, "container", "stop", "--time=12", d.ID)
		}
		if d.Kind == RemoveContainer {
			if idx < 4 {
				return nil, ErrCommandPlan
			}
			args = append(args, "container", "rm", d.ID)
		}
		if d.Kind == RemoveNetwork {
			if idx >= 4 {
				return nil, ErrCommandPlan
			}
			for _, r := range s.Resources[4:] {
				if r.State != "uncreated" && r.State != "absent" {
					return nil, ErrCommandPlan
				}
			}
			args = append(args, "network", "rm", d.ID)
		}
	default:
		return nil, ErrCommandPlan
	}
	bytes := 0
	for _, v := range args {
		bytes += len(v) + 1
	}
	if bytes > MaxCommandBytes || len(args) > 80 {
		return nil, ErrCommandPlan
	}
	return args, nil
}
func ownershipArgs(g Graph, k ResourceKey) []string {
	return []string{"--label=com.faultline.launch=" + g.LaunchID, "--label=com.faultline.role=" + string(k), "--label=com.faultline.owner=" + g.OwnerNonce}
}
