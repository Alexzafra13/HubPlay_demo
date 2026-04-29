import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { renderHook } from "@testing-library/react";
import { useEventStream } from "./useEventStream";

// jsdom doesn't ship EventSource. We replace the global with a tiny
// fake that records what listeners are attached, lets the test fire
// matching events, and tracks open/close so the cleanup contract is
// observable.
class FakeEventSource {
  static instances: FakeEventSource[] = [];
  url: string;
  withCredentials: boolean;
  closed = false;
  listeners = new Map<string, Set<(e: MessageEvent) => void>>();
  constructor(url: string, init?: { withCredentials?: boolean }) {
    this.url = url;
    this.withCredentials = init?.withCredentials ?? false;
    FakeEventSource.instances.push(this);
  }
  addEventListener(type: string, handler: (e: MessageEvent) => void) {
    if (!this.listeners.has(type)) this.listeners.set(type, new Set());
    this.listeners.get(type)!.add(handler);
  }
  removeEventListener(type: string, handler: (e: MessageEvent) => void) {
    this.listeners.get(type)?.delete(handler);
  }
  close() {
    this.closed = true;
  }
  // Test helper: dispatch an event of `type` to all listeners.
  emit(type: string, data: string) {
    this.listeners.get(type)?.forEach((h) => h({ data } as MessageEvent));
  }
}

beforeEach(() => {
  FakeEventSource.instances = [];
  vi.stubGlobal("EventSource", FakeEventSource);
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("useEventStream", () => {
  it("opens one EventSource on mount and forwards matching events", () => {
    const onEvent = vi.fn();
    renderHook(() => useEventStream("library.scan.completed", onEvent));

    expect(FakeEventSource.instances).toHaveLength(1);
    const es = FakeEventSource.instances[0];
    expect(es.url).toBe("/api/v1/events");
    expect(es.withCredentials).toBe(true);

    es.emit("library.scan.completed", '{"library_id":"lib-1"}');
    expect(onEvent).toHaveBeenCalledWith('{"library_id":"lib-1"}');
  });

  it("ignores events of a different type", () => {
    const onEvent = vi.fn();
    renderHook(() => useEventStream("playlist.refreshed", onEvent));
    const es = FakeEventSource.instances[0];

    es.emit("library.scan.completed", "{}");
    expect(onEvent).not.toHaveBeenCalled();
  });

  it("closes the EventSource on unmount", () => {
    const { unmount } = renderHook(() => useEventStream("x", vi.fn()));
    const es = FakeEventSource.instances[0];

    expect(es.closed).toBe(false);
    unmount();
    expect(es.closed).toBe(true);
  });

  it("does not open an EventSource when disabled", () => {
    renderHook(() => useEventStream("x", vi.fn(), false));
    expect(FakeEventSource.instances).toHaveLength(0);
  });

  it("toggling enabled tears down the existing source and opens a fresh one", () => {
    const onEvent = vi.fn();
    const { rerender } = renderHook(
      ({ enabled }: { enabled: boolean }) => useEventStream("x", onEvent, enabled),
      { initialProps: { enabled: true } },
    );
    expect(FakeEventSource.instances).toHaveLength(1);

    rerender({ enabled: false });
    expect(FakeEventSource.instances[0].closed).toBe(true);

    rerender({ enabled: true });
    expect(FakeEventSource.instances).toHaveLength(2);
    expect(FakeEventSource.instances[1].closed).toBe(false);
  });

  // Regression for the "stash handler in a ref" optimisation: the hook
  // must not tear down and reopen the EventSource on every render with
  // a new handler closure. Without the ref, the parent re-rendering
  // with a fresh inline `(d) => ...` would churn the connection — and
  // each open eats a server-side handler subscription.
  it("re-rendering with a new handler closure keeps the same EventSource", () => {
    let received = "";
    const { rerender } = renderHook(
      ({ tag }: { tag: string }) =>
        useEventStream("x", (d) => {
          received = `${tag}:${d}`;
        }),
      { initialProps: { tag: "first" } },
    );
    const firstES = FakeEventSource.instances[0];

    rerender({ tag: "second" });
    expect(FakeEventSource.instances).toHaveLength(1);
    expect(firstES.closed).toBe(false);

    // The latest handler closure must be invoked, not the one captured
    // when the EventSource was first opened.
    firstES.emit("x", "payload");
    expect(received).toBe("second:payload");
  });

  it("changing the event type swaps the underlying source", () => {
    const onEvent = vi.fn();
    const { rerender } = renderHook(
      ({ type }: { type: string }) => useEventStream(type, onEvent),
      { initialProps: { type: "a" } },
    );
    const first = FakeEventSource.instances[0];

    rerender({ type: "b" });
    expect(first.closed).toBe(true);
    expect(FakeEventSource.instances).toHaveLength(2);

    FakeEventSource.instances[1].emit("a", "stale");
    expect(onEvent).not.toHaveBeenCalled();

    FakeEventSource.instances[1].emit("b", "fresh");
    expect(onEvent).toHaveBeenCalledWith("fresh");
  });
});
