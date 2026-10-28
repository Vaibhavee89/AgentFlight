import { useState } from "react";
import { Link, useNavigate, useSearchParams } from "react-router-dom";
import { StatusBadge } from "../components/StatusBadge";
import { NewRunDialog } from "../components/NewRunDialog";
import { useRuns } from "../hooks/useRuns";
import { KNOWN_STATUSES, formatDuration } from "../lib/status";
import type { RunListItem } from "../api/types";

function formatTime(iso: string | null): string {
  if (iso === null) return "—";
  return new Date(iso).toLocaleString();
}

export function RunsPage() {
  const navigate = useNavigate();
  const [params, setParams] = useSearchParams();
  const status = params.get("status") ?? "";
  const { state, reload } = useRuns(status);
  const [creating, setCreating] = useState(false);

  const onFilter = (value: string) => {
    setParams(value ? { status: value } : {});
  };

  return (
    <section>
      <div className="page-head">
        <h1>Runs</h1>
        <div className="page-actions">
          <label className="filter">
            <span>Status</span>
            <select
              value={status}
              onChange={(event) => {
                onFilter(event.target.value);
              }}
            >
              <option value="">All</option>
              {KNOWN_STATUSES.map((s) => (
                <option key={s} value={s}>
                  {s}
                </option>
              ))}
            </select>
          </label>
          <button
            type="button"
            className="button button-primary"
            onClick={() => {
              setCreating(true);
            }}
          >
            New run
          </button>
        </div>
      </div>

      {state.kind === "loading" && <p className="muted">Loading runs…</p>}

      {state.kind === "error" && (
        <div className="notice notice-bad" role="alert">
          <p>Could not load runs: {state.message}</p>
          <button type="button" className="button" onClick={reload}>
            Retry
          </button>
        </div>
      )}

      {state.kind === "ready" && state.runs.length === 0 && (
        <div className="notice">
          <p>{status === "" ? "No runs yet." : `No ${status} runs.`}</p>
          <button
            type="button"
            className="button button-primary"
            onClick={() => {
              setCreating(true);
            }}
          >
            Start a run
          </button>
        </div>
      )}

      {state.kind === "ready" && state.runs.length > 0 && (
        <RunsTable
          runs={state.runs}
          now={state.fetchedAt}
          onOpen={(id) => {
            void navigate(`/runs/${encodeURIComponent(id)}`);
          }}
        />
      )}

      {creating && (
        <NewRunDialog
          onClose={() => {
            setCreating(false);
          }}
        />
      )}
    </section>
  );
}

interface RunsTableProps {
  runs: RunListItem[];
  now: number;
  onOpen: (id: string) => void;
}

function RunsTable({ runs, now, onOpen }: RunsTableProps) {
  return (
    <div className="table-wrap">
      <table className="runs-table">
        <thead>
          <tr>
            <th>Status</th>
            <th>Run</th>
            <th>Started</th>
            <th className="num">Duration</th>
            <th className="num">Steps</th>
            <th className="num">Tool calls</th>
            <th className="num">Retries</th>
            <th className="num">Tokens</th>
          </tr>
        </thead>
        <tbody>
          {runs.map((run) => (
            <tr
              key={run.id}
              className="clickable"
              onClick={() => {
                onOpen(run.id);
              }}
            >
              <td>
                <StatusBadge status={run.status} />
              </td>
              <td>
                <Link
                  to={`/runs/${encodeURIComponent(run.id)}`}
                  onClick={(event) => {
                    event.stopPropagation();
                  }}
                >
                  {run.id}
                </Link>
              </td>
              <td>{formatTime(run.started_at)}</td>
              <td className="num">{formatDuration(run.started_at, run.finished_at, now)}</td>
              <td className="num">{run.steps}</td>
              <td className="num">{run.tool_calls}</td>
              <td className="num">{run.retries}</td>
              <td className="num">{run.total_tokens}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
