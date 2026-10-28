import { useCallback, useState } from "react";
import { replayRun } from "../api/client";
import type { ReplayResponse } from "../api/types";

export type ReplayState =
  | { kind: "idle" }
  | { kind: "running" }
  | { kind: "done"; result: ReplayResponse }
  | { kind: "error"; message: string };

export interface UseReplay {
  state: ReplayState;
  verify: () => void;
}

/** Runs replay verification for a finished run and holds its result. */
export function useReplay(runId: string): UseReplay {
  const [state, setState] = useState<ReplayState>({ kind: "idle" });

  const verify = useCallback(() => {
    setState({ kind: "running" });
    replayRun(runId)
      .then((result) => {
        setState({ kind: "done", result });
      })
      .catch((error: unknown) => {
        const message = error instanceof Error ? error.message : "replay failed";
        setState({ kind: "error", message });
      });
  }, [runId]);

  return { state, verify };
}
