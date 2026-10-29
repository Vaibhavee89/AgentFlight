import type { CompareDocument } from "../api/types";
import { RUNTIME_COLORS, killCards } from "../lib/benchData";

/** Mid-run kill and recover: how many killed runs each runtime finished, and what it re-executed to get there. */
export function KillPanel({ doc }: { doc: CompareDocument }) {
  const cards = killCards(doc);
  if (cards.length === 0) return <p className="muted">No kill and resume results in this file.</p>;
  return (
    <div className="kill-grid">
      {cards.map((card) => (
        <section
          key={card.runtime}
          className="kill-card"
          aria-label={`${card.label} kill and resume`}
        >
          <h3 style={{ color: RUNTIME_COLORS[card.runtime] }}>{card.label}</h3>
          <p className="kill-score">
            Recovered <strong>{String(card.recovered)}</strong> of {String(card.attempts)}
          </p>
          <progress
            max={card.attempts}
            value={card.recovered}
            aria-label={`${card.label} recovered ${String(card.recovered)} of ${String(card.attempts)} killed runs`}
          />
          <p className="muted">
            {String(card.duplicateToolExecs)} duplicate tool executions (
            {String(card.totalToolExecs)} run, {String(card.expectedToolExecs)} expected)
          </p>
        </section>
      ))}
    </div>
  );
}
