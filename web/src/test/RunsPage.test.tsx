import { afterEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom";
import { RunsPage } from "../pages/RunsPage";
import { json, mockFetch, urlOf } from "./fetchMock";

afterEach(() => {
  cleanup();
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

const completed = {
  id: "run-done",
  status: "completed",
  started_at: "2026-10-28T10:00:00Z",
  finished_at: "2026-10-28T10:00:03Z",
  steps: 2,
  tool_calls: 1,
  retries: 0,
  total_tokens: 15,
};
const running = { ...completed, id: "run-live", status: "running", finished_at: null, steps: 1 };

function LocationProbe() {
  const location = useLocation();
  return <p data-testid="location">{location.pathname + location.search}</p>;
}

function renderRuns(initial = "/runs") {
  return render(
    <MemoryRouter initialEntries={[initial]}>
      <Routes>
        <Route path="/runs" element={<RunsPage />} />
        <Route path="/runs/:id" element={<p>detail page</p>} />
      </Routes>
      <LocationProbe />
    </MemoryRouter>,
  );
}

describe("RunsPage", () => {
  it("shows a loading state before the first response", () => {
    mockFetch(() => new Promise<Response>(() => undefined));
    renderRuns();
    expect(screen.getByText("Loading runs…")).toBeTruthy();
  });

  it("renders one row per run with its status and counts", async () => {
    mockFetch(() => json([completed, running]));
    renderRuns();
    expect(await screen.findByText("run-done")).toBeTruthy();
    expect(screen.getByText("run-live")).toBeTruthy();
    const table = within(screen.getByRole("table"));
    expect(table.getByText("completed").className).toBe("badge badge-ok");
    expect(table.getByText("running").className).toBe("badge badge-info");
    expect(table.getAllByText("15")).toHaveLength(2);
  });

  it("shows an empty state with a call to action", async () => {
    mockFetch(() => json([]));
    renderRuns();
    expect(await screen.findByText("No runs yet.")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Start a run" })).toBeTruthy();
  });

  it("shows an error with a retry that refetches", async () => {
    let calls = 0;
    mockFetch(() => {
      calls += 1;
      return calls === 1 ? json({ error: "database is locked" }, 500) : json([completed]);
    });
    renderRuns();
    expect(await screen.findByRole("alert")).toBeTruthy();
    expect(screen.getByText(/database is locked/)).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(await screen.findByText("run-done")).toBeTruthy();
  });

  it("filters by status through the query string", async () => {
    const fetchMock = mockFetch((url) =>
      url.includes("status=failed") ? json([]) : json([completed]),
    );
    renderRuns();
    await screen.findByText("run-done");

    fireEvent.change(screen.getByLabelText(/Status/), { target: { value: "failed" } });

    expect(await screen.findByText("No failed runs.")).toBeTruthy();
    expect(screen.getByTestId("location").textContent).toBe("/runs?status=failed");
    const urls = fetchMock.mock.calls.map((call) => urlOf(call[0]));
    expect(urls.at(-1)).toBe("/runs?limit=100&status=failed");
  });

  it("opens the run when its row is clicked", async () => {
    mockFetch(() => json([completed]));
    renderRuns();
    fireEvent.click(
      await screen
        .findByText("run-done", { selector: "td > a" })
        .then((el) => el.closest("tr") ?? el),
    );
    expect(await screen.findByText("detail page")).toBeTruthy();
  });

  it("polls every 2s while a run is executing and every 10s otherwise", async () => {
    vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout", "Date"] });
    const fetchMock = mockFetch(() => json([running]));
    renderRuns();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0);
    });
    expect(fetchMock).toHaveBeenCalledTimes(1);

    await act(async () => {
      await vi.advanceTimersByTimeAsync(2_000);
    });
    expect(fetchMock).toHaveBeenCalledTimes(2);

    fetchMock.mockImplementation(() => Promise.resolve(json([completed])));
    await act(async () => {
      await vi.advanceTimersByTimeAsync(2_000);
    });
    expect(fetchMock).toHaveBeenCalledTimes(3);

    await act(async () => {
      await vi.advanceTimersByTimeAsync(9_000);
    });
    expect(fetchMock).toHaveBeenCalledTimes(3);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1_000);
    });
    expect(fetchMock).toHaveBeenCalledTimes(4);
  });

  it("opens the new run dialog", async () => {
    mockFetch(() => json([]));
    renderRuns();
    fireEvent.click(await screen.findByRole("button", { name: "New run" }));
    await waitFor(() => {
      expect(screen.getByRole("dialog")).toBeTruthy();
    });
  });
});
