import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { ReplayPanel } from "../components/ReplayPanel";
import type { ReplayState } from "../hooks/useReplay";

afterEach(() => {
  cleanup();
});

describe("ReplayPanel", () => {
  it("renders nothing before a replay has been requested", () => {
    const { container } = render(<ReplayPanel state={{ kind: "idle" }} onShowMismatch={vi.fn()} />);
    expect(container.textContent).toBe("");
  });

  it("shows a green verified result when the recording reproduces", () => {
    const state: ReplayState = {
      kind: "done",
      result: { ok: true, mismatch_at_seq: null, detail: "replay reproduced the recorded result" },
    };
    render(<ReplayPanel state={state} onShowMismatch={vi.fn()} />);

    const panel = screen.getByRole("status");
    expect(panel.className).toContain("replay-ok");
    expect(screen.getByText("Replay verified.")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Show event" })).toBeNull();
  });

  it("shows a red divergence with its seq, and asks to show that event", () => {
    const onShow = vi.fn();
    const state: ReplayState = {
      kind: "done",
      result: { ok: false, mismatch_at_seq: 6, detail: "request differs at step 1" },
    };
    render(<ReplayPanel state={state} onShowMismatch={onShow} />);

    const panel = screen.getByRole("alert");
    expect(panel.className).toContain("replay-bad");
    expect(screen.getByText("Replay diverged at event #6.")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Show event" }));
    expect(onShow).toHaveBeenCalledWith(6);
  });

  it("shows a red 'could not verify' when there is no event to point at", () => {
    const state: ReplayState = {
      kind: "done",
      result: { ok: false, mismatch_at_seq: null, detail: "no run_finished event found" },
    };
    render(<ReplayPanel state={state} onShowMismatch={vi.fn()} />);
    expect(screen.getByText("Replay could not verify this run.")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Show event" })).toBeNull();
  });

  it("shows a pending state while the replay runs", () => {
    render(<ReplayPanel state={{ kind: "running" }} onShowMismatch={vi.fn()} />);
    expect(screen.getByRole("status").textContent).toBe("Verifying replay…");
  });

  it("shows a request failure inline", () => {
    render(
      <ReplayPanel
        state={{ kind: "error", message: "run is still active" }}
        onShowMismatch={vi.fn()}
      />,
    );
    expect(screen.getByRole("alert").textContent).toContain("run is still active");
  });
});
