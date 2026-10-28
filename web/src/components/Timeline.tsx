import type { StepGroup } from "../lib/timeline";
import { formatMs } from "../lib/status";
import { EventRow } from "./EventRow";

interface TimelineProps {
  groups: StepGroup[];
  hidden: ReadonlySet<string>;
}

function groupTitle(group: StepGroup): string {
  switch (group.kind) {
    case "run":
      return "Run";
    case "step":
      return `Step ${String(group.step ?? 0)}`;
    case "end":
      return "Finished";
  }
}

/** Vertical timeline, one section per step, with per-step durations. */
export function Timeline({ groups, hidden }: TimelineProps) {
  const sections = groups
    .map((group) => ({ group, events: group.events.filter((e) => !hidden.has(e.event.type)) }))
    .filter(({ events }) => events.length > 0);

  if (sections.length === 0) {
    return <p className="muted">No events match the current filters.</p>;
  }

  return (
    <div className="timeline">
      {sections.map(({ group, events }) => (
        <section
          key={group.id}
          className={`step step-${group.kind}`}
          aria-label={groupTitle(group)}
        >
          <header className="step-head">
            <h2>{groupTitle(group)}</h2>
            {group.kind === "step" && <span className="muted">{formatMs(group.durationMs)}</span>}
          </header>
          <ol className="events">
            {events.map((item) => (
              <EventRow key={item.event.seq} item={item} />
            ))}
          </ol>
        </section>
      ))}
    </div>
  );
}
