import { useEffect, useMemo, useState } from "react";
import { Link, useParams } from "react-router-dom";
import { cancelRun, resumeRun } from "../api/client";
import { FilterChips } from "../components/FilterChips";
import { ReplayPanel } from "../components/ReplayPanel";
import { RunActions, type PendingAction } from "../components/RunActions";
import { RunHeader } from "../components/RunHeader";
import { Timeline } from "../components/Timeline";
import { useReplay } from "../hooks/useReplay";
import { useRunEvents } from "../hooks/useRunEvents";
import { availableActions } from "../lib/runActions";
import { buildTimeline } from "../lib/timeline";

/** A request to bring one event into view. The nonce makes repeat requests fire again. */
interface Focus {
  seq: number;
  nonce: number;
}

function errorText(error: unknown, fallback: string): string {
  return error instanceof Error ? error.message : fallback;
}

export function RunDetailPage() {
  const { id = "" } = useParams();
  const run = useRunEvents(id);
  const replay = useReplay(id);
  const timeline = useMemo(() => buildTimeline(run.events), [run.events]);

  const [hidden, setHidden] = useState<ReadonlySet<string>>(() => new Set());
  const [pending, setPending] = useState<PendingAction>(null);
  const [optimistic, setOptimistic] = useState<string | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);
  const [focus, setFocus] = useState<Focus | null>(null);

  const status = optimistic ?? run.runStatus;
  const running = status === "running";
  const inFlight: PendingAction = pending ?? (replay.state.kind === "running" ? "replay" : null);

  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (!running) return;
    const timer = setInterval(() => {
      setNow(Date.now());
    }, 1000);
    return () => {
      clearInterval(timer);
    };
  }, [running]);

  const counts = useMemo(() => {
    const m = new Map<string, number>();
    for (const e of run.events) m.set(e.type, (m.get(e.type) ?? 0) + 1);
    return m;
  }, [run.events]);

  const toggle = (type: string) => {
    setHidden((prev) => {
      const next = new Set(prev);
      if (next.has(type)) next.delete(type);
      else next.add(type);
      return next;
    });
  };

  // Brings an event into view: shows its type if a filter hid it, then asks the
  // timeline to scroll to it.
  const showEvent = (seq: number) => {
    const type = run.events.find((e) => e.seq === seq)?.type;
    if (type !== undefined) {
      setHidden((prev) => {
        if (!prev.has(type)) return prev;
        const next = new Set(prev);
        next.delete(type);
        return next;
      });
    }
    setFocus((prev) => ({ seq, nonce: (prev?.nonce ?? 0) + 1 }));
  };

  const mismatchSeq = replay.state.kind === "done" ? replay.state.result.mismatch_at_seq : null;
  useEffect(() => {
    if (mismatchSeq !== null) showEvent(mismatchSeq);
    // Runs once per replay result, so a repeat verify moves the view again.
  }, [replay.state]);

  useEffect(() => {
    if (focus === null) return;
    document
      .getElementById(`event-${String(focus.seq)}`)
      ?.scrollIntoView({ behavior: "smooth", block: "center" });
  }, [focus, timeline.groups]);

  const act = (kind: "cancel" | "resume") => {
    setPending(kind);
    setActionError(null);
    setOptimistic(kind === "cancel" ? "cancelling" : "running");
    const request = kind === "cancel" ? cancelRun(id) : resumeRun(id);
    void request
      .then(() => {
        setOptimistic(null);
        run.reload();
      })
      .catch((error: unknown) => {
        setOptimistic(null);
        setActionError(errorText(error, `could not ${kind} the run`));
      })
      .finally(() => {
        setPending(null);
      });
  };

  if (run.notFound) {
    return (
      <section>
        <p>
          <Link to="/runs">← Runs</Link>
        </p>
        <div className="notice notice-bad">
          <p>
            No run with id <code>{id}</code>.
          </p>
        </div>
      </section>
    );
  }

  if (run.error !== null) {
    return (
      <section>
        <p>
          <Link to="/runs">← Runs</Link>
        </p>
        <div className="notice notice-bad" role="alert">
          <p>Could not load this run: {run.error}</p>
        </div>
      </section>
    );
  }

  return (
    <section>
      <p>
        <Link to="/runs">← Runs</Link>
      </p>
      {run.loading && run.events.length === 0 ? (
        <p className="muted">Loading run…</p>
      ) : (
        <>
          <RunHeader
            runId={id}
            runStatus={status}
            summary={timeline.summary}
            startedISO={run.events.find((e) => e.type === "run_started")?.time ?? null}
            finishedISO={run.events.find((e) => e.type === "run_finished")?.time ?? null}
            now={now}
            live={run.live}
          />
          <RunActions
            actions={availableActions(status)}
            pending={inFlight}
            onCancel={() => {
              act("cancel");
            }}
            onResume={() => {
              act("resume");
            }}
            onVerify={replay.verify}
          />
          {actionError !== null && (
            <p className="action-error" role="alert">
              {actionError}
            </p>
          )}
          <ReplayPanel state={replay.state} onShowMismatch={showEvent} />
          <FilterChips counts={counts} hidden={hidden} onToggle={toggle} />
          <Timeline groups={timeline.groups} hidden={hidden} highlightSeq={focus?.seq ?? null} />
        </>
      )}
    </section>
  );
}
