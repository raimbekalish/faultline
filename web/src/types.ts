export type Status = "complete" | "failed" | "canceled" | "running";
export type PhaseName = "baseline" | "fault" | "recovery";
export const phases: PhaseName[] = ["baseline", "fault", "recovery"];
export interface Spec {
  schema_version: number;
  samples_per_phase: number;
  latency_ms: number;
  request_timeout_ms: number;
}
export interface Summary {
  requests: number;
  failures: number;
  failure_rate: number;
  min_ms: number;
  mean_ms: number;
  p50_ms: number;
  p95_ms: number;
  p99_ms: number;
  max_ms: number;
}
export interface Sample {
  duration_ms: number;
  status: number;
  error?: string;
  trace_id?: string;
  span_id?: string;
}
export interface Phase {
  samples: Sample[] | null;
  summary: Summary;
}
export interface Environment {
  schema_version: number;
  spec_hash: string;
  faultline_revision: string;
  go_version: string;
  runner_os: string;
  runner_arch: string;
  measurement_methodology: string;
  tracing_enabled: boolean;
  container_runtime?: {
    schema_version: number;
    docker_server_version: string;
    images: { component: string; image_id: string; reference?: string }[];
  };
}
export interface Report {
  environment?: Environment;
  schema_version: number;
  status: Status;
  run_id: string;
  started_at: string;
  finished_at: string;
  config: Omit<Spec, "schema_version">;
  error?: string;
  cleanup_complete: boolean;
  revision: string;
  go_version: string;
  toxiproxy_version: string;
  baseline: Phase;
  fault: Phase;
  recovery: Phase;
  telemetry?: { enabled?: boolean };
}
export interface Run {
  id: string;
  run_id?: string;
  status: Status;
  created_at: string;
  finished_at?: string;
  spec: Spec;
  spec_hash: string;
  phases?: Record<PhaseName, Summary>;
  cleanup_complete?: boolean;
  error?: string;
}
export interface Entry extends Run {
  report?: Report;
}
export interface Page {
  items: Run[];
  counts: Record<Status, number>;
  total: number;
  next_cursor: string | null;
}
export interface Delta {
  mean_delta_ms: number;
  p95_delta_ms: number;
  failure_rate_delta: number;
}
export interface Comparison {
  schema_version: number;
  left_id: string;
  right_id: string;
  left_spec: Spec;
  right_spec: Spec;
  same_spec: boolean;
  phases: {
    phase: PhaseName;
    left: Summary;
    right: Summary;
    right_minus_left: Delta;
  }[];
}
