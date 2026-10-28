import type { RecordedEvent } from "../api/types";

/** Keeps one copy of each event, keyed by seq, and returns them in seq order. */
export class EventStore {
  private readonly bySeq = new Map<number, RecordedEvent>();

  /** Adds an event. Returns false when its seq was already present. */
  add(event: RecordedEvent): boolean {
    if (this.bySeq.has(event.seq)) return false;
    this.bySeq.set(event.seq, event);
    return true;
  }

  addAll(events: RecordedEvent[]): number {
    let added = 0;
    for (const e of events) if (this.add(e)) added += 1;
    return added;
  }

  /** The highest seq held, or 0 when empty. Used as the resume cursor. */
  lastSeq(): number {
    let max = 0;
    for (const seq of this.bySeq.keys()) if (seq > max) max = seq;
    return max;
  }

  snapshot(): RecordedEvent[] {
    return [...this.bySeq.values()].sort((a, b) => a.seq - b.seq);
  }
}
