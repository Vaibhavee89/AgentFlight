import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiError, listRuns, startRun } from "../api/client";
import { bodyOf, json, mockFetch } from "./fetchMock";

afterEach(() => {
  vi.unstubAllGlobals();
});

const run = {
  id: "abc",
  status: "completed",
  started_at: "2026-10-28T10:00:00Z",
  finished_at: "2026-10-28T10:00:02Z",
  steps: 2,
  tool_calls: 1,
  retries: 0,
  total_tokens: 15,
};

describe("listRuns", () => {
  it.each([
    { name: "no query", query: {}, want: "/runs" },
    { name: "limit only", query: { limit: 25 }, want: "/runs?limit=25" },
    { name: "status only", query: { status: "failed" }, want: "/runs?status=failed" },
    {
      name: "both",
      query: { limit: 10, status: "running" },
      want: "/runs?limit=10&status=running",
    },
    { name: "empty status is omitted", query: { status: "" }, want: "/runs" },
  ])("builds the URL for $name", async ({ query, want }) => {
    const fetchMock = mockFetch(() => json([]));
    await listRuns(query);
    expect(fetchMock.mock.calls[0]?.[0]).toBe(want);
  });

  it("decodes items, including null timestamps", async () => {
    mockFetch(() => json([{ ...run, started_at: null, finished_at: null }]));
    const runs = await listRuns();
    expect(runs).toEqual([{ ...run, started_at: null, finished_at: null }]);
  });

  it("rejects a non-array body", async () => {
    mockFetch(() => json({ runs: [] }));
    await expect(listRuns()).rejects.toBeInstanceOf(ApiError);
  });

  it("rejects an item with the wrong field types", async () => {
    mockFetch(() => json([{ ...run, steps: "2" }]));
    await expect(listRuns()).rejects.toThrow("unexpected response from /runs");
  });

  it("maps a 400 to the server's message", async () => {
    mockFetch(() => json({ error: "limit must be an integer from 1 to 200" }, 400));
    await expect(listRuns({ limit: 0 })).rejects.toMatchObject({
      status: 400,
      message: "limit must be an integer from 1 to 200",
    });
  });

  it("reports an unreachable API as a status-0 ApiError", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(() => Promise.reject(new TypeError("Failed to fetch"))),
    );
    await expect(listRuns()).rejects.toMatchObject({
      status: 0,
      message: "cannot reach the AgentFlight API",
    });
  });
});

describe("startRun", () => {
  it("posts the prompt as JSON and returns the run view", async () => {
    const fetchMock = mockFetch(() => json({ id: "new1", status: "running" }, 202));
    const view = await startRun("hello");
    expect(view).toEqual({ id: "new1", status: "running" });

    const [url, init] = fetchMock.mock.calls[0] ?? [];
    expect(url).toBe("/runs");
    expect(init?.method).toBe("POST");
    expect(init?.headers).toMatchObject({ "Content-Type": "application/json" });
    expect(JSON.parse(bodyOf(init))).toEqual({ prompt: "hello" });
  });

  it("rejects a response without an id", async () => {
    mockFetch(() => json({ status: "running" }, 202));
    await expect(startRun("hi")).rejects.toThrow("unexpected response from /runs");
  });
});
