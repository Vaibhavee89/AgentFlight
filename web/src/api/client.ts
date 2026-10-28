import type { Health } from "./types";

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

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

async function requestJSON(path: string, signal?: AbortSignal): Promise<unknown> {
  const response = await fetch(`${API_BASE}${path}`, {
    headers: { Accept: "application/json" },
    signal: signal ?? null,
  });
  const text = await response.text();
  if (!response.ok) {
    let message = response.statusText || `HTTP ${String(response.status)}`;
    try {
      const parsed: unknown = JSON.parse(text);
      if (isRecord(parsed) && typeof parsed.error === "string") {
        message = parsed.error;
      }
    } catch {
      // Not JSON: keep the status text.
    }
    throw new ApiError(response.status, message);
  }
  return JSON.parse(text) as unknown;
}

function parseHealth(value: unknown): Health {
  if (
    !isRecord(value) ||
    typeof value.status !== "string" ||
    typeof value.active_runs !== "number"
  ) {
    throw new ApiError(0, "unexpected response from /healthz");
  }
  return { status: value.status, active_runs: value.active_runs };
}

/** GET /healthz. */
export async function getHealth(signal?: AbortSignal): Promise<Health> {
  return parseHealth(await requestJSON("/healthz", signal));
}
