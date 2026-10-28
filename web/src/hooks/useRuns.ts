import { useCallback, useEffect, useState } from "react";
import { listRuns } from "../api/client";
import type { RunListItem } from "../api/types";

/** Poll quickly while a run is executing, slowly otherwise. */
export const POLL_RUNNING_MS = 2_000;
export const POLL_IDLE_MS = 10_000;
export const RUN_LIMIT = 100;

export type RunsState =
  | { kind: "loading" }
  | { kind: "error"; message: string }
  | { kind: "ready"; runs: RunListItem[]; fetchedAt: number };

export interface UseRuns {
  state: RunsState;
  reload: () => void;
}

/** Loads the run list for a status filter ("" = all) and keeps it fresh by polling. */
export function useRuns(status: string): UseRuns {
  const [state, setState] = useState<RunsState>({ kind: "loading" });
  const [token, setToken] = useState(0);

  useEffect(() => {
    const controller = new AbortController();
    // Read through a function so the flag is not narrowed across awaits.
    const stopped = (): boolean => controller.signal.aborted;
    let timer: ReturnType<typeof setTimeout> | undefined;

    const load = async (): Promise<void> => {
      let delay = POLL_IDLE_MS;
      try {
        const runs = await listRuns({ limit: RUN_LIMIT, status }, controller.signal);
        if (stopped()) return;
        setState({ kind: "ready", runs, fetchedAt: Date.now() });
        if (runs.some((run) => run.status === "running")) delay = POLL_RUNNING_MS;
      } catch (error: unknown) {
        if (stopped()) return;
        const message = error instanceof Error ? error.message : "could not load runs";
        setState({ kind: "error", message });
      }
      if (!stopped()) {
        timer = setTimeout(() => void load(), delay);
      }
    };

    void load();
    return () => {
      controller.abort();
      if (timer !== undefined) clearTimeout(timer);
    };
  }, [status, token]);

  const reload = useCallback(() => {
    setState({ kind: "loading" });
    setToken((n) => n + 1);
  }, []);

  return { state, reload };
}
