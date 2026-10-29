import { describe, expect, it } from "vitest";
import {
  LOG_FLOOR_MS,
  describeSeries,
  duplicateCells,
  faultRateLabel,
  killCards,
  latencySeries,
  pivotByFaultRate,
  successSeries,
} from "../lib/benchData";
import { compareFixture } from "./fixtures";

describe("pivotByFaultRate", () => {
  it("makes one point per fault rate with one value per runtime", () => {
    const doc = compareFixture();
    const pivoted = pivotByFaultRate(doc.rows, (r) => r.executions);
    expect(pivoted).toEqual([
      { faultRate: 0, agentflight: 120, baseline: 120 },
      { faultRate: 0.2, agentflight: 120, baseline: 120 },
    ]);
  });

  it("leaves a missing runtime as null rather than inventing a value", () => {
    const doc = compareFixture({
      rows: compareFixture().rows.filter((r) => r.runtime === "agentflight"),
    });
    expect(pivotByFaultRate(doc.rows, (r) => r.success_rate)).toEqual([
      { faultRate: 0, agentflight: 1, baseline: null },
      { faultRate: 0.2, agentflight: 0.583, baseline: null },
    ]);
  });

  it("ignores rows for runtimes the page does not know", () => {
    const doc = compareFixture();
    const template = doc.rows[0];
    if (template === undefined) throw new Error("fixture has no rows");
    const extra = [...doc.rows, { ...template, runtime: "mystery" }];
    expect(pivotByFaultRate(extra, (r) => r.executions)).toHaveLength(2);
  });
});

describe("series builders", () => {
  it("expresses success as a percentage", () => {
    const series = successSeries(compareFixture());
    expect(series[0]).toEqual({ faultRate: 0, agentflight: 100, baseline: 75 });
    expect(series[1]?.baseline).toBeCloseTo(40.8);
  });

  it("keeps latency as recorded on a linear scale", () => {
    const series = latencySeries(compareFixture(), false);
    expect(series[0]?.baseline).toBe(0);
    expect(series[1]?.baseline).toBeCloseTo(400.7);
  });

  it("raises zero latency to the log floor so its bar still shows", () => {
    const series = latencySeries(compareFixture(), true);
    expect(series[0]?.baseline).toBe(LOG_FLOOR_MS);
    expect(series[1]?.agentflight).toBeCloseTo(44.6);
  });
});

describe("tables and cards", () => {
  it("orders duplicate cells by fault rate, then runtime", () => {
    const cells = duplicateCells(compareFixture());
    expect(cells.map((c) => [c.faultRate, c.runtime])).toEqual([
      [0, "agentflight"],
      [0, "baseline"],
      [0.2, "agentflight"],
      [0.2, "baseline"],
    ]);
    expect(cells[2]).toMatchObject({
      duplicateToolExecs: 24,
      runsWithFinalState: 120,
      executions: 120,
    });
  });

  it("builds one kill card per runtime with recovery and duplicate counts", () => {
    const cards = killCards(compareFixture());
    expect(cards.map((c) => [c.label, c.recovered, c.attempts, c.duplicateToolExecs])).toEqual([
      ["AgentFlight", 30, 30, 0],
      ["Baseline", 20, 30, 30],
    ]);
    expect(cards[1]).toMatchObject({ expectedToolExecs: 60, totalToolExecs: 90 });
  });
});

describe("labels", () => {
  it.each([
    { rate: 0, want: "0%" },
    { rate: 0.05, want: "5%" },
    { rate: 0.2, want: "20%" },
  ])("labels fault rate $rate as $want", ({ rate, want }) => {
    expect(faultRateLabel(rate)).toBe(want);
  });

  it("describes a series in words for the accessible label", () => {
    const text = describeSeries(successSeries(compareFixture()), "%");
    expect(text).toContain("AgentFlight: 0% 100%, 20% 58.3%");
    expect(text).toContain("Baseline: 0% 75%, 20% 40.8%");
  });
});
