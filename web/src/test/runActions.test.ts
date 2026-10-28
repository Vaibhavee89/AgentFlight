import { describe, expect, it } from "vitest";
import { availableActions } from "../lib/runActions";

describe("availableActions", () => {
  it.each([
    { status: "running", cancel: true, resume: false, replay: false },
    { status: "completed", cancel: false, resume: false, replay: true },
    { status: "failed", cancel: false, resume: false, replay: true },
    { status: "cancelled", cancel: false, resume: true, replay: true },
    { status: "interrupted", cancel: false, resume: true, replay: true },
    { status: "cancelling", cancel: false, resume: false, replay: false },
    { status: null, cancel: false, resume: false, replay: false },
  ])("status $status offers cancel=$cancel resume=$resume replay=$replay", (c) => {
    expect(availableActions(c.status)).toEqual({
      cancel: c.cancel,
      resume: c.resume,
      replay: c.replay,
    });
  });
});
