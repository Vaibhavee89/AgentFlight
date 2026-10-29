// Types mirror the JSON written by the Go server (api package). Keep field
// names and optionality identical to the Go structs; see web/AGENTS.md.

/** Response of GET /healthz (api.Server.handleHealth). */
export interface Health {
  status: string;
  active_runs: number;
}

/** One item of GET /runs (api.runListItem). Timestamps are RFC 3339 or null. */
export interface RunListItem {
  id: string;
  status: string;
  started_at: string | null;
  finished_at: string | null;
  steps: number;
  tool_calls: number;
  retries: number;
  total_tokens: number;
}

/** Response of POST /runs and the run view (api.runView). Optional fields are omitted when empty. */
export interface RunView {
  id: string;
  status: string;
  content?: string;
  steps?: number;
  error?: string;
}

/** Body of every non-2xx JSON response (writeError). */
export interface ErrorBody {
  error: string;
}

/** Response of POST /runs/:id/replay (api.replayResponse). */
export interface ReplayResponse {
  ok: boolean;
  mismatch_at_seq: number | null;
  detail: string;
}

/** One recorded event as the events and stream endpoints send it (api.eventView). */
export interface RecordedEvent {
  seq: number;
  type: string;
  version: number;
  time: string;
  payload: unknown;
}

/** One runtime at one fault rate (benchmark.RuntimeFaultRow). Latency is p95 in milliseconds. */
export interface RuntimeFaultRow {
  runtime: string;
  fault_rate: number;
  executions: number;
  success_rate: number;
  mean_steps: number;
  p95_latency_ms: number;
  duplicate_tool_execs: number;
  runs_with_final_state: number;
}

/** One runtime's mid-run kill-and-recover results (benchmark.KillRow). */
export interface KillRow {
  runtime: string;
  attempts: number;
  recovered: number;
  recovery_rate: number;
  total_tool_execs: number;
  expected_tool_execs: number;
  duplicate_tool_execs: number;
}

/** GET /bench/compare (benchmark.CompareDocument). Only schema version 1 is understood. */
export interface CompareDocument {
  schema_version: number;
  generated_at: string;
  git_commit: string;
  seed: number;
  suite: string;
  repeats: number;
  fault_rates: number[];
  rows: RuntimeFaultRow[];
  kill_scenario: KillRow[];
}
