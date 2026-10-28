import { vi } from "vitest";

/** The URL a fetch call was made with, whatever form the caller passed. */
export function urlOf(input: RequestInfo | URL): string {
  if (typeof input === "string") return input;
  if (input instanceof URL) return input.href;
  return input.url;
}

/** The request body as text, or "" when none was sent. */
export function bodyOf(init: RequestInit | undefined): string {
  return typeof init?.body === "string" ? init.body : "";
}

export type FetchHandler = (
  url: string,
  init: RequestInit | undefined,
) => Response | Promise<Response>;

/** Replaces global fetch with a handler, and returns the mock for assertions. */
export function mockFetch(handler: FetchHandler) {
  const fn = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
    return Promise.resolve(handler(urlOf(input), init));
  });
  vi.stubGlobal("fetch", fn);
  return fn;
}

export function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

/** The HTTP method of a fetch call, defaulting to GET as fetch does. */
export function methodOf(init: RequestInit | undefined): string {
  return typeof init?.method === "string" ? init.method : "GET";
}
