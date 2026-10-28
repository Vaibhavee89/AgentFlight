import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { RunDetailPage } from "../pages/RunDetailPage";
import { json, mockFetch } from "./fetchMock";

class StubEventSource {
  close() {
    /* nothing to close in these tests */
  }
  onmessage: unknown = null;
  onerror: unknown = null;
}

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const events = [
  {
    seq: 1,
    type: "run_started",
    version: 1,
    time: "2026-10-28T10:00:00Z",
    payload: { prompt: "go" },
  },
  {
    seq: 2,
    type: "model_request",
    version: 1,
    time: "2026-10-28T10:00:01Z",
    payload: { Prompt: "go", ToolResults: null },
  },
  {
    seq: 3,
    type: "policy_decision",
    version: 1,
    time: "2026-10-28T10:00:02Z",
    payload: {
      tool: "echo",
      allowed: true,
      retry: true,
      attempt: 1,
      backoff_ms: 40,
      reason: "timed out",
    },
  },
  {
    seq: 4,
    type: "run_finished",
    version: 1,
    time: "2026-10-28T10:00:03Z",
    payload: { Status: "completed", Steps: 1 },
  },
];

function renderDetail(path: string) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <Routes>
        <Route path="/runs/:id" element={<RunDetailPage />} />
      </Routes>
    </MemoryRouter>,
  );
}

describe("RunDetailPage", () => {
  it("shows the header, the step sections, and the retry", async () => {
    vi.stubGlobal("EventSource", StubEventSource);
    mockFetch((url) => {
      if (url === "/runs/run-1") return json({ id: "run-1", status: "completed" });
      return json(events);
    });
    renderDetail("/runs/run-1");

    expect(await screen.findByRole("heading", { level: 1, name: "Run" })).toBeTruthy();
    expect(screen.getByText("run-1")).toBeTruthy();
    expect(screen.getByRole("region", { name: "Step 1" })).toBeTruthy();
    const timeline = within(screen.getByRole("region", { name: "Step 1" }));
    expect(timeline.getByText("retry")).toBeTruthy();
    expect(timeline.getByText("attempt 1 failed, retrying in 40ms — timed out")).toBeTruthy();
  });

  it("hides an event type when its chip is toggled off", async () => {
    vi.stubGlobal("EventSource", StubEventSource);
    mockFetch((url) => {
      if (url === "/runs/run-1") return json({ id: "run-1", status: "completed" });
      return json(events);
    });
    renderDetail("/runs/run-1");
    await screen.findByRole("region", { name: "Step 1" });

    const chip = screen.getByRole("button", { name: /policy/ });
    expect(chip.getAttribute("aria-pressed")).toBe("true");
    fireEvent.click(chip);
    expect(chip.getAttribute("aria-pressed")).toBe("false");
    expect(screen.queryByText("attempt 1 failed, retrying in 40ms — timed out")).toBeNull();
  });

  it("expands an event to show its pretty-printed payload", async () => {
    vi.stubGlobal("EventSource", StubEventSource);
    mockFetch((url) => {
      if (url === "/runs/run-1") return json({ id: "run-1", status: "completed" });
      return json(events);
    });
    renderDetail("/runs/run-1");
    await screen.findByRole("region", { name: "Step 1" });

    const row = screen.getByRole("button", { name: /attempt 1 failed/ });
    expect(row.getAttribute("aria-expanded")).toBe("false");
    fireEvent.click(row);
    expect(row.getAttribute("aria-expanded")).toBe("true");
    expect(screen.getByText(/"backoff_ms": 40/)).toBeTruthy();
  });

  it("shows a not-found notice for an unknown run", async () => {
    vi.stubGlobal("EventSource", StubEventSource);
    mockFetch(() => json({ error: "run not found" }, 404));
    renderDetail("/runs/missing");
    expect(await screen.findByText(/No run with id/)).toBeTruthy();
  });

  it("waits for the first load before rendering", async () => {
    vi.stubGlobal("EventSource", StubEventSource);
    mockFetch(() => new Promise<Response>(() => undefined));
    renderDetail("/runs/run-1");
    await waitFor(() => {
      expect(screen.getByText("Loading run…")).toBeTruthy();
    });
  });
});
