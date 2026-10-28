import { useEffect, useMemo, useState } from "react";
import { Link, useParams } from "react-router-dom";
import { FilterChips } from "../components/FilterChips";
import { RunHeader } from "../components/RunHeader";
import { Timeline } from "../components/Timeline";
import { useRunEvents } from "../hooks/useRunEvents";
import { buildTimeline } from "../lib/timeline";

export function RunDetailPage() {
  const { id = "" } = useParams();
  const state = useRunEvents(id);
  const timeline = useMemo(() => buildTimeline(state.events), [state.events]);
  const [hidden, setHidden] = useState<ReadonlySet<string>>(() => new Set());
  const running =
    (state.runStatus ?? "running") === "running" && !state.notFound && state.error === null;

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
    for (const e of state.events) m.set(e.type, (m.get(e.type) ?? 0) + 1);
    return m;
  }, [state.events]);

  const toggle = (type: string) => {
    setHidden((prev) => {
      const next = new Set(prev);
      if (next.has(type)) next.delete(type);
      else next.add(type);
      return next;
    });
  };

  if (state.notFound) {
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

  if (state.error !== null) {
    return (
      <section>
        <p>
          <Link to="/runs">← Runs</Link>
        </p>
        <div className="notice notice-bad" role="alert">
          <p>Could not load this run: {state.error}</p>
        </div>
      </section>
    );
  }

  return (
    <section>
      <p>
        <Link to="/runs">← Runs</Link>
      </p>
      {state.loading && state.events.length === 0 ? (
        <p className="muted">Loading run…</p>
      ) : (
        <>
          <RunHeader
            runId={id}
            runStatus={state.runStatus}
            summary={timeline.summary}
            startedISO={state.events.find((e) => e.type === "run_started")?.time ?? null}
            finishedISO={state.events.find((e) => e.type === "run_finished")?.time ?? null}
            now={now}
            live={state.live}
          />
          <FilterChips counts={counts} hidden={hidden} onToggle={toggle} />
          <Timeline groups={timeline.groups} hidden={hidden} />
        </>
      )}
    </section>
  );
}
