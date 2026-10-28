package lab

const LabReference = "faultline-lab:http-work-v1"
const ProxyReference = "ghcr.io/shopify/toxiproxy:2.12.0@sha256:9378ed52a28bc50edc1350f936f518f31fa95f0d15917d6eb40b8e376d1a214e"
const proxyRepoDigest = "ghcr.io/shopify/toxiproxy@sha256:9378ed52a28bc50edc1350f936f518f31fa95f0d15917d6eb40b8e376d1a214e"

type Limits struct {
	MemoryMiB     int `json:"memory_mib"`
	CPUMillicores int `json:"cpu_millicores"`
	PIDs          int `json:"pids"`
	NoFile        int `json:"nofile"`
	TmpfsMiB      int `json:"tmpfs_mib"`
}
type Role struct {
	Name       string   `json:"name"`
	Entrypoint []string `json:"entrypoint"`
	Command    []string `json:"command"`
	Networks   []string `json:"networks"`
	Limits     Limits   `json:"limits"`
}
type Network struct {
	Name        string `json:"name"`
	Internal    bool   `json:"internal"`
	GatewayMode string `json:"gateway_mode"`
	IPv6        bool   `json:"ipv6"`
}
type EnvironmentEntry struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}
type ImageRequirements struct {
	ApplicationEnvEntries int    `json:"application_env_entries"`
	Volumes               int    `json:"volumes"`
	Healthchecks          bool   `json:"healthchecks"`
	OnBuildTriggers       int    `json:"onbuild_triggers"`
	LabReference          string `json:"lab_reference"`
	LabContract           string `json:"lab_contract"`
	LabRevision           string `json:"lab_revision"`
	ProxyReference        string `json:"proxy_reference"`
	ProxyDigestRequired   bool   `json:"proxy_digest_required"`
}
type Definition struct {
	Version                int                 `json:"version"`
	Adapter                string              `json:"adapter"`
	Platform               string              `json:"platform"`
	ImageRequirements      ImageRequirements   `json:"image_requirements"`
	FrontendAlias          string              `json:"frontend_alias"`
	DependencyAlias        string              `json:"dependency_alias"`
	ControlAlias           string              `json:"control_alias"`
	TmpfsOptions           [3]string           `json:"tmpfs_options"`
	Roles                  [4]Role             `json:"roles"`
	Networks               [4]Network          `json:"networks"`
	ApplicationEnvironment [2]EnvironmentEntry `json:"application_environment"`
	ProbeNetworks          [2]string           `json:"probe_networks"`
	UID                    int                 `json:"uid"`
	GID                    int                 `json:"gid"`
	WorkingDirectory       string              `json:"working_directory"`
	ReadOnly               bool                `json:"read_only"`
	Capabilities           string              `json:"capabilities"`
	NoNewPrivileges        bool                `json:"no_new_privileges"`
	Seccomp                string              `json:"seccomp"`
	Logging                string              `json:"logging"`
	Restart                string              `json:"restart"`
	StopSignal             string              `json:"stop_signal"`
	DNSUpstream            string              `json:"dns_upstream"`
	DNSSearch              string              `json:"dns_search"`
	PublishedPorts         bool                `json:"published_ports"`
	HostMounts             bool                `json:"host_mounts"`
	Privileged             bool                `json:"privileged"`
	ApplicationURL         string              `json:"application_url"`
	ControlURL             string              `json:"control_url"`
	ProxyListen            string              `json:"proxy_listen"`
	ProxyUpstream          string              `json:"proxy_upstream"`
	ReadinessRequests      int                 `json:"readiness_requests"`
	ReadinessSeconds       int                 `json:"readiness_seconds"`
	EngineSeconds          int                 `json:"engine_seconds"`
	WorkSeconds            int                 `json:"work_seconds"`
	ProxyCleanupSeconds    int                 `json:"proxy_cleanup_seconds"`
	LabCleanupSeconds      int                 `json:"lab_cleanup_seconds"`
	ResponseBodyBytes      int                 `json:"response_body_bytes"`
	ResponseHeaderBytes    int                 `json:"response_header_bytes"`
	MaxActiveLabs          int                 `json:"max_active_labs"`
}

// Catalog returns fresh slices. This is one closed definition, not a plugin registry.
// These are intended constraints, not assertions about any running container.
func Catalog() Definition {
	small := Limits{64, 500, 64, 256, 0}
	return Definition{
		Version: 1, Adapter: "http-work-v1", Platform: "linux/arm64",
		ImageRequirements: ImageRequirements{0, 0, false, 0, LabReference, "http-work-v1", "full_git_sha40", ProxyReference, true},
		FrontendAlias:     "frontend", DependencyAlias: "dependency", ControlAlias: "toxiproxy-control", TmpfsOptions: [3]string{"nodev", "nosuid", "noexec"},
		Roles: [4]Role{
			{"application", []string{"/app/service"}, []string{}, []string{"edge", "data"}, Limits{128, 500, 64, 256, 32}},
			{"runner", []string{"/app/faultline"}, []string{"lab-worker"}, []string{"edge", "control"}, small},
			{"fixture", []string{"/app/sample"}, []string{"-role", "dependency"}, []string{"backend"}, small},
			{"toxiproxy", []string{"/toxiproxy"}, []string{"-host=toxiproxy-control"}, []string{"control", "data", "backend"}, small},
		},
		Networks:               [4]Network{{"edge", true, "isolated", false}, {"data", true, "isolated", false}, {"control", true, "isolated", false}, {"backend", true, "isolated", false}},
		ApplicationEnvironment: [2]EnvironmentEntry{{"FAULTLINE_DEPENDENCY_URL", "http://toxiproxy:8666/value"}, {"FAULTLINE_DEPENDENCY_TIMEOUT_MS", "250"}},
		ProbeNetworks:          [2]string{"edge", "data"}, UID: 65532, GID: 65532, WorkingDirectory: "/", ReadOnly: true, Capabilities: "drop_all", NoNewPrivileges: true, Seccomp: "runtime_default", Logging: "none", Restart: "no", StopSignal: "SIGTERM", DNSUpstream: "127.0.0.1", DNSSearch: ".",
		ApplicationURL: "http://frontend:8080/work", ControlURL: "http://toxiproxy-control:8474", ProxyListen: "0.0.0.0:8666", ProxyUpstream: "dependency:8080", ReadinessRequests: 20, ReadinessSeconds: 5, EngineSeconds: 120, WorkSeconds: 180, ProxyCleanupSeconds: 5, LabCleanupSeconds: 30, ResponseBodyBytes: 65536, ResponseHeaderBytes: 8192, MaxActiveLabs: 1,
	}
}
