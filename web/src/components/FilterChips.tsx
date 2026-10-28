import { eventStyle, FILTERABLE_TYPES } from "../lib/eventStyle";

interface FilterChipsProps {
  counts: ReadonlyMap<string, number>;
  hidden: ReadonlySet<string>;
  onToggle: (type: string) => void;
}

/** Toggles which event types the timeline shows. Pressed means shown. */
export function FilterChips({ counts, hidden, onToggle }: FilterChipsProps) {
  return (
    <div className="chips" role="group" aria-label="Show event types">
      {FILTERABLE_TYPES.map((type) => {
        const shown = !hidden.has(type);
        const style = eventStyle(type, false);
        return (
          <button
            key={type}
            type="button"
            className={`chip chip-${style.tone}${shown ? " chip-on" : ""}`}
            aria-pressed={shown}
            onClick={() => {
              onToggle(type);
            }}
          >
            <span aria-hidden="true">{style.glyph}</span> {style.label}{" "}
            <span className="chip-count">{String(counts.get(type) ?? 0)}</span>
          </button>
        );
      })}
    </div>
  );
}
