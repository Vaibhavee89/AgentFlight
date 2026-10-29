/** The command that writes the file the page reads. Kept in one place so the page and its tests agree. */
export const GENERATE_COMMAND =
  "go run ./cmd/harnessbench compare --suite benchmarks/basic.jsonl --faults 0,0.05,0.1,0.2 --repeats 10 --json docs/compare.json";

export function MissingResults() {
  return (
    <div className="notice">
      <p>
        No benchmark results have been generated yet. The server reads them from its bench
        directory.
      </p>
      <p>From the repository root, run:</p>
      <pre className="command">{GENERATE_COMMAND}</pre>
      <p className="muted">
        Then reload this page. Start the server with <code>--bench-dir docs</code> if you use
        another directory.
      </p>
    </div>
  );
}
