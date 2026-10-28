import type { RecordedEvent } from "../api/types";
import { asRecord, boolAt, numberAt, stringAt } from "./payloads";

export interface RetryInfo {
  /** The attempt that failed, 1-based. The next attempt is attempt + 1. */
  attempt: number;
  backoffMs: number | null;
  reason: string;
}

export interface TimelineEvent {
  event: RecordedEvent;
  /** Milliseconds from run_started, or 0 when the run has no start event. */
  offsetMs: number;
  retry: RetryInfo | null;
}

export interface StepGroup {
  id: string;
  kind: "run" | "step" | "end";
  /** 1-based step number for kind "step", otherwise null. */
  step: number | null;
  events: TimelineEvent[];
  /** Span from the group's first event to its last, in milliseconds. */
  durationMs: number;
}

export interface RunSummary {
  /** Completed model calls. */
  steps: number;
  /** Model calls started, which can exceed steps while a call is in flight. */
  stepsStarted: number;
  toolCalls: number;
  retries: number;
  errors: number;
  inputTokens: number;
  outputTokens: number;
  startedAt: number | null;
  finishedAt: number | null;
  finalStatus: string | null;
}

export interface Timeline {
  groups: StepGroup[];
  summary: RunSummary;
}

function parseRetry(event: RecordedEvent): RetryInfo | null {
  if (event.type !== "policy_decision") return null;
  const p = asRecord(event.payload);
  if (boolAt(p, "retry") !== true) return null;
  return {
    attempt: numberAt(p, "attempt") ?? 0,
    backoffMs: numberAt(p, "backoff_ms") ?? null,
    reason: stringAt(p, "reason") ?? "",
  };
}

function tokensOf(event: RecordedEvent): { input: number; output: number } {
  const usage = asRecord(asRecord(event.payload)?.Usage);
  return {
    input: numberAt(usage, "InputTokens") ?? 0,
    output: numberAt(usage, "OutputTokens") ?? 0,
  };
}

/**
 * Groups events into run / step / end sections and summarises the run.
 * Events are expected in seq order. A step starts at each model_request, so
 * every event belongs to the step whose model call preceded it.
 */
export function buildTimeline(events: RecordedEvent[]): Timeline {
  const summary: RunSummary = {
    steps: 0,
    stepsStarted: 0,
    toolCalls: 0,
    retries: 0,
    errors: 0,
    inputTokens: 0,
    outputTokens: 0,
    startedAt: null,
    finishedAt: null,
    finalStatus: null,
  };

  const started = events.find((e) => e.type === "run_started");
  const runStart = started ? Date.parse(started.time) : null;
  summary.startedAt = runStart;

  const groups: StepGroup[] = [];
  let current: StepGroup | null = null;

  for (const event of events) {
    const at = Date.parse(event.time);
    const offsetMs = runStart === null ? 0 : Math.max(0, at - runStart);
    const retry = parseRetry(event);
    const item: TimelineEvent = { event, offsetMs, retry };

    switch (event.type) {
      case "model_request": {
        summary.stepsStarted += 1;
        const step = summary.stepsStarted;
        current = { id: `step-${String(step)}`, kind: "step", step, events: [item], durationMs: 0 };
        groups.push(current);
        break;
      }
      case "run_finished": {
        const p = asRecord(event.payload);
        summary.finishedAt = at;
        summary.finalStatus = stringAt(p, "Status") ?? null;
        current = { id: "end", kind: "end", step: null, events: [item], durationMs: 0 };
        groups.push(current);
        break;
      }
      default: {
        if (current === null) {
          current = { id: "run", kind: "run", step: null, events: [], durationMs: 0 };
          groups.push(current);
        }
        current.events.push(item);
      }
    }

    if (event.type === "model_response") {
      summary.steps += 1;
      const tokens = tokensOf(event);
      summary.inputTokens += tokens.input;
      summary.outputTokens += tokens.output;
    }
    if (event.type === "tool_call") summary.toolCalls += 1;
    if (event.type === "error") summary.errors += 1;
    if (retry !== null) summary.retries += 1;
  }

  for (const group of groups) {
    const first = group.events[0];
    const last = group.events[group.events.length - 1];
    if (first !== undefined && last !== undefined) {
      group.durationMs = Math.max(0, Date.parse(last.event.time) - Date.parse(first.event.time));
    }
  }

  return { groups, summary };
}
