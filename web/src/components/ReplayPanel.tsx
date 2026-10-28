import type { ReplayState } from "../hooks/useReplay";

interface ReplayPanelProps {
  state: ReplayState;
  onShowMismatch: (seq: number) => void;
}

/** The outcome of replay verification: green when the recording reproduces, red otherwise. */
export function ReplayPanel({ state, onShowMismatch }: ReplayPanelProps) {
  if (state.kind === "idle") return null;

  if (state.kind === "running") {
    return (
      <div className="replay-panel replay-pending" role="status">
        Verifying replay…
      </div>
    );
  }

  if (state.kind === "error") {
    return (
      <div className="replay-panel replay-bad" role="alert">
        <strong>Replay could not run.</strong> {state.message}
      </div>
    );
  }

  const { result } = state;
  if (result.ok) {
    return (
      <div className="replay-panel replay-ok" role="status">
        <strong>Replay verified.</strong> The recorded run reproduces identically.
        <span className="replay-detail">{result.detail}</span>
      </div>
    );
  }

  const seq = result.mismatch_at_seq;
  return (
    <div className="replay-panel replay-bad" role="alert">
      {seq === null ? (
        <strong>Replay could not verify this run.</strong>
      ) : (
        <strong>Replay diverged at event #{String(seq)}.</strong>
      )}
      <span className="replay-detail">{result.detail}</span>
      {seq !== null && (
        <button
          type="button"
          className="button button-small"
          onClick={() => {
            onShowMismatch(seq);
          }}
        >
          Show event
        </button>
      )}
    </div>
  );
}
