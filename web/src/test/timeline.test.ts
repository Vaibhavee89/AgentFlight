import { describe, expect, it } from "vitest";
import type { RecordedEvent } from "../api/types";
import { buildTimeline } from "../lib/timeline";

function evt(seq: number, type: string, atMs: number, payload: unknown = {}): RecordedEvent {
  return {
    seq,
    type,
    version: 1,
    time: new Date(Date.UTC(2026, 9, 28, 10, 0, 0) + atMs).toISOString(),
    payload,
  };
}

const retryPayload = {
  tool: "echo",
  allowed: true,
  retry: true,
  attempt: 1,
  backoff_ms: 40,
  reason: "timed out",
};

describe("buildTimeline", () => {
  it("groups a two-step run with a retry into run, step, and end sections", () => {
    const events = [
      evt(1, "run_started", 0, { prompt: "go" }),
      evt(2, "model_request", 10),
      evt(3, "model_response", 20, {
        ToolCalls: [{ Name: "echo" }],
        Usage: { InputTokens: 7, OutputTokens: 3 },
      }),
      evt(4, "tool_call", 21),
      evt(5, "policy_decision", 25, retryPayload),
      evt(6, "tool_result", 90),
      evt(7, "model_request", 100),
      evt(8, "model_response", 130, {
        Content: "done",
        Usage: { InputTokens: 5, OutputTokens: 2 },
      }),
      evt(9, "run_finished", 135, { Status: "completed", Steps: 2 }),
    ];
    const { groups, summary } = buildTimeline(events);

    expect(groups.map((g) => [g.kind, g.step, g.events.length])).toEqual([
      ["run", null, 1],
      ["step", 1, 5],
      ["step", 2, 2],
      ["end", null, 1],
    ]);
    expect(summary).toMatchObject({
      steps: 2,
      stepsStarted: 2,
      toolCalls: 1,
      retries: 1,
      errors: 0,
      inputTokens: 12,
      outputTokens: 5,
      finalStatus: "completed",
    });
  });

  it("attaches retry details and offsets from run start", () => {
    const events = [
      evt(1, "run_started", 1000),
      evt(2, "model_request", 1500),
      evt(3, "policy_decision", 1750, retryPayload),
    ];
    const { groups } = buildTimeline(events);
    const retryItem = groups[1]?.events[1];
    expect(retryItem?.retry).toEqual({ attempt: 1, backoffMs: 40, reason: "timed out" });
    expect(retryItem?.offsetMs).toBe(750);
    expect(groups[1]?.durationMs).toBe(250);
  });

  it("counts a step in flight when its model call has not returned", () => {
    const events = [evt(1, "run_started", 0), evt(2, "model_request", 10)];
    const { summary } = buildTimeline(events);
    expect(summary.stepsStarted).toBe(1);
    expect(summary.steps).toBe(0);
    expect(summary.finalStatus).toBeNull();
  });

  it("records errors and reads the error count", () => {
    const events = [
      evt(1, "run_started", 0),
      evt(2, "model_request", 5),
      evt(3, "error", 9, { step: 0, message: "boom" }),
    ];
    expect(buildTimeline(events).summary.errors).toBe(1);
  });

  it("keeps events that arrive before any model call in the run section", () => {
    const { groups } = buildTimeline([evt(1, "checkpoint", 0, { step: 0 })]);
    expect(groups.map((g) => g.kind)).toEqual(["run"]);
  });

  it("returns an empty timeline for no events", () => {
    const { groups, summary } = buildTimeline([]);
    expect(groups).toEqual([]);
    expect(summary.startedAt).toBeNull();
  });
});
