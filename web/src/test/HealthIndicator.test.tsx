import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import { HealthIndicator } from "../components/HealthIndicator";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe("HealthIndicator", () => {
  it.each([
    {
      name: "shows ok with the active run count",
      response: () =>
        new Response(JSON.stringify({ status: "ok", active_runs: 3 }), { status: 200 }),
      want: "API: ok · 3 active",
    },
    {
      name: "shows unreachable when the request fails",
      response: () => Promise.reject(new TypeError("network down")),
      want: "API: unreachable",
    },
  ])("$name", async ({ response, want }) => {
    vi.stubGlobal(
      "fetch",
      vi.fn<typeof fetch>().mockImplementation(() => {
        const r = response();
        return r instanceof Promise ? r : Promise.resolve(r);
      }),
    );
    render(<HealthIndicator />);
    await waitFor(() => {
      expect(screen.getByText(want)).toBeTruthy();
    });
  });

  it("starts in the checking state before the first response", () => {
    vi.stubGlobal(
      "fetch",
      vi.fn<typeof fetch>(() => new Promise<Response>(() => undefined)),
    );
    render(<HealthIndicator />);
    expect(screen.getByText("API: checking…")).toBeTruthy();
  });
});
