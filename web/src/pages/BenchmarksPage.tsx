import { useState } from "react";
import { BenchHeader } from "../components/BenchHeader";
import { DuplicatesTable } from "../components/DuplicatesTable";
import { KillPanel } from "../components/KillPanel";
import { LatencyChart } from "../components/LatencyChart";
import { MissingResults } from "../components/MissingResults";
import { SuccessChart } from "../components/SuccessChart";
import { useBenchCompare } from "../hooks/useBenchCompare";
import { describeSeries, latencySeries, successSeries } from "../lib/benchData";

export function BenchmarksPage() {
  const state = useBenchCompare();
  const [logScale, setLogScale] = useState(false);

  return (
    <section>
      <h1>Benchmarks</h1>
      {state.kind === "loading" && <p className="muted">Loading benchmark results…</p>}
      {state.kind === "missing" && <MissingResults />}
      {state.kind === "error" && (
        <div className="notice notice-bad" role="alert">
          <p>Could not load benchmark results: {state.message}</p>
        </div>
      )}
      {state.kind === "ready" && (
        <>
          <BenchHeader doc={state.doc} />

          <h2>Success rate by fault rate</h2>
          <SuccessChart series={successSeries(state.doc)} />

          <h2>p95 latency by fault rate</h2>
          <LatencyChart
            series={latencySeries(state.doc, logScale)}
            logScale={logScale}
            onToggleLog={() => {
              setLogScale((v) => !v);
            }}
          />

          <h2>Duplicates and recorded final state</h2>
          <DuplicatesTable doc={state.doc} />

          <h2>Kill and resume</h2>
          <KillPanel doc={state.doc} />

          <details className="data-fallback">
            <summary>Show the chart data as text</summary>
            <p>Success rate: {describeSeries(successSeries(state.doc), "%")}</p>
            <p>p95 latency: {describeSeries(latencySeries(state.doc, false), " ms")}</p>
          </details>
        </>
      )}
    </section>
  );
}
