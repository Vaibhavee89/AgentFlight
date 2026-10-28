import { useCallback, useEffect, useState } from "react";
import { ApiError, getEventsPage, getRun, parseRecordedEvent, streamPath } from "../api/client";
import type { RecordedEvent } from "../api/types";
import { asRecord, stringAt } from "../lib/payloads";
import { EventStore } from "../lib/eventStore";

/** Page size for the paginated events endpoint (the server's maximum). */
export const PAGE_SIZE = 1000;

export interface RunEventsState {
  events: RecordedEvent[];
  /** Status from GET /runs/:id, or from run_finished once it arrives. */
  runStatus: string | null;
  loading: boolean;
  /** True while the live stream is open. */
  live: boolean;
  error: string | null;
  notFound: boolean;
}

const INITIAL: RunEventsState = {
  events: [],
  runStatus: null,
  loading: true,
  live: false,
  error: null,
  notFound: false,
};

/**
 * Owns the event log of one run. A running run is followed through its SSE
 * stream, and a finished run is loaded through the paginated endpoint.
 *
 * The stream is trusted only as far as its seq numbers go: events are keyed by
 * seq, so redelivery after a reconnect is ignored. When the stream errors,
 * the browser reconnects by itself with Last-Event-ID. If the run turns out to
 * have finished, the stream is closed and the rest is loaded by page. The
 * stream is also closed as soon as run_finished arrives, before the server
 * closes it, so the browser does not reconnect to a finished run.
 */
export interface RunEvents extends RunEventsState {
  /** Discards the local copy and re-reads the run from the server. */
  reload: () => void;
}

export function useRunEvents(runId: string): RunEvents {
  const [state, setState] = useState<RunEventsState>(INITIAL);
  const [generation, setGeneration] = useState(0);

  useEffect(() => {
    const controller = new AbortController();
    const cancelled = (): boolean => controller.signal.aborted;
    const store = new EventStore();
    let source: EventSource | null = null;

    // Status-only updates keep the events already on screen, so a reconnect or
    // a status check does not blank the timeline while the data reloads.
    const publish = (patch: Partial<RunEventsState>, withEvents = true) => {
      if (cancelled()) return;
      setState((prev) => ({
        ...prev,
        ...(withEvents ? { events: store.snapshot() } : {}),
        ...patch,
      }));
    };

    const closeSource = () => {
      source?.close();
      source = null;
    };

    const loadRest = async (): Promise<void> => {
      let after = store.lastSeq();
      for (;;) {
        const page = await getEventsPage(runId, { after, limit: PAGE_SIZE }, controller.signal);
        store.addAll(page);
        if (page.length < PAGE_SIZE) return;
        after = store.lastSeq();
      }
    };

    const finishWith = (runStatus: string | null) => {
      closeSource();
      publish({ runStatus, live: false, loading: false });
    };

    const openStream = () => {
      const es = new EventSource(streamPath(runId));
      source = es;
      publish({ live: true, loading: false });

      es.onmessage = (message: MessageEvent<unknown>) => {
        if (cancelled() || typeof message.data !== "string") return;
        const event = parseRecordedEvent(message.data);
        if (event === null) return;
        store.add(event);
        if (event.type === "run_finished") {
          const status = stringAt(asRecord(event.payload), "Status") ?? null;
          finishWith(status);
          return;
        }
        publish({});
      };

      es.onerror = () => {
        if (cancelled()) return;
        void checkAfterError();
      };
    };

    const checkAfterError = async (): Promise<void> => {
      try {
        const run = await getRun(runId, controller.signal);
        if (cancelled()) return;
        if (run.status === "running") return; // EventSource reconnects with Last-Event-ID.
        closeSource();
        await loadRest();
        finishWith(run.status);
      } catch (error: unknown) {
        if (cancelled()) return;
        if (error instanceof ApiError && error.status === 404) {
          closeSource();
          publish({ notFound: true, loading: false, live: false });
        }
        // Other failures leave the stream to retry on its own.
      }
    };

    const start = async (): Promise<void> => {
      try {
        const run = await getRun(runId, controller.signal);
        if (cancelled()) return;
        publish({ runStatus: run.status }, false);
        if (run.status === "running") {
          openStream();
          return;
        }
        await loadRest();
        finishWith(run.status);
      } catch (error: unknown) {
        if (cancelled()) return;
        if (error instanceof ApiError && error.status === 404) {
          publish({ notFound: true, loading: false });
          return;
        }
        const message = error instanceof Error ? error.message : "could not load the run";
        publish({ error: message, loading: false });
      }
    };

    void start();
    return () => {
      controller.abort();
      closeSource();
    };
  }, [runId, generation]);

  const reload = useCallback(() => {
    setGeneration((g) => g + 1);
  }, []);

  return { ...state, reload };
}
