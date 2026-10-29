import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { BenchmarksPage } from "../pages/BenchmarksPage";
import { GENERATE_COMMAND } from "../components/MissingResults";
import { json, mockFetch } from "./fetchMock";
import { compareFixture } from "./fixtures";

class StubResizeObserver {
  observe() {
    /* charts size themselves in the browser; jsdom has no layout */
  }
  unobserve() {
    /* nothing to release */
  }
  disconnect() {
    /* nothing to release */
  }
}

beforeEach(() => {
  vi.stubGlobal("ResizeObserver", StubResizeObserver);
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

function renderPage() {
  return render(
    <MemoryRouter>
      <BenchmarksPage />
    </MemoryRouter>,
  );
}

describe("BenchmarksPage", () => {
  it("explains how to generate the file when there are no results", async () => {
    mockFetch(() => json({ error: "compare.json not found in docs" }, 404));
    renderPage();
    expect(await screen.findByText(/No benchmark results have been generated yet/)).toBeTruthy();
    expect(screen.getByText(GENERATE_COMMAND)).toBeTruthy();
    expect(GENERATE_COMMAND).toContain("--json docs/compare.json");
  });

  it("shows an error for a failed load that is not a missing file", async () => {
    mockFetch(() => json({ error: "compare.json has schema_version 9" }, 500));
    renderPage();
    expect((await screen.findByRole("alert")).textContent).toContain(
      "compare.json has schema_version 9",
    );
  });

  it("shows the run metadata from the document", async () => {
    mockFetch(() => json(compareFixture()));
    renderPage();
    expect(await screen.findByText("benchmarks/basic.jsonl")).toBeTruthy();
    expect(screen.getByText("abcdef1")).toBeTruthy();
    expect(screen.getByText("Repeats")).toBeTruthy();
    expect(screen.getByText("10")).toBeTruthy();
  });

  it("labels each chart with its data, so a screen reader gets the values", async () => {
    mockFetch(() => json(compareFixture()));
    renderPage();
    const success = await screen.findByRole("img", { name: /Success rate by fault rate/ });
    expect(success.getAttribute("aria-label")).toContain("AgentFlight: 0% 100%, 20% 58.3%");
    const latency = screen.getByRole("img", {
      name: /p95 latency in milliseconds by fault rate, linear scale/,
    });
    expect(latency.getAttribute("aria-label")).toContain("Baseline:");
  });

  it("switches the latency chart to a log scale", async () => {
    mockFetch(() => json(compareFixture()));
    renderPage();
    const toggle = await screen.findByRole("checkbox", { name: /Log scale/ });
    expect((toggle as HTMLInputElement).checked).toBe(false);
    fireEvent.click(toggle);
    expect((toggle as HTMLInputElement).checked).toBe(true);
    expect(screen.getByRole("img", { name: /log scale/ })).toBeTruthy();
  });

  it("explains the duplicates in a tooltip linked to the column header", async () => {
    mockFetch(() => json(compareFixture()));
    renderPage();
    const table = within(await screen.findByRole("table"));
    const trigger = table.getByRole("button", { name: "About: duplicate tool executions" });
    const tipId = trigger.getAttribute("aria-describedby");
    expect(tipId).not.toBeNull();
    const tip = document.getElementById(tipId ?? "");
    expect(tip?.getAttribute("role")).toBe("tooltip");
    expect(tip?.textContent).toContain("policy retries");
  });

  it("reports the kill and resume results per runtime", async () => {
    mockFetch(() => json(compareFixture()));
    renderPage();
    await waitFor(() => {
      expect(
        screen.getByRole("progressbar", { name: /AgentFlight recovered 30 of 30/ }),
      ).toBeTruthy();
    });
    expect(screen.getByRole("progressbar", { name: /Baseline recovered 20 of 30/ })).toBeTruthy();
    expect(screen.getByText(/30 duplicate tool executions/)).toBeTruthy();
  });
});
