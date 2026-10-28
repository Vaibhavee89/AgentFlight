export const KNOWN_STATUSES = [
  "running",
  "completed",
  "cancelled",
  "failed",
  "interrupted",
] as const;

export type KnownStatus = (typeof KNOWN_STATUSES)[number];

export function isKnownStatus(status: string): status is KnownStatus {
  return (KNOWN_STATUSES as readonly string[]).includes(status);
}

export type BadgeTone = "info" | "ok" | "muted" | "bad" | "warn";

export interface StatusBadgeSpec {
  label: string;
  tone: BadgeTone;
}

/** Maps a run status to its badge label and colour. Unknown statuses are shown as-is, muted. */
export function statusBadge(status: string): StatusBadgeSpec {
  if (!isKnownStatus(status)) return { label: status || "unknown", tone: "muted" };
  switch (status) {
    case "running":
      return { label: "running", tone: "info" };
    case "completed":
      return { label: "completed", tone: "ok" };
    case "cancelled":
      return { label: "cancelled", tone: "muted" };
    case "failed":
      return { label: "failed", tone: "bad" };
    case "interrupted":
      return { label: "interrupted", tone: "warn" };
  }
}

const SECOND = 1000;
const MINUTE = 60 * SECOND;
const HOUR = 60 * MINUTE;

/** Formats a span in milliseconds as 450ms, 2.3s, 2m 5s, or 1h 5m. */
export function formatMs(ms: number): string {
  const span = Math.max(0, ms);
  if (span < SECOND) return `${String(span)}ms`;
  if (span < MINUTE) return `${(span / SECOND).toFixed(1)}s`;
  if (span < HOUR) {
    const m = Math.floor(span / MINUTE);
    const s = Math.floor((span % MINUTE) / SECOND);
    return `${String(m)}m ${String(s)}s`;
  }
  const h = Math.floor(span / HOUR);
  const m = Math.floor((span % HOUR) / MINUTE);
  return `${String(h)}h ${String(m)}m`;
}

/**
 * Formats how long a run has taken. A run without an end is measured to now.
 * Returns an em dash when the run has no start time.
 */
export function formatDuration(
  startedAt: string | null,
  finishedAt: string | null,
  now: number,
): string {
  if (startedAt === null) return "—";
  const start = Date.parse(startedAt);
  const end = finishedAt === null ? now : Date.parse(finishedAt);
  return formatMs(end - start);
}
