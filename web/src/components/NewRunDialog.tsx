import { useEffect, useId, useState } from "react";
import { useNavigate } from "react-router-dom";
import { startRun } from "../api/client";

interface NewRunDialogProps {
  onClose: () => void;
}

/** Starts a run from a prompt and opens its detail page. */
export function NewRunDialog({ onClose }: NewRunDialogProps) {
  const navigate = useNavigate();
  const titleId = useId();
  const fieldId = useId();
  const [prompt, setPrompt] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") onClose();
    };
    window.addEventListener("keydown", onKey);
    return () => {
      window.removeEventListener("keydown", onKey);
    };
  }, [onClose]);

  const trimmed = prompt.trim();
  const canSubmit = trimmed.length > 0 && !submitting;

  const onSubmit = (event: { preventDefault: () => void }) => {
    event.preventDefault();
    if (!canSubmit) return;
    setSubmitting(true);
    setError(null);
    void startRun(trimmed)
      .then((run) => {
        void navigate(`/runs/${encodeURIComponent(run.id)}`);
      })
      .catch((err: unknown) => {
        setError(err instanceof Error ? err.message : "could not start the run");
        setSubmitting(false);
      });
  };

  return (
    <div className="overlay" onClick={onClose}>
      <div
        className="dialog"
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        onClick={(event) => {
          event.stopPropagation();
        }}
      >
        <h2 id={titleId}>New run</h2>
        <form onSubmit={onSubmit}>
          <label htmlFor={fieldId}>Prompt</label>
          <textarea
            id={fieldId}
            value={prompt}
            onChange={(event) => {
              setPrompt(event.target.value);
            }}
            rows={5}
            placeholder="What should the agent do?"
            disabled={submitting}
          />
          {error !== null && (
            <p className="form-error" role="alert">
              {error}
            </p>
          )}
          <div className="dialog-actions">
            <button type="button" className="button" onClick={onClose} disabled={submitting}>
              Cancel
            </button>
            <button type="submit" className="button button-primary" disabled={!canSubmit}>
              {submitting ? "Starting…" : "Start run"}
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}
