import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiError, getHealth } from "../api/client";

function stubFetch(response: Response): ReturnType<typeof vi.fn> {
  const fn = vi.fn<(input: RequestInfo | URL, init?: RequestInit) => Promise<Response>>();
  fn.mockResolvedValue(response);
  vi.stubGlobal("fetch", fn);
  return fn;
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("getHealth", () => {
  it.each([
    {
      name: "decodes a healthy response",
      body: { status: "ok", active_runs: 2 },
      want: { status: "ok", active_runs: 2 },
    },
  ])("$name", async ({ body, want }) => {
    const fetchMock = stubFetch(new Response(JSON.stringify(body), { status: 200 }));
    await expect(getHealth()).resolves.toEqual(want);
    expect(fetchMock).toHaveBeenCalledWith(
      "/healthz",
      expect.objectContaining({ headers: { Accept: "application/json" } }),
    );
  });

  it.each([
    { name: "rejects a non-object body", body: "nope" },
    { name: "rejects a missing active_runs", body: { status: "ok" } },
    { name: "rejects a non-numeric active_runs", body: { status: "ok", active_runs: "2" } },
  ])("$name", async ({ body }) => {
    stubFetch(new Response(JSON.stringify(body), { status: 200 }));
    await expect(getHealth()).rejects.toBeInstanceOf(ApiError);
  });

  it("surfaces the server's error message", async () => {
    stubFetch(new Response(JSON.stringify({ error: "run not found" }), { status: 404 }));
    await expect(getHealth()).rejects.toMatchObject({ status: 404, message: "run not found" });
  });

  it("falls back to the status text for non-JSON errors", async () => {
    stubFetch(new Response("<html>bad gateway</html>", { status: 502, statusText: "Bad Gateway" }));
    await expect(getHealth()).rejects.toMatchObject({ status: 502, message: "Bad Gateway" });
  });
});
