import type { CompareDocument } from "../api/types";

export function BenchHeader({ doc }: { doc: CompareDocument }) {
  const commit = doc.git_commit === "unknown" ? "unknown" : doc.git_commit.slice(0, 7);
  return (
    <dl className="bench-meta">
      <div>
        <dt>Suite</dt>
        <dd>{doc.suite}</dd>
      </div>
      <div>
        <dt>Seed</dt>
        <dd>{String(doc.seed)}</dd>
      </div>
      <div>
        <dt>Repeats</dt>
        <dd>{String(doc.repeats)}</dd>
      </div>
      <div>
        <dt>Git commit</dt>
        <dd>
          <code title={doc.git_commit}>{commit}</code>
        </dd>
      </div>
      <div>
        <dt>Generated</dt>
        <dd>{new Date(doc.generated_at).toLocaleString()}</dd>
      </div>
    </dl>
  );
}
