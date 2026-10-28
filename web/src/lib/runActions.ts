/** Statuses the server reports for a run that is no longer executing. */
const TERMINAL: ReadonlySet<string> = new Set(["completed", "cancelled", "failed", "interrupted"]);

export interface AvailableActions {
  cancel: boolean;
  resume: boolean;
  replay: boolean;
}

/**
 * Which controls a run offers, from its status alone. Cancel needs a running
 * run. Resume is for runs that stopped early and kept a checkpoint. Replay needs
 * a run that is no longer executing; the server refuses an active one with 409.
 * Requests in flight are shown by disabling controls, not by hiding them.
 */
export function availableActions(status: string | null): AvailableActions {
  if (status === null) return { cancel: false, resume: false, replay: false };
  return {
    cancel: status === "running",
    resume: status === "cancelled" || status === "interrupted",
    replay: TERMINAL.has(status),
  };
}
