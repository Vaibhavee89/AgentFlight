import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiError, UNAUTHORIZED_EVENT, getSession, listRuns, login, logout } from "../api/client";
import { bodyOf, json, methodOf, mockFetch } from "./fetchMock";

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("getSession", () => {
  it("returns the session state", async () => {
    mockFetch(() => json({ auth_required: true, authenticated: false }));
    await expect(getSession()).resolves.toEqual({ auth_required: true, authenticated: false });
  });

  it.each([
    { name: "a missing field", body: { auth_required: true } },
    { name: "a wrong type", body: { auth_required: "yes", authenticated: false } },
    { name: "not an object", body: [] },
  ])("rejects $name", async ({ body }) => {
    mockFetch(() => json(body));
    await expect(getSession()).rejects.toMatchObject({ status: 0 });
  });
});

describe("login", () => {
  it("posts the token as JSON and resolves on 204", async () => {
    const fetchMock = mockFetch(() => new Response(null, { status: 204 }));
    await expect(login("secret-token")).resolves.toBeUndefined();

    const [url, init] = fetchMock.mock.calls[0] ?? [];
    expect(url).toBe("/login");
    expect(methodOf(init)).toBe("POST");
    expect(JSON.parse(bodyOf(init))).toEqual({ token: "secret-token" });
  });

  it.each([
    { status: 401, message: "invalid token" },
    { status: 429, message: "too many failed attempts; try again later" },
  ])("rejects with the server's message on $status", async ({ status, message }) => {
    mockFetch(() => json({ error: message }, status));
    const error = await login("x").then(
      () => null,
      (e: unknown) => e,
    );
    expect(error).toBeInstanceOf(ApiError);
    expect(error).toMatchObject({ status, message });
  });
});

describe("logout", () => {
  it("posts to /logout and resolves on 204", async () => {
    const fetchMock = mockFetch(() => new Response(null, { status: 204 }));
    await expect(logout()).resolves.toBeUndefined();
    const [url, init] = fetchMock.mock.calls[0] ?? [];
    expect(url).toBe("/logout");
    expect(methodOf(init)).toBe("POST");
  });
});

describe("the unauthorized event", () => {
  function listen(): { count: () => number; stop: () => void } {
    let n = 0;
    const handler = () => {
      n += 1;
    };
    window.addEventListener(UNAUTHORIZED_EVENT, handler);
    return {
      count: () => n,
      stop: () => {
        window.removeEventListener(UNAUTHORIZED_EVENT, handler);
      },
    };
  }

  it("fires when a data request is refused", async () => {
    mockFetch(() => json({ error: "authentication required" }, 401));
    const l = listen();
    await expect(listRuns()).rejects.toMatchObject({ status: 401 });
    l.stop();
    expect(l.count()).toBe(1);
  });

  it.each([
    { name: "a wrong token at login", call: () => login("bad") },
    { name: "the session check", call: () => getSession() },
  ])("does not fire for $name", async ({ call }) => {
    mockFetch(() => json({ error: "invalid token" }, 401));
    const l = listen();
    await call().catch(() => undefined);
    l.stop();
    expect(l.count()).toBe(0);
  });

  it("does not fire for other errors", async () => {
    mockFetch(() => json({ error: "nope" }, 500));
    const l = listen();
    await listRuns().catch(() => undefined);
    l.stop();
    expect(l.count()).toBe(0);
  });
});
