import { describe, expect, it } from "vitest";
import { eventStyle } from "../lib/eventStyle";

describe("eventStyle", () => {
  it.each([
    { type: "model_request", label: "model request", tone: "accent" },
    { type: "model_response", label: "model response", tone: "accent" },
    { type: "tool_call", label: "tool call", tone: "info" },
    { type: "tool_result", label: "tool result", tone: "info" },
    { type: "policy_decision", label: "policy", tone: "muted" },
    { type: "checkpoint", label: "checkpoint", tone: "muted" },
    { type: "error", label: "error", tone: "bad" },
    { type: "run_finished", label: "finished", tone: "ok" },
  ])("$type is $label, coloured $tone", ({ type, label, tone }) => {
    const style = eventStyle(type, false);
    expect(style.label).toBe(label);
    expect(style.tone).toBe(tone);
  });

  it("gives a retry its own warn style regardless of type", () => {
    expect(eventStyle("policy_decision", true)).toEqual({
      label: "retry",
      glyph: "↻",
      tone: "warn",
    });
  });

  it("falls back to the raw type for unknown events", () => {
    expect(eventStyle("future_event", false).label).toBe("future_event");
  });
});
