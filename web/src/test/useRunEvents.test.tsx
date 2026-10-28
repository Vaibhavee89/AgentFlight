import { afterEach, describe, expect, it, vi } from "vitest";
import { renderHook, waitFor, act } from "@testing-library/react";
import { useRunEvents } from "../hooks/useRunEvents";
import type { RecordedEvent } from "../api/types";
import { json, mockFetch, urlOf } from "./fetchMock";

class FakeEventSource {
  static instances: FakeEventSource[] = [];
  readonly url: string;
  closed = false;
  onmessage: ((event: MessageEvent<unknown>) => void) | null = null;
  onerror: ((event: Event) => void) | null = null;

  constructor(url: string) {
    this.url = url;
    FakeEventSource.instances.push(this);
  }

  close() {
    this.closed = true;
  }

  emit(event: RecordedEvent) {
    this.onmessage?.(new MessageEvent("message", { data: JSON.stringify(event) }));
  }

  fail() {
    this.onerror?.(new Event("error"));
  }
}

function evt(seq: number, type: string, payload: unknown = {}): RecordedEvent {
  const second = String(seq % 60).padStart(2, "0");
  return { seq, type, version: 1, time: `2026-10-28T10:00:${second}Z`, payload };
}

afterEach(() => {
  FakeEventSource.instances = [];
  vi.unstubAllGlobals();
});

function setup(runStatus: () => string) {
  vi.stubGlobal("EventSource", FakeEventSource);
  return mockFetch((url) => {
    if (url === "/runs/run-1") return json({ id: "run-1", status: runStatus() });
    if (url.startsWith("/runs/run-1/events")) return json([]);
    return json({ error: "unexpected" }, 500);
  });
}

describe("useRunEvents", () => {
  it("orders events by seq and drops duplicates", async () => {
    setup(() => "running");
    const { result } = renderHook(() => useRunEvents("run-1"));
    await waitFor(() => {
      expect(FakeEventSource.instances).toHaveLength(1);
    });
    const source = FakeEventSource.instances[0];
    expect(source?.url).toBe("/runs/run-1/stream");

    act(() => {
      source?.emit(evt(3, "tool_result"));
      source?.emit(evt(1, "run_started", { prompt: "go" }));
      source?.emit(evt(2, "model_request"));
      source?.emit(evt(2, "model_request"));
    });

    expect(result.current.events.map((e) => e.seq)).toEqual([1, 2, 3]);
    expect(result.current.live).toBe(true);
  });

  it("keeps one stream across a reconnect and ignores redelivered events", async () => {
    setup(() => "running");
    const { result } = renderHook(() => useRunEvents("run-1"));
    await waitFor(() => {
      expect(FakeEventSource.instances).toHaveLength(1);
    });
    const source = FakeEventSource.instances[0];

    act(() => {
      source?.emit(evt(1, "run_started"));
      source?.emit(evt(2, "model_request"));
    });
    act(() => {
      source?.fail();
    });
    await waitFor(() => {
      expect(result.current.runStatus).toBe("running");
    });

    expect(FakeEventSource.instances).toHaveLength(1);
    expect(source?.closed).toBe(false);

    act(() => {
      source?.emit(evt(2, "model_request"));
      source?.emit(evt(3, "model_response"));
    });
    expect(result.current.events.map((e) => e.seq)).toEqual([1, 2, 3]);
  });

  it("closes the stream when run_finished arrives and records the final status", async () => {
    setup(() => "running");
    const { result } = renderHook(() => useRunEvents("run-1"));
    await waitFor(() => {
      expect(FakeEventSource.instances).toHaveLength(1);
    });
    const source = FakeEventSource.instances[0];

    act(() => {
      source?.emit(evt(1, "run_started"));
      source?.emit(evt(2, "run_finished", { Status: "completed" }));
    });

    expect(source?.closed).toBe(true);
    expect(result.current.live).toBe(false);
    expect(result.current.runStatus).toBe("completed");
    expect(result.current.events.map((e) => e.type)).toEqual(["run_started", "run_finished"]);
  });

  it("loads a finished run through the paginated endpoint, without opening a stream", async () => {
    const page1 = Array.from({ length: 1000 }, (_, i) => evt(i + 1, "tool_call"));
    const page2 = [evt(1001, "run_finished", { Status: "completed" }), evt(1002, "checkpoint")];
    vi.stubGlobal("EventSource", FakeEventSource);
    const fetchMock = mockFetch((url) => {
      if (url === "/runs/run-1") return json({ id: "run-1", status: "completed" });
      if (url.includes("after=0")) return json(page1);
      if (url.includes("after=1000")) return json(page2);
      return json({ error: "unexpected" }, 500);
    });

    const { result } = renderHook(() => useRunEvents("run-1"));
    await waitFor(() => {
      expect(result.current.loading).toBe(false);
    });

    expect(FakeEventSource.instances).toHaveLength(0);
    expect(result.current.events).toHaveLength(1002);
    expect(result.current.events.at(-1)?.seq).toBe(1002);
    expect(result.current.runStatus).toBe("completed");
    const urls = fetchMock.mock.calls.map((call) => urlOf(call[0]));
    expect(urls).toContain("/runs/run-1/events?after=0&limit=1000");
    expect(urls).toContain("/runs/run-1/events?after=1000&limit=1000");
  });

  it("stops reconnecting when the run was interrupted", async () => {
    let status = "running";
    setup(() => status);
    const { result } = renderHook(() => useRunEvents("run-1"));
    await waitFor(() => {
      expect(FakeEventSource.instances).toHaveLength(1);
    });
    const source = FakeEventSource.instances[0];
    act(() => {
      source?.emit(evt(1, "run_started"));
    });

    status = "interrupted";
    act(() => {
      source?.fail();
    });

    await waitFor(() => {
      expect(result.current.runStatus).toBe("interrupted");
    });
    expect(source?.closed).toBe(true);
    expect(result.current.live).toBe(false);
  });

  it("reports an unknown run as notFound", async () => {
    vi.stubGlobal("EventSource", FakeEventSource);
    mockFetch(() => json({ error: "run not found" }, 404));
    const { result } = renderHook(() => useRunEvents("nope"));
    await waitFor(() => {
      expect(result.current.notFound).toBe(true);
    });
    expect(FakeEventSource.instances).toHaveLength(0);
  });
});
