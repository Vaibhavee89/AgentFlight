import { RUNTIME_LABELS, duplicateCells, faultRateLabel } from "../lib/benchData";
import type { CompareDocument } from "../api/types";
import { Tooltip } from "./Tooltip";

export function DuplicatesTable({ doc }: { doc: CompareDocument }) {
  const cells = duplicateCells(doc);
  return (
    <div className="table-wrap">
      <table className="bench-table">
        <caption>
          Duplicate tool executions and recorded final state, per runtime and fault rate
        </caption>
        <thead>
          <tr>
            <th scope="col">Runtime</th>
            <th scope="col" className="num">
              Fault rate
            </th>
            <th scope="col" className="num">
              <span className="th-with-tip">
                Duplicate tool executions
                <Tooltip label="duplicate tool executions">
                  AgentFlight&apos;s duplicates are policy retries: a tool call that hit a transient
                  fault runs again. Baseline never retries, so its count stays at zero here.
                </Tooltip>
              </span>
            </th>
            <th scope="col" className="num">
              Runs with recorded final state
            </th>
          </tr>
        </thead>
        <tbody>
          {cells.map((c) => (
            <tr key={`${c.runtime}-${String(c.faultRate)}`}>
              <td>{RUNTIME_LABELS[c.runtime]}</td>
              <td className="num">{faultRateLabel(c.faultRate)}</td>
              <td className="num">{String(c.duplicateToolExecs)}</td>
              <td className="num">
                {String(c.runsWithFinalState)} / {String(c.executions)}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
