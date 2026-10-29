import { useEffect, useState } from "react";
import { ApiError, getBenchCompare } from "../api/client";
import type { CompareDocument } from "../api/types";

export type BenchState =
  | { kind: "loading" }
  | { kind: "ready"; doc: CompareDocument }
  | { kind: "missing"; message: string }
  | { kind: "error"; message: string };

/** Loads the benchmark comparison once. A 404 means it has not been generated. */
export function useBenchCompare(): BenchState {
  const [state, setState] = useState<BenchState>({ kind: "loading" });

  useEffect(() => {
    const controller = new AbortController();
    getBenchCompare(controller.signal)
      .then((doc) => {
        if (!controller.signal.aborted) setState({ kind: "ready", doc });
      })
      .catch((error: unknown) => {
        if (controller.signal.aborted) return;
        if (error instanceof ApiError && error.status === 404) {
          setState({ kind: "missing", message: error.message });
          return;
        }
        const message = error instanceof Error ? error.message : "could not load benchmark results";
        setState({ kind: "error", message });
      });
    return () => {
      controller.abort();
    };
  }, []);

  return state;
}
