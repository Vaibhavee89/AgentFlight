import type { ErrorBody, Health, RecordedEvent, RunListItem, RunView } from "./types";

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
  if (!response.ok) {
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
