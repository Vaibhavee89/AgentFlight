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
