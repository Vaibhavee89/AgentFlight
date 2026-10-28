import { useEffect, useState } from "react";
import { getHealth } from "../api/client";
import type { Health } from "../api/types";

const POLL_MS = 15_000;

type State =
  { kind: "checking" } | { kind: "ok"; health: Health } | { kind: "down"; reason: string };

/** Shows whether the agentflight API answers /healthz, and how many runs are active. */
export function HealthIndicator() {
  const [state, setState] = useState<State>({ kind: "checking" });

  useEffect(() => {
    const controller = new AbortController();
    let timer: ReturnType<typeof setTimeout> | undefined;

    const check = async (): Promise<void> => {
      try {
        const health = await getHealth(controller.signal);
        setState({ kind: "ok", health });
      } catch (error: unknown) {
        if (controller.signal.aborted) return;
        const reason = error instanceof Error ? error.message : "unreachable";
        setState({ kind: "down", reason });
      }
      if (!controller.signal.aborted) {
        timer = setTimeout(() => void check(), POLL_MS);
      }
    };

    void check();
    return () => {
      controller.abort();
      if (timer !== undefined) clearTimeout(timer);
    };
  }, []);

  if (state.kind === "checking") {
    return <span className="health health-checking">API: checking…</span>;
  }
  if (state.kind === "down") {
    return (
      <span className="health health-down" title={state.reason}>
        API: unreachable
      </span>
    );
  }
  return (
    <span className="health health-ok">API: ok · {String(state.health.active_runs)} active</span>
  );
}
