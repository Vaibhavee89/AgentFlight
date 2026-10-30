import type { KeyboardEvent } from "react";
import type { StepGroup } from "../lib/timeline";
import { formatMs } from "../lib/status";
import { nextIndex } from "../lib/keyboard";
import { EventRow } from "./EventRow";

interface TimelineProps {
  groups: StepGroup[];
  hidden: ReadonlySet<string>;
  highlightSeq?: number | null;
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
export function Timeline({ groups, hidden, highlightSeq = null }: TimelineProps) {
  const sections = groups
    .map((group) => ({ group, events: group.events.filter((e) => !hidden.has(e.event.type)) }))
    .filter(({ events }) => events.length > 0);

  if (sections.length === 0) {
    return <p className="muted">No events match the current filters.</p>;
  }

  // Arrow keys, Home, and End move between event rows. Enter and Space, on the
  // focused row, expand it through its own button.
  const onKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    const heads = Array.from(
      event.currentTarget.querySelectorAll<HTMLButtonElement>(".event-head"),
    );
    const current = heads.findIndex((h) => h === document.activeElement);
    const target = nextIndex(current, heads.length, event.key);
    if (target === null) return;
    event.preventDefault();
    heads[target]?.focus();
  };

  return (
    <div className="timeline" role="region" aria-label="Event timeline" onKeyDown={onKeyDown}>
      <p className="sr-only">
        Use the arrow keys to move between events, Home and End to jump to the first or last, and
        Enter to expand.
      </p>
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
              <EventRow
                key={item.event.seq}
                item={item}
                highlighted={item.event.seq === highlightSeq}
              />
            ))}
          </ol>
        </section>
      ))}
    </div>
  );
}
