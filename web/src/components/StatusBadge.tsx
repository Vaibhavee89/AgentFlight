import { statusBadge } from "../lib/status";

export function StatusBadge({ status }: { status: string }) {
  const { label, tone } = statusBadge(status);
  return <span className={`badge badge-${tone}`}>{label}</span>;
}
