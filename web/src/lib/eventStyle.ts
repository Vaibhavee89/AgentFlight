export type EventTone = "accent" | "info" | "ok" | "warn" | "bad" | "muted";

export interface EventStyle {
  label: string;
  glyph: string;
  tone: EventTone;
}

/** Labels, glyphs, and colours per event type. Unknown types fall back to their raw name. */
export function eventStyle(type: string, retry: boolean): EventStyle {
  if (retry) return { label: "retry", glyph: "↻", tone: "warn" };
  switch (type) {
    case "run_started":
      return { label: "started", glyph: "▶", tone: "muted" };
    case "run_finished":
      return { label: "finished", glyph: "■", tone: "ok" };
    case "model_request":
      return { label: "model request", glyph: "→", tone: "accent" };
    case "model_response":
      return { label: "model response", glyph: "←", tone: "accent" };
    case "tool_call":
      return { label: "tool call", glyph: "⚙", tone: "info" };
    case "tool_result":
      return { label: "tool result", glyph: "✓", tone: "info" };
    case "policy_decision":
      return { label: "policy", glyph: "⚖", tone: "muted" };
    case "checkpoint":
      return { label: "checkpoint", glyph: "◆", tone: "muted" };
    case "error":
      return { label: "error", glyph: "✗", tone: "bad" };
    default:
      return { label: type, glyph: "•", tone: "muted" };
  }
}

/** Every event type the timeline has a style for, in display order. */
export const FILTERABLE_TYPES = [
  "model_request",
  "model_response",
  "tool_call",
  "tool_result",
  "policy_decision",
  "checkpoint",
  "error",
  "run_finished",
] as const;
