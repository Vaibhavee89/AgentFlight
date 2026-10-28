// Types mirror the JSON written by the Go server (api package). Keep field
// names and optionality identical to the Go structs; see web/AGENTS.md.

/** Response of GET /healthz (api.Server.handleHealth). */
export interface Health {
  status: string;
  active_runs: number;
}
