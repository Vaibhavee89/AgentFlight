import type { RecordedEvent } from "../api/types";
import { asRecord, boolAt, numberAt, stringAt } from "./payloads";
import type { RetryInfo } from "./timeline";

function clip(text: string, max: number): string {
  return text.length <= max ? text : `${text.slice(0, max - 1)}…`;
}

/** One line describing an event, read from its payload. Empty when there is nothing useful to say. */
export function summaryLine(event: RecordedEvent, retry: RetryInfo | null): string {
  if (retry !== null) {
    const backoff = retry.backoffMs === null ? "" : `, retrying in ${String(retry.backoffMs)}ms`;
    const reason = retry.reason === "" ? "" : ` — ${clip(retry.reason, 120)}`;
    return `attempt ${String(retry.attempt)} failed${backoff}${reason}`;
  }
  const p = asRecord(event.payload);
  switch (event.type) {
    case "run_started":
      return clip(stringAt(p, "prompt") ?? "", 140);
    case "run_finished": {
      const usage = asRecord(p?.Usage);
      const tokens = (numberAt(usage, "InputTokens") ?? 0) + (numberAt(usage, "OutputTokens") ?? 0);
      return `${stringAt(p, "Status") ?? "unknown"} · ${String(numberAt(p, "Steps") ?? 0)} steps · ${String(numberAt(p, "Retries") ?? 0)} retries · ${String(tokens)} tokens`;
    }
    case "model_request": {
      const results = Array.isArray(p?.ToolResults) ? p.ToolResults.length : 0;
      return results === 0 ? "new conversation turn" : `with ${String(results)} tool result(s)`;
    }
    case "model_response": {
      const calls = Array.isArray(p?.ToolCalls) ? p.ToolCalls.length : 0;
      if (calls > 0) return `requests ${String(calls)} tool call(s)`;
      return clip(stringAt(p, "Content") ?? "", 140);
    }
    case "tool_call":
      return stringAt(asRecord(p?.call), "Name") ?? "";
    case "tool_result": {
      const name = stringAt(p, "name") ?? "";
      const err = stringAt(p, "error");
      if (err !== undefined && err !== "") return `${name} failed — ${clip(err, 120)}`;
      const result = stringAt(asRecord(p?.output), "Result") ?? "";
      return `${name} → ${clip(result, 120)}`;
    }
    case "policy_decision": {
      const verdict = boolAt(p, "allowed") === false ? "denied" : "allowed";
      return `${stringAt(p, "tool") ?? ""}: ${verdict}`;
    }
    case "checkpoint":
      return `saved at step ${String(numberAt(p, "step") ?? 0)}`;
    case "error":
      return clip(stringAt(p, "message") ?? "", 160);
    default:
      return "";
  }
}
