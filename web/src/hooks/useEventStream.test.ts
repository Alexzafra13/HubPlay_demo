import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { renderHook } from "@testing-library/react";
import { useEventStream } from "./useEventStream";
import { __eventBusTestHelpers, subscribeSse } from "./eventBus";

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
  // Bus state is module-global; tests that share a URL across cases
  // would otherwise observe stale refcounts from a previous run.
  __eventBusTestHelpers.reset();
});

afterEach(() => {
  __eventBusTestHelpers.reset();
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

// ────────────────────────────────────────────────────────────────────────
// Bus multiplexing — pins the contract that N subscribers to the same URL
// share ONE EventSource. Without this, every useEventStream / use*EventStream
// call opens its own connection and a tab quickly hits Chrome's ~6
// SSE-per-origin cap (admin streams + the three /me/events listeners in
// useUserDataSync).
// ────────────────────────────────────────────────────────────────────────

describe("eventBus multiplexing", () => {
  it("two subscribers to the same URL share one EventSource", () => {
    const onA = vi.fn();
    const onB = vi.fn();
    renderHook(() => useEventStream("a", onA));
    renderHook(() => useEventStream("b", onB));

    expect(FakeEventSource.instances).toHaveLength(1);
    expect(__eventBusTestHelpers.refcount("/api/v1/events", true)).toBe(2);

    // Both handlers fire from the single underlying source.
    const es = FakeEventSource.instances[0];
    es.emit("a", "first");
    es.emit("b", "second");
    expect(onA).toHaveBeenCalledWith("first");
    expect(onB).toHaveBeenCalledWith("second");
  });

  it("the source closes only after the LAST subscriber unmounts", () => {
    const a = renderHook(() => useEventStream("a", vi.fn()));
    const b = renderHook(() => useEventStream("b", vi.fn()));
    const es = FakeEventSource.instances[0];

    a.unmount();
    expect(es.closed).toBe(false);
    expect(__eventBusTestHelpers.refcount("/api/v1/events", true)).toBe(1);

    b.unmount();
    expect(es.closed).toBe(true);
    expect(__eventBusTestHelpers.channelCount()).toBe(0);
  });

  it("multiple subscribers to the SAME type all receive the event", () => {
    const onA1 = vi.fn();
    const onA2 = vi.fn();
    renderHook(() => useEventStream("shared", onA1));
    renderHook(() => useEventStream("shared", onA2));

    expect(FakeEventSource.instances).toHaveLength(1);
    FakeEventSource.instances[0].emit("shared", "broadcast");
    expect(onA1).toHaveBeenCalledWith("broadcast");
    expect(onA2).toHaveBeenCalledWith("broadcast");
  });

  it("a throwing handler does not break sibling subscribers", () => {
    const bad = vi.fn(() => {
      throw new Error("boom");
    });
    const good = vi.fn();
    subscribeSse("/api/v1/events", true, "x", bad);
    subscribeSse("/api/v1/events", true, "x", good);

    const es = FakeEventSource.instances[0];
    es.emit("x", "ping");
    expect(bad).toHaveBeenCalled();
    expect(good).toHaveBeenCalledWith("ping");
  });
});
