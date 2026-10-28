import { afterEach, describe, expect, it, vi } from "vitest";
import { cancelRun, replayRun, resumeRun } from "../api/client";
import { json, mockFetch, urlOf } from "./fetchMock";

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("run action client calls", () => {
  it.each([
    {
      name: "cancel",
      call: cancelRun,
      path: "/runs/r1/cancel",
      body: { id: "r1", status: "cancelling" },
    },
    {
      name: "resume",
      call: resumeRun,
      path: "/runs/r1/resume",
      body: { id: "r1", status: "running" },
    },
  ])("$name POSTs to its own path", async ({ call, path, body }) => {
    const fetchMock = mockFetch(() => json(body, 202));
    await expect(call("r1")).resolves.toEqual(body);
    expect(urlOf(fetchMock.mock.calls[0]?.[0] ?? "")).toBe(path);
    expect(fetchMock.mock.calls[0]?.[1]?.method).toBe("POST");
  });

  it("surfaces a 409 from cancel as an error", async () => {
    mockFetch(() => json({ error: "run is not active" }, 409));
    await expect(cancelRun("r1")).rejects.toMatchObject({
      status: 409,
      message: "run is not active",
    });
  });

  it("resolves a replay divergence (200)", async () => {
    mockFetch(() => json({ ok: false, mismatch_at_seq: 4, detail: "request differs" }));
    await expect(replayRun("r1")).resolves.toEqual({
      ok: false,
      mismatch_at_seq: 4,
      detail: "request differs",
    });
  });

  it("resolves a run that cannot be verified (422) rather than rejecting", async () => {
    mockFetch(() =>
      json({ ok: false, mismatch_at_seq: null, detail: "no run_finished event found" }, 422),
    );
    await expect(replayRun("r1")).resolves.toEqual({
      ok: false,
      mismatch_at_seq: null,
      detail: "no run_finished event found",
    });
  });

  it("rejects a replay still refused with 409", async () => {
    mockFetch(() => json({ error: "run is still active; replay a finished run" }, 409));
    await expect(replayRun("r1")).rejects.toMatchObject({ status: 409 });
  });
});
