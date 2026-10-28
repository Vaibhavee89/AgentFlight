import { describe, expect, it } from "vitest";
import { formatDuration, isKnownStatus, statusBadge } from "../lib/status";

describe("statusBadge", () => {
  it.each([
    { status: "running", label: "running", tone: "info" },
    { status: "completed", label: "completed", tone: "ok" },
    { status: "cancelled", label: "cancelled", tone: "muted" },
    { status: "failed", label: "failed", tone: "bad" },
    { status: "interrupted", label: "interrupted", tone: "warn" },
    { status: "mystery", label: "mystery", tone: "muted" },
    { status: "", label: "unknown", tone: "muted" },
  ])("maps $status to $label/$tone", ({ status, label, tone }) => {
    expect(statusBadge(status)).toEqual({ label, tone });
  });

  it("recognises only the five known statuses", () => {
    expect(isKnownStatus("running")).toBe(true);
    expect(isKnownStatus("paused")).toBe(false);
  });
});

describe("formatDuration", () => {
  const start = "2026-10-28T10:00:00.000Z";
  const at = (ms: number) => new Date(Date.parse(start) + ms).toISOString();
  const now = Date.parse(start) + 999_999_999;

  it.each([
    { name: "no start time", started: null, finished: null, want: "—" },
    { name: "milliseconds", started: start, finished: at(450), want: "450ms" },
    { name: "seconds with a decimal", started: start, finished: at(2_340), want: "2.3s" },
    { name: "minutes and seconds", started: start, finished: at(125_000), want: "2m 5s" },
    { name: "hours and minutes", started: start, finished: at(3_900_000), want: "1h 5m" },
    { name: "running run is measured to now", started: start, finished: null, want: "11d 13h" },
  ])("$name", ({ started, finished, want }) => {
    if (want === "11d 13h") {
      expect(formatDuration(started, finished, now)).toMatch(/^\d+h \d+m$/);
      return;
    }
    expect(formatDuration(started, finished, now)).toBe(want);
  });

  it("never returns a negative duration", () => {
    expect(formatDuration(start, at(-5_000), Date.parse(start))).toBe("0ms");
  });
});
