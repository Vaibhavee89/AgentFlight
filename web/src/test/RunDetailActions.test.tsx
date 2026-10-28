import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { RunDetailPage } from "../pages/RunDetailPage";
import { json, methodOf, mockFetch, urlOf } from "./fetchMock";

class StubEventSource {
  onmessage: unknown = null;
  onerror: unknown = null;
  close() {
    /* no stream in these tests */
  }
}

const finishedEvents = [
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
    type: "model_response",
    version: 1,
    time: "2026-10-28T10:00:02Z",
    payload: { Content: "done", Usage: { InputTokens: 1, OutputTokens: 1 } },
  },
  {
    seq: 4,
    type: "run_finished",
    version: 1,
    time: "2026-10-28T10:00:03Z",
    payload: { Status: "completed", Steps: 1 },
  },
];

let scrollSpy = vi.fn();

beforeEach(() => {
  vi.stubGlobal("EventSource", StubEventSource);
  scrollSpy = vi.fn();
  Element.prototype.scrollIntoView = scrollSpy;
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

function renderRun() {
  return render(
    <MemoryRouter initialEntries={["/runs/run-1"]}>
      <Routes>
        <Route path="/runs/:id" element={<RunDetailPage />} />
      </Routes>
    </MemoryRouter>,
  );
}

/** A fake server whose run status can be changed between requests. */
function fakeServer(initial: { status: string; events?: unknown[] }) {
  const state = { status: initial.status, events: initial.events ?? finishedEvents };
  const fetchMock = mockFetch((url, init) => {
    const method = init?.method ?? "GET";
    if (url === "/runs/run-1" && method === "GET")
      return json({ id: "run-1", status: state.status });
    if (url.startsWith("/runs/run-1/events")) return json(state.events);
    if (url === "/runs/run-1/cancel") {
      state.status = "cancelled";
      return json({ id: "run-1", status: "cancelling" }, 202);
    }
    if (url === "/runs/run-1/resume") {
      state.status = "running";
      return json({ id: "run-1", status: "running" }, 202);
    }
    if (url === "/runs/run-1/replay")
      return json({
        ok: true,
        mismatch_at_seq: null,
        detail: "replay reproduced the recorded result",
      });
    return json({ error: `unexpected ${method} ${url}` }, 500);
  });
  return { state, fetchMock };
}

describe("RunDetailPage actions", () => {
  it.each([
    { status: "running", want: ["Cancel run"] },
    { status: "cancelled", want: ["Resume run", "Verify replay"] },
    { status: "interrupted", want: ["Resume run", "Verify replay"] },
    { status: "completed", want: ["Verify replay"] },
  ])("a $status run offers $want", async ({ status, want }) => {
    fakeServer({ status, events: status === "running" ? [] : finishedEvents });
    renderRun();
    const actions = await screen.findByRole("button", { name: want[0] ?? "" });
    expect(actions).toBeTruthy();
    const offered = screen.queryAllByRole("button").map((b) => b.textContent);
    for (const label of ["Cancel run", "Resume run", "Verify replay"]) {
      expect(offered.includes(label)).toBe(want.includes(label));
    }
  });

  it("asks before cancelling, then reconciles with the server", async () => {
    const { fetchMock } = fakeServer({ status: "running", events: [] });
    renderRun();
    fireEvent.click(await screen.findByRole("button", { name: "Cancel run" }));

    const confirm = screen.getByRole("group", { name: "Confirm cancel" });
    expect(within(confirm).getByText(/stops at the next step boundary/)).toBeTruthy();
    expect(fetchMock.mock.calls.some((c) => urlOf(c[0]).endsWith("/cancel"))).toBe(false);

    fireEvent.click(within(confirm).getByRole("button", { name: "Yes, cancel" }));

    await waitFor(() => {
      expect(screen.getByRole("button", { name: "Resume run" })).toBeTruthy();
    });
    expect(screen.queryByRole("button", { name: "Cancel run" })).toBeNull();
    expect(fetchMock.mock.calls.some((c) => urlOf(c[0]).endsWith("/cancel"))).toBe(true);
  });

  it("backs out of the confirmation without calling the server", async () => {
    const { fetchMock } = fakeServer({ status: "running", events: [] });
    renderRun();
    fireEvent.click(await screen.findByRole("button", { name: "Cancel run" }));
    fireEvent.click(screen.getByRole("button", { name: "Back" }));
    expect(screen.getByRole("button", { name: "Cancel run" })).toBeTruthy();
    expect(fetchMock.mock.calls.some((c) => urlOf(c[0]).endsWith("/cancel"))).toBe(false);
  });

  it("disables controls while a request is in flight", async () => {
    let release: (r: Response) => void = () => undefined;
    mockFetch((url, init) => {
      if (url === "/runs/run-1" && methodOf(init) === "GET")
        return json({ id: "run-1", status: "running" });
      if (url.startsWith("/runs/run-1/events")) return json([]);
      return new Promise<Response>((resolve) => {
        release = resolve;
      });
    });
    renderRun();
    fireEvent.click(await screen.findByRole("button", { name: "Cancel run" }));
    fireEvent.click(screen.getByRole("button", { name: "Yes, cancel" }));

    const busy = await screen.findByRole("button", { name: "Cancelling…" });
    expect((busy as HTMLButtonElement).disabled).toBe(true);
    release(json({ error: "run is not active" }, 409));
  });

  it("shows a failed action inline and keeps the run's status", async () => {
    mockFetch((url, init) => {
      if (url === "/runs/run-1" && methodOf(init) === "GET")
        return json({ id: "run-1", status: "running" });
      if (url.startsWith("/runs/run-1/events")) return json([]);
      return json({ error: "run is not active" }, 409);
    });
    renderRun();
    fireEvent.click(await screen.findByRole("button", { name: "Cancel run" }));
    fireEvent.click(screen.getByRole("button", { name: "Yes, cancel" }));

    expect((await screen.findByRole("alert")).textContent).toBe("run is not active");
    expect(screen.getByRole("button", { name: "Cancel run" })).toBeTruthy();
  });

  it("resumes a cancelled run after confirmation", async () => {
    fakeServer({ status: "cancelled" });
    renderRun();
    fireEvent.click(await screen.findByRole("button", { name: "Resume run" }));
    fireEvent.click(screen.getByRole("button", { name: "Yes, resume" }));
    await waitFor(() => {
      expect(screen.getByRole("button", { name: "Cancel run" })).toBeTruthy();
    });
  });

  it("verifies a replay and reports success", async () => {
    fakeServer({ status: "completed" });
    renderRun();
    fireEvent.click(await screen.findByRole("button", { name: "Verify replay" }));
    expect(await screen.findByText("Replay verified.")).toBeTruthy();
  });

  it("reports a replay divergence, scrolls to its event, and opens it", async () => {
    mockFetch((url, init) => {
      if (url === "/runs/run-1" && methodOf(init) === "GET")
        return json({ id: "run-1", status: "completed" });
      if (url.startsWith("/runs/run-1/events")) return json(finishedEvents);
      if (url === "/runs/run-1/replay")
        return json({ ok: false, mismatch_at_seq: 2, detail: "request differs at step 0" });
      return json({ error: "unexpected" }, 500);
    });
    renderRun();
    fireEvent.click(await screen.findByRole("button", { name: "Verify replay" }));

    expect(await screen.findByText("Replay diverged at event #2.")).toBeTruthy();
    await waitFor(() => {
      expect(scrollSpy).toHaveBeenCalled();
    });
    const row = document.getElementById("event-2");
    expect(row?.className).toContain("event-highlight");
    expect(row?.querySelector("button")?.getAttribute("aria-expanded")).toBe("true");
  });
});
