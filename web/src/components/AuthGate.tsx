import { useCallback, useEffect, useId, useMemo, useState, type ReactNode } from "react";
import { UNAUTHORIZED_EVENT, getSession, login, logout } from "../api/client";
import { AuthContext } from "../hooks/useAuth";

type GateState =
  | { kind: "checking" }
  | { kind: "error"; message: string }
  | { kind: "login" }
  | { kind: "open"; authRequired: boolean };

interface AuthGateProps {
  children: ReactNode;
}

/**
 * Shows the app only once the server is satisfied with this browser. When the
 * server has no authentication it renders the app straight away. Otherwise it
 * asks the server whether a session exists and shows a login form if not, and
 * again if the session ends later.
 */
export function AuthGate({ children }: AuthGateProps) {
  const [state, setState] = useState<GateState>({ kind: "checking" });
  const [attempt, setAttempt] = useState(0);

  useEffect(() => {
    const controller = new AbortController();
    setState({ kind: "checking" });
    getSession(controller.signal)
      .then((session) => {
        setState(
          !session.auth_required || session.authenticated
            ? { kind: "open", authRequired: session.auth_required }
            : { kind: "login" },
        );
      })
      .catch((err: unknown) => {
        if (controller.signal.aborted) return;
        setState({
          kind: "error",
          message: err instanceof Error ? err.message : "could not reach the server",
        });
      });
    return () => {
      controller.abort();
    };
  }, [attempt]);

  useEffect(() => {
    const onUnauthorized = () => {
      setState((prev) => (prev.kind === "open" ? { kind: "login" } : prev));
    };
    window.addEventListener(UNAUTHORIZED_EVENT, onUnauthorized);
    return () => {
      window.removeEventListener(UNAUTHORIZED_EVENT, onUnauthorized);
    };
  }, []);

  const signOut = useCallback(() => {
    // Whether or not the server heard us, this browser stops showing data.
    void logout()
      .catch(() => undefined)
      .then(() => {
        setState({ kind: "login" });
      });
  }, []);

  const authRequired = state.kind === "open" && state.authRequired;
  const value = useMemo(() => ({ authRequired, signOut }), [authRequired, signOut]);

  switch (state.kind) {
    case "checking":
      return (
        <main className="login">
          <p className="muted">Loading…</p>
        </main>
      );
    case "error":
      return (
        <main className="login">
          <h1>AgentFlight</h1>
          <p className="form-error" role="alert">
            {state.message}
          </p>
          <button
            type="button"
            className="button"
            onClick={() => {
              setAttempt((n) => n + 1);
            }}
          >
            Retry
          </button>
        </main>
      );
    case "login":
      return (
        <LoginForm
          onSuccess={() => {
            setState({ kind: "open", authRequired: true });
          }}
        />
      );
    case "open":
      return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
  }
}

interface LoginFormProps {
  onSuccess: () => void;
}

function LoginForm({ onSuccess }: LoginFormProps) {
  const fieldId = useId();
  const [token, setToken] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const canSubmit = token.trim().length > 0 && !submitting;

  const onSubmit = (event: { preventDefault: () => void }) => {
    event.preventDefault();
    if (!canSubmit) return;
    setSubmitting(true);
    setError(null);
    void login(token.trim())
      .then(onSuccess)
      .catch((err: unknown) => {
        setError(err instanceof Error ? err.message : "could not sign in");
        setSubmitting(false);
      });
  };

  return (
    <main className="login">
      <h1>AgentFlight</h1>
      <form onSubmit={onSubmit}>
        <label htmlFor={fieldId}>API token</label>
        <input
          id={fieldId}
          type="password"
          autoComplete="off"
          autoFocus
          value={token}
          onChange={(event) => {
            setToken(event.target.value);
          }}
          disabled={submitting}
        />
        {error !== null && (
          <p className="form-error" role="alert">
            {error}
          </p>
        )}
        <button type="submit" className="button button-primary" disabled={!canSubmit}>
          {submitting ? "Signing in…" : "Sign in"}
        </button>
      </form>
    </main>
  );
}
