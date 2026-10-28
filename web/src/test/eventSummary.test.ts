import { describe, expect, it } from "vitest";
import type { RecordedEvent } from "../api/types";
import { summaryLine } from "../lib/eventSummary";
import { formatMs } from "../lib/status";

function ev(type: string, payload: unknown): RecordedEvent {
  return { seq: 1, type, version: 1, time: "2026-10-28T10:00:00Z", payload };
}

describe("summaryLine", () => {
  it.each([
    {
      name: "retry names the attempt, backoff, and reason",
      event: ev("policy_decision", {
        tool: "echo",
        allowed: true,
        retry: true,
        attempt: 2,
        backoff_ms: 40,
        reason: "timed out",
      }),
      retry: { attempt: 2, backoffMs: 40, reason: "timed out" },
      want: "attempt 2 failed, retrying in 40ms — timed out",
    },
    {
      name: "tool result shows the output",
      event: ev("tool_result", { name: "echo", output: { Result: "hi" } }),
      retry: null,
      want: "echo → hi",
    },
    {
      name: "tool result shows its error when it failed",
      event: ev("tool_result", {
        name: "calculator",
        output: { Result: "" },
        error: "division by zero",
      }),
      retry: null,
      want: "calculator failed — division by zero",
    },
    {
      name: "model response with tool calls counts them",
      event: ev("model_response", { Content: "", ToolCalls: [{ Name: "echo" }] }),
      retry: null,
      want: "requests 1 tool call(s)",
    },
    {
      name: "run finished gives the totals",
      event: ev("run_finished", {
        Status: "completed",
        Steps: 2,
        Retries: 1,
        Usage: { InputTokens: 10, OutputTokens: 5 },
      }),
      retry: null,
      want: "completed · 2 steps · 1 retries · 15 tokens",
    },
    {
      name: "denied policy decision",
      event: ev("policy_decision", { tool: "rm", allowed: false }),
      retry: null,
      want: "rm: denied",
    },
    {
      name: "unknown type is empty",
      event: ev("something_new", {}),
      retry: null,
      want: "",
    },
  ])("$name", ({ event, retry, want }) => {
    expect(summaryLine(event, retry)).toBe(want);
  });
});

describe("formatMs", () => {
  it.each([
    { ms: 0, want: "0ms" },
    { ms: 999, want: "999ms" },
    { ms: 1_000, want: "1.0s" },
    { ms: 59_999, want: "60.0s" },
    { ms: 60_000, want: "1m 0s" },
    { ms: 3_600_000, want: "1h 0m" },
  ])("$ms ms is $want", ({ ms, want }) => {
    expect(formatMs(ms)).toBe(want);
  });
});
