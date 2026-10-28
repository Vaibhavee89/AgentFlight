import { Link, useParams } from "react-router-dom";

/** Placeholder until the flight recorder timeline lands (D3). */
export function RunDetailPage() {
  const { id = "" } = useParams();
  return (
    <section>
      <p>
        <Link to="/runs">← Runs</Link>
      </p>
      <h1>Run {id}</h1>
      <p className="muted">The event timeline for this run arrives in the next phase.</p>
    </section>
  );
}
