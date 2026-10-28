import { useState } from "react";
import type { AvailableActions } from "../lib/runActions";

export type PendingAction = "cancel" | "resume" | "replay" | null;

interface RunActionsProps {
  actions: AvailableActions;
  /** The action currently in flight, if any. Every control is disabled meanwhile. */
  pending: PendingAction;
  onCancel: () => void;
  onResume: () => void;
  onVerify: () => void;
}

type Confirming = "cancel" | "resume" | null;

/**
 * Cancel and resume ask for confirmation first, since they change what the
 * agent does next. Verify replay runs at once: it only reads.
 */
export function RunActions({ actions, pending, onCancel, onResume, onVerify }: RunActionsProps) {
  const [confirming, setConfirming] = useState<Confirming>(null);
  const busy = pending !== null;

  if (confirming !== null) {
    const isCancel = confirming === "cancel";
    return (
      <div
        className="confirm"
        role="group"
        aria-label={isCancel ? "Confirm cancel" : "Confirm resume"}
      >
        <span>
          {isCancel
            ? "Cancel this run? It stops at the next step boundary."
            : "Resume this run from its last checkpoint?"}
        </span>
        <button
          type="button"
          className={`button ${isCancel ? "button-danger" : "button-primary"}`}
          onClick={() => {
            setConfirming(null);
            if (isCancel) onCancel();
            else onResume();
          }}
        >
          {isCancel ? "Yes, cancel" : "Yes, resume"}
        </button>
        <button
          type="button"
          className="button"
          onClick={() => {
            setConfirming(null);
          }}
        >
          Back
        </button>
      </div>
    );
  }

  // A pending action keeps its button on screen, disabled, even when the
  // optimistic status has already moved past the state that offered it.
  const showCancel = actions.cancel || pending === "cancel";
  const showResume = actions.resume || pending === "resume";
  const showReplay = actions.replay || pending === "replay";

  return (
    <div className="run-actions">
      {showCancel && (
        <button
          type="button"
          className="button button-danger"
          disabled={busy}
          onClick={() => {
            setConfirming("cancel");
          }}
        >
          {pending === "cancel" ? "Cancelling…" : "Cancel run"}
        </button>
      )}
      {showResume && (
        <button
          type="button"
          className="button button-primary"
          disabled={busy}
          onClick={() => {
            setConfirming("resume");
          }}
        >
          {pending === "resume" ? "Resuming…" : "Resume run"}
        </button>
      )}
      {showReplay && (
        <button type="button" className="button" disabled={busy} onClick={onVerify}>
          {pending === "replay" ? "Verifying…" : "Verify replay"}
        </button>
      )}
    </div>
  );
}
