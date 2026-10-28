import { StatusBadge } from "./StatusBadge";
import { formatDuration } from "../lib/status";
import type { RunSummary } from "../lib/timeline";

interface RunHeaderProps {
  runId: string;
  runStatus: string | null;
  summary: RunSummary;
  startedISO: string | null;
  finishedISO: string | null;
  now: number;
  live: boolean;
}

export function RunHeader({
  runId,
  runStatus,
  summary,
  startedISO,
  finishedISO,
  now,
  live,
}: RunHeaderProps) {
  const status = runStatus ?? summary.finalStatus ?? "running";
  const inFlight = summary.stepsStarted - summary.steps;
  const progress =
    status === "running" && inFlight > 0
      ? `step ${String(summary.stepsStarted)} in progress`
      : `${String(summary.steps)} step${summary.steps === 1 ? "" : "s"} completed`;

  return (
    <header className="run-header">
      <div className="run-title">
        <h1>Run</h1>
        <code className="run-id">{runId}</code>
        <StatusBadge status={status} />
        {live && <span className="live-dot">live</span>}
      </div>
      <dl className="run-stats">
        <div>
          <dt>Duration</dt>
          <dd>{formatDuration(startedISO, finishedISO, now)}</dd>
        </div>
        <div>
          <dt>Progress</dt>
          <dd>{progress}</dd>
        </div>
        <div>
          <dt>Tool calls</dt>
          <dd>{String(summary.toolCalls)}</dd>
        </div>
        <div>
          <dt>Retries</dt>
          <dd className={summary.retries > 0 ? "text-warn" : undefined}>
            {String(summary.retries)}
          </dd>
        </div>
        <div>
          <dt>Errors</dt>
          <dd className={summary.errors > 0 ? "text-bad" : undefined}>{String(summary.errors)}</dd>
        </div>
        <div>
          <dt>Tokens in / out</dt>
          <dd>
            {String(summary.inputTokens)} / {String(summary.outputTokens)}
          </dd>
        </div>
      </dl>
    </header>
  );
}
