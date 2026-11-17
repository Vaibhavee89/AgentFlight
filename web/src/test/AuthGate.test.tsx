import { afterEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { UNAUTHORIZED_EVENT } from "../api/client";
import { AuthGate } from "../components/AuthGate";
import { Header } from "../components/Header";
import { json, methodOf, mockFetch, urlOf } from "./fetchMock";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

interface Server {
  authRequired: boolean;
  authenticated: boolean;
  /** The token the fake server accepts. */
  token: string;
}

/** A fake server for the session, login and logout endpoints. */
function fakeServer(s: Server) {
  return mockFetch((url, init) => {
    if (url === "/session") {
      return json({ auth_required: s.authRequired, authenticated: s.authenticated });
    }
    if (url === "/login" && methodOf(init) === "POST") {
      const body = JSON.parse(typeof init?.body === "string" ? init.body : "{}") as {
        token?: string;
      };
      if (body.token === s.token) {
        s.authenticated = true;
        return new Response(null, { status: 204 });
      }
      return json({ error: "invalid token" }, 401);
    }
    if (url === "/logout") {
      s.authenticated = false;
      return new Response(null, { status: 204 });
    }
    if (url === "/healthz") return json({ status: "ok", active_runs: 0 });
    return json({ error: "not found" }, 404);
  });
}

function renderGate() {
  return render(
    <MemoryRouter>
      <AuthGate>
        <Header />
        <p>the dashboard</p>
      </AuthGate>
    </MemoryRouter>,
  );
}

describe("AuthGate", () => {
  it("shows the app straight away when the server has no authentication", async () => {
    fakeServer({ authRequired: false, authenticated: true, token: "t" });
    renderGate();
    expect(await screen.findByText("the dashboard")).toBeTruthy();
    expect(screen.queryByLabelText("API token")).toBeNull();
    // There is no session to end, so no sign-out button.
    expect(screen.queryByRole("button", { name: "Sign out" })).toBeNull();
  });

  it("shows the app, with a sign-out button, when already signed in", async () => {
    fakeServer({ authRequired: true, authenticated: true, token: "t" });
    renderGate();
    expect(await screen.findByText("the dashboard")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Sign out" })).toBeTruthy();
  });

  it("asks for a token and keeps the app hidden until it is accepted", async () => {
    fakeServer({ authRequired: true, authenticated: false, token: "right-token-123456" });
    renderGate();

    const field = await screen.findByLabelText("API token");
    expect(screen.queryByText("the dashboard")).toBeNull();
    expect(field.getAttribute("type")).toBe("password");
    // Nothing to submit yet.
    expect(screen.getByRole<HTMLButtonElement>("button", { name: "Sign in" }).disabled).toBe(true);

    fireEvent.change(field, { target: { value: "wrong-token" } });
    fireEvent.click(screen.getByRole("button", { name: "Sign in" }));
    expect((await screen.findByRole("alert")).textContent).toBe("invalid token");
    expect(screen.queryByText("the dashboard")).toBeNull();

    fireEvent.change(screen.getByLabelText("API token"), {
      target: { value: "right-token-123456" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Sign in" }));
    expect(await screen.findByText("the dashboard")).toBeTruthy();
  });

  it("returns to the login form on sign-out", async () => {
    fakeServer({ authRequired: true, authenticated: true, token: "t" });
    renderGate();
    fireEvent.click(await screen.findByRole("button", { name: "Sign out" }));
    expect(await screen.findByLabelText("API token")).toBeTruthy();
    expect(screen.queryByText("the dashboard")).toBeNull();
  });

  it("returns to the login form when the session ends while the app is open", async () => {
    fakeServer({ authRequired: true, authenticated: true, token: "t" });
    renderGate();
    await screen.findByText("the dashboard");

    act(() => {
      window.dispatchEvent(new Event(UNAUTHORIZED_EVENT));
    });
    expect(await screen.findByLabelText("API token")).toBeTruthy();
    expect(screen.queryByText("the dashboard")).toBeNull();
  });

  it("shows an error with a retry when the session check fails, then recovers", async () => {
    let up = false;
    mockFetch((url) => {
      if (url === "/session") {
        return up
          ? json({ auth_required: false, authenticated: true })
          : Promise.reject(new TypeError("down"));
      }
      return json({ status: "ok", active_runs: 0 });
    });
    renderGate();

    expect((await screen.findByRole("alert")).textContent).toMatch(/cannot reach/);
    expect(screen.queryByText("the dashboard")).toBeNull();

    up = true;
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    await waitFor(() => {
      expect(screen.getByText("the dashboard")).toBeTruthy();
    });
  });

  it("never sends the token anywhere but /login", async () => {
    const fetchMock = fakeServer({
      authRequired: true,
      authenticated: false,
      token: "right-token-123456",
    });
    renderGate();
    fireEvent.change(await screen.findByLabelText("API token"), {
      target: { value: "right-token-123456" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Sign in" }));
    await screen.findByText("the dashboard");

    for (const [input, init] of fetchMock.mock.calls) {
      const carriesToken =
        typeof init?.body === "string" && init.body.includes("right-token-123456");
      if (carriesToken) expect(urlOf(input)).toBe("/login");
    }
  });
});
