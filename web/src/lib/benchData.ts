import type { CompareDocument, KillRow, RuntimeFaultRow } from "../api/types";

/** The two runtimes the comparison always contains, in display order. */
export const RUNTIMES = ["agentflight", "baseline"] as const;
export type Runtime = (typeof RUNTIMES)[number];

export const RUNTIME_LABELS: Record<Runtime, string> = {
  agentflight: "AgentFlight",
  baseline: "Baseline",
};

/**
 * A log-scale axis cannot show zero. A latency of 0 is drawn at this floor
 * instead, and the axis starts here.
 */
export const LOG_FLOOR_MS = 0.001;

/** One fault rate, with one value per runtime. A missing runtime is null. */
export interface Pivoted {
  faultRate: number;
  agentflight: number | null;
  baseline: number | null;
}

export function isRuntime(name: string): name is Runtime {
  return (RUNTIMES as readonly string[]).includes(name);
}

/** Fault rates in ascending order, from the document's own list. */
export function faultRatesOf(doc: CompareDocument): number[] {
  return [...doc.fault_rates].sort((a, b) => a - b);
}

/**
 * Turns per-runtime rows into one row per fault rate, which is the shape the
 * charts draw. Values come from the metric function, so the same mapping
 * serves success rate, latency, and duplicates.
 */
export function pivotByFaultRate(
  rows: readonly RuntimeFaultRow[],
  metric: (row: RuntimeFaultRow) => number,
): Pivoted[] {
  const byRate = new Map<number, Pivoted>();
  for (const row of rows) {
    if (!isRuntime(row.runtime)) continue;
    let point = byRate.get(row.fault_rate);
    if (point === undefined) {
      point = { faultRate: row.fault_rate, agentflight: null, baseline: null };
      byRate.set(row.fault_rate, point);
    }
    point[row.runtime] = metric(row);
  }
  return [...byRate.values()].sort((a, b) => a.faultRate - b.faultRate);
}

/** Success rate as a percentage, one point per fault rate and runtime. */
export function successSeries(doc: CompareDocument): Pivoted[] {
  return pivotByFaultRate(doc.rows, (r) => r.success_rate * 100);
}

/**
 * p95 latency in milliseconds. On a log scale, zero is raised to LOG_FLOOR_MS
 * so its bar is still drawn.
 */
export function latencySeries(doc: CompareDocument, logScale: boolean): Pivoted[] {
  return pivotByFaultRate(doc.rows, (r) => {
    const ms = r.p95_latency_ms;
    return logScale ? Math.max(ms, LOG_FLOOR_MS) : ms;
  });
}

export function faultRateLabel(rate: number): string {
  return `${String(Math.round(rate * 1000) / 10)}%`;
}

export interface DuplicateCell {
  runtime: Runtime;
  faultRate: number;
  duplicateToolExecs: number;
  runsWithFinalState: number;
  executions: number;
}

/** Rows for the duplicates table, ordered by fault rate then runtime. */
export function duplicateCells(doc: CompareDocument): DuplicateCell[] {
  return doc.rows
    .filter((r): r is RuntimeFaultRow & { runtime: Runtime } => isRuntime(r.runtime))
    .map((r) => ({
      runtime: r.runtime,
      faultRate: r.fault_rate,
      duplicateToolExecs: r.duplicate_tool_execs,
      runsWithFinalState: r.runs_with_final_state,
      executions: r.executions,
    }))
    .sort(
      (a, b) =>
        a.faultRate - b.faultRate || RUNTIMES.indexOf(a.runtime) - RUNTIMES.indexOf(b.runtime),
    );
}

export interface KillCard {
  runtime: Runtime;
  label: string;
  recovered: number;
  attempts: number;
  duplicateToolExecs: number;
  expectedToolExecs: number;
  totalToolExecs: number;
}

/** One card per runtime for the kill/resume panel, in display order. */
export function killCards(doc: CompareDocument): KillCard[] {
  const cards: KillCard[] = [];
  for (const row of doc.kill_scenario) {
    if (!isRuntime(row.runtime)) continue;
    cards.push(cardFrom(row, row.runtime));
  }
  return cards.sort((a, b) => RUNTIMES.indexOf(a.runtime) - RUNTIMES.indexOf(b.runtime));
}

function cardFrom(row: KillRow, runtime: Runtime): KillCard {
  return {
    runtime,
    label: RUNTIME_LABELS[runtime],
    recovered: row.recovered,
    attempts: row.attempts,
    duplicateToolExecs: row.duplicate_tool_execs,
    expectedToolExecs: row.expected_tool_execs,
    totalToolExecs: row.total_tool_execs,
  };
}

/** Plain-text summary of a series, used as an accessible chart label. */
export function describeSeries(series: readonly Pivoted[], unit: string): string {
  return RUNTIMES.map((runtime) => {
    const parts = series.map((p) => {
      const v = p[runtime];
      return `${faultRateLabel(p.faultRate)} ${v === null ? "n/a" : `${roundTo(v, 2)}${unit}`}`;
    });
    return `${RUNTIME_LABELS[runtime]}: ${parts.join(", ")}`;
  }).join(". ");
}

function roundTo(v: number, digits: number): string {
  return String(Math.round(v * 10 ** digits) / 10 ** digits);
}

/** Chart colours. SVG presentation attributes cannot resolve CSS variables, so these are literal. */
export const RUNTIME_COLORS: Record<Runtime, string> = {
  agentflight: "#58a6ff",
  baseline: "#d29922",
};
