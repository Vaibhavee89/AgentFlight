import type {
  CompareDocument,
  ErrorBody,
  Health,
  KillRow,
  RecordedEvent,
  ReplayResponse,
  RunListItem,
  RunView,
  RuntimeFaultRow,
} from "./types";

/** An HTTP error from the API, carrying the server's message when it sent one. */
export class ApiError extends Error {
  readonly status: number;

  constructor(status: number, message: string) {
    super(message);
    this.name = "ApiError";
    this.status = status;
  }
}

/** The API is served from the same origin as the UI, so paths are relative. */
const API_BASE = "";

type Method = "GET" | "POST";

interface RequestOptions {
  method?: Method;
  body?: unknown;
  signal?: AbortSignal | undefined;
  /** Statuses whose JSON body is a valid result rather than an error. */
  acceptStatus?: readonly number[];
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function isErrorBody(value: unknown): value is ErrorBody {
  return isRecord(value) && typeof value.error === "string";
}

/** Shape-checks a decoded body. Throws ApiError(0) when the server sent something unexpected. */
function ensure<T>(value: unknown, path: string, check: (v: unknown) => v is T): T {
  if (!check(value)) {
    throw new ApiError(0, `unexpected response from ${path}`);
  }
  return value;
}

async function requestJSON(path: string, options: RequestOptions = {}): Promise<unknown> {
  const headers: Record<string, string> = { Accept: "application/json" };
  const init: RequestInit = {
    method: options.method ?? "GET",
    headers,
    signal: options.signal ?? null,
  };
  if (options.body !== undefined) {
    headers["Content-Type"] = "application/json";
    init.body = JSON.stringify(options.body);
  }

  let response: Response;
  try {
    response = await fetch(`${API_BASE}${path}`, init);
  } catch (error: unknown) {
    if (options.signal?.aborted) throw error;
    throw new ApiError(0, "cannot reach the AgentFlight API");
  }

  const text = await response.text();
  const accepted = options.acceptStatus?.includes(response.status) === true;
  if (!response.ok && !accepted) {
    let message = response.statusText || `HTTP ${String(response.status)}`;
    try {
      const parsed: unknown = JSON.parse(text);
      if (isErrorBody(parsed)) message = parsed.error;
    } catch {
      // Not JSON: keep the status text.
    }
    throw new ApiError(response.status, message);
  }
  return JSON.parse(text) as unknown;
}

function isHealth(value: unknown): value is Health {
  return (
    isRecord(value) && typeof value.status === "string" && typeof value.active_runs === "number"
  );
}

function isRunListItem(value: unknown): value is RunListItem {
  return (
    isRecord(value) &&
    typeof value.id === "string" &&
    typeof value.status === "string" &&
    (value.started_at === null || typeof value.started_at === "string") &&
    (value.finished_at === null || typeof value.finished_at === "string") &&
    typeof value.steps === "number" &&
    typeof value.tool_calls === "number" &&
    typeof value.retries === "number" &&
    typeof value.total_tokens === "number"
  );
}

function isRecordedEvent(value: unknown): value is RecordedEvent {
  return (
    isRecord(value) &&
    typeof value.seq === "number" &&
    typeof value.type === "string" &&
    typeof value.version === "number" &&
    typeof value.time === "string" &&
    "payload" in value
  );
}

function isRunView(value: unknown): value is RunView {
  return isRecord(value) && typeof value.id === "string" && typeof value.status === "string";
}

/** GET /healthz. */
export async function getHealth(signal?: AbortSignal): Promise<Health> {
  const body = await requestJSON("/healthz", { signal });
  return ensure(body, "/healthz", isHealth);
}

export interface ListRunsQuery {
  limit?: number;
  status?: string;
}

/** GET /runs with optional limit and status filter. */
export async function listRuns(
  query: ListRunsQuery = {},
  signal?: AbortSignal,
): Promise<RunListItem[]> {
  const params = new URLSearchParams();
  if (query.limit !== undefined) params.set("limit", String(query.limit));
  if (query.status) params.set("status", query.status);
  const qs = params.toString();
  const body = await requestJSON(`/runs${qs ? `?${qs}` : ""}`, { signal });
  if (!Array.isArray(body)) {
    throw new ApiError(0, "unexpected response from /runs");
  }
  return body.map((item: unknown) => ensure(item, "/runs", isRunListItem));
}

/** POST /runs: starts a run and returns its view (202 Accepted). */
export async function startRun(prompt: string, signal?: AbortSignal): Promise<RunView> {
  const body = await requestJSON("/runs", { method: "POST", body: { prompt }, signal });
  return ensure(body, "/runs", isRunView);
}

/** GET /runs/:id. Rejects with ApiError(404) for an unknown run. */
export async function getRun(id: string, signal?: AbortSignal): Promise<RunView> {
  const body = await requestJSON(`/runs/${encodeURIComponent(id)}`, { signal });
  return ensure(body, "/runs/:id", isRunView);
}

export interface EventsPageQuery {
  after?: number;
  limit?: number;
}

/** GET /runs/:id/events with cursor paging: events with seq > after, up to limit. */
export async function getEventsPage(
  id: string,
  query: EventsPageQuery = {},
  signal?: AbortSignal,
): Promise<RecordedEvent[]> {
  const params = new URLSearchParams();
  params.set("after", String(query.after ?? 0));
  if (query.limit !== undefined) params.set("limit", String(query.limit));
  const body = await requestJSON(`/runs/${encodeURIComponent(id)}/events?${params.toString()}`, {
    signal,
  });
  if (!Array.isArray(body)) {
    throw new ApiError(0, "unexpected response from /runs/:id/events");
  }
  return body.map((item: unknown) => ensure(item, "/runs/:id/events", isRecordedEvent));
}

/** Path of the live Server-Sent Events stream for a run. */
export function streamPath(id: string): string {
  return `${API_BASE}/runs/${encodeURIComponent(id)}/stream`;
}

/** Decodes one SSE message body into a recorded event, or null if it is not one. */
export function parseRecordedEvent(data: string): RecordedEvent | null {
  try {
    const value: unknown = JSON.parse(data);
    return isRecordedEvent(value) ? value : null;
  } catch {
    return null;
  }
}

function isReplayResponse(value: unknown): value is ReplayResponse {
  return (
    isRecord(value) &&
    typeof value.ok === "boolean" &&
    (value.mismatch_at_seq === null || typeof value.mismatch_at_seq === "number") &&
    typeof value.detail === "string"
  );
}

/** POST /runs/:id/cancel. The run stops at its next step boundary. */
export async function cancelRun(id: string, signal?: AbortSignal): Promise<RunView> {
  const body = await requestJSON(`/runs/${encodeURIComponent(id)}/cancel`, {
    method: "POST",
    signal,
  });
  return ensure(body, "/runs/:id/cancel", isRunView);
}

/** POST /runs/:id/resume. Continues from the last checkpoint. */
export async function resumeRun(id: string, signal?: AbortSignal): Promise<RunView> {
  const body = await requestJSON(`/runs/${encodeURIComponent(id)}/resume`, {
    method: "POST",
    signal,
  });
  return ensure(body, "/runs/:id/resume", isRunView);
}

/**
 * POST /runs/:id/replay. A divergence (200) and a run that cannot be replayed
 * (422) both carry a ReplayResponse, so both resolve. Other failures reject.
 */
export async function replayRun(id: string, signal?: AbortSignal): Promise<ReplayResponse> {
  const body = await requestJSON(`/runs/${encodeURIComponent(id)}/replay`, {
    method: "POST",
    signal,
    acceptStatus: [422],
  });
  return ensure(body, "/runs/:id/replay", isReplayResponse);
}

function isRuntimeFaultRow(value: unknown): value is RuntimeFaultRow {
  return (
    isRecord(value) &&
    typeof value.runtime === "string" &&
    typeof value.fault_rate === "number" &&
    typeof value.executions === "number" &&
    typeof value.success_rate === "number" &&
    typeof value.mean_steps === "number" &&
    typeof value.p95_latency_ms === "number" &&
    typeof value.duplicate_tool_execs === "number" &&
    typeof value.runs_with_final_state === "number"
  );
}

function isKillRow(value: unknown): value is KillRow {
  return (
    isRecord(value) &&
    typeof value.runtime === "string" &&
    typeof value.attempts === "number" &&
    typeof value.recovered === "number" &&
    typeof value.recovery_rate === "number" &&
    typeof value.total_tool_execs === "number" &&
    typeof value.expected_tool_execs === "number" &&
    typeof value.duplicate_tool_execs === "number"
  );
}

function isCompareDocument(value: unknown): value is CompareDocument {
  return (
    isRecord(value) &&
    typeof value.schema_version === "number" &&
    typeof value.generated_at === "string" &&
    typeof value.git_commit === "string" &&
    typeof value.seed === "number" &&
    typeof value.suite === "string" &&
    typeof value.repeats === "number" &&
    Array.isArray(value.fault_rates) &&
    Array.isArray(value.rows) &&
    Array.isArray(value.kill_scenario)
  );
}

/**
 * GET /bench/compare. Rejects with ApiError(404) when no results have been
 * generated yet; the message names the command that produces them.
 */
export async function getBenchCompare(signal?: AbortSignal): Promise<CompareDocument> {
  const body = await requestJSON("/bench/compare", { signal });
  const doc = ensure(body, "/bench/compare", isCompareDocument);
  if (doc.schema_version !== 1) {
    throw new ApiError(0, `unsupported benchmark schema version ${String(doc.schema_version)}`);
  }
  for (const row of doc.rows) ensure(row, "/bench/compare", isRuntimeFaultRow);
  for (const row of doc.kill_scenario) ensure(row, "/bench/compare", isKillRow);
  return doc;
}
