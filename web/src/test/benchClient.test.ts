import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiError, getBenchCompare } from "../api/client";
import { json, mockFetch } from "./fetchMock";
import { compareFixture } from "./fixtures";

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("getBenchCompare", () => {
  it("returns a valid version-1 document", async () => {
    mockFetch(() => json(compareFixture()));
    await expect(getBenchCompare()).resolves.toMatchObject({ seed: 1, repeats: 10 });
  });

  it("rejects an unknown schema version", async () => {
    mockFetch(() => json(compareFixture({ schema_version: 2 })));
    await expect(getBenchCompare()).rejects.toThrow("unsupported benchmark schema version 2");
  });

  it("rejects a row with the wrong field types", async () => {
    const bad = compareFixture();
    bad.rows[0] = { ...bad.rows[0], success_rate: "1" } as unknown as (typeof bad.rows)[number];
    mockFetch(() => json(bad));
    await expect(getBenchCompare()).rejects.toThrow("unexpected response from /bench/compare");
  });

  it("reports a missing file as a 404 carrying the server's message", async () => {
    mockFetch(() => json({ error: "compare.json not found in docs" }, 404));
    await expect(getBenchCompare()).rejects.toMatchObject({
      status: 404,
      message: "compare.json not found in docs",
    });
    await expect(getBenchCompare()).rejects.toBeInstanceOf(ApiError);
  });
});
