import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { App } from "../App";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

function renderAt(path: string): void {
  vi.stubGlobal(
    "fetch",
    vi.fn<typeof fetch>(() => new Promise<Response>(() => undefined)),
  );
  render(
    <MemoryRouter initialEntries={[path]}>
      <App />
    </MemoryRouter>,
  );
}

describe("App routes", () => {
  it.each([
    { path: "/", heading: "Runs" },
    { path: "/runs", heading: "Runs" },
    { path: "/benchmarks", heading: "Benchmarks" },
    { path: "/no-such-page", heading: "Not found" },
  ])("$path renders $heading", ({ path, heading }) => {
    renderAt(path);
    expect(screen.getByRole("heading", { level: 1, name: heading })).toBeTruthy();
  });

  it("links to both top-level pages in the header", () => {
    renderAt("/runs");
    expect(screen.getByRole("link", { name: "Runs" }).getAttribute("href")).toBe("/runs");
    expect(screen.getByRole("link", { name: "Benchmarks" }).getAttribute("href")).toBe(
      "/benchmarks",
    );
  });
});
