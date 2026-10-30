import { afterEach, describe, expect, it } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { Timeline } from "../components/Timeline";
import { nextIndex } from "../lib/keyboard";
import { buildTimeline } from "../lib/timeline";
import type { RecordedEvent } from "../api/types";

describe("nextIndex", () => {
  it.each([
    { name: "down from the first", current: 0, count: 3, key: "ArrowDown", want: 1 },
    { name: "down stops at the last", current: 2, count: 3, key: "ArrowDown", want: 2 },
    { name: "up from the last", current: 2, count: 3, key: "ArrowUp", want: 1 },
    { name: "up stops at the first", current: 0, count: 3, key: "ArrowUp", want: 0 },
    {
      name: "down with nothing focused goes to the first",
      current: -1,
      count: 3,
      key: "ArrowDown",
      want: 0,
    },
    {
      name: "up with nothing focused goes to the first",
      current: -1,
      count: 3,
      key: "ArrowUp",
      want: 0,
    },
    { name: "Home goes to the first", current: 2, count: 3, key: "Home", want: 0 },
    { name: "End goes to the last", current: 0, count: 3, key: "End", want: 2 },
    { name: "other keys do not move focus", current: 1, count: 3, key: "a", want: null },
    {
      name: "an empty list has nowhere to go",
      current: -1,
      count: 0,
      key: "ArrowDown",
      want: null,
    },
  ])("$name", ({ current, count, key, want }) => {
    expect(nextIndex(current, count, key)).toBe(want);
  });
});

afterEach(() => {
  cleanup();
});

describe("Timeline keyboard navigation", () => {
  it("moves focus between event rows with the arrow keys", () => {
    const events: RecordedEvent[] = [
      {
        seq: 1,
        type: "run_started",
        version: 1,
        time: "2026-10-30T10:00:00Z",
        payload: { prompt: "a" },
      },
      { seq: 2, type: "model_request", version: 1, time: "2026-10-30T10:00:01Z", payload: {} },
      { seq: 3, type: "model_response", version: 1, time: "2026-10-30T10:00:02Z", payload: {} },
    ];
    const { groups } = buildTimeline(events);
    render(<Timeline groups={groups} hidden={new Set()} />);

    const heads = Array.from(document.querySelectorAll<HTMLButtonElement>(".event-head"));
    expect(heads).toHaveLength(3);
    heads[0]?.focus();

    const region = screen.getByRole("region", { name: "Event timeline" });
    fireEvent.keyDown(region, { key: "ArrowDown" });
    expect(document.activeElement).toBe(heads[1]);
    fireEvent.keyDown(region, { key: "End" });
    expect(document.activeElement).toBe(heads[2]);
    fireEvent.keyDown(region, { key: "Home" });
    expect(document.activeElement).toBe(heads[0]);
  });
});
