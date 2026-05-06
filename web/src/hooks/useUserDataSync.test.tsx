import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { renderHook } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useUserDataSync } from "./useUserDataSync";
import { queryKeys } from "@/api/queryKeys";
import { __eventBusTestHelpers } from "./eventBus";

// jsdom has no EventSource — fake it the same way useEventStream's
// own tests do. With the SSE event bus, all three subscriptions on
// /me/events multiplex through ONE EventSource (refcounted), so we
// expect a single instance even though useUserDataSync registers
// listeners for three different event types.
class FakeEventSource {
  static instances: FakeEventSource[] = [];
  url: string;
  closed = false;
  listeners = new Map<string, Set<(e: MessageEvent) => void>>();
  constructor(url: string) {
    this.url = url;
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
  emit(type: string, data: string) {
    this.listeners.get(type)?.forEach((h) => h({ data } as MessageEvent));
  }
}

function findInstance(type: string): FakeEventSource | undefined {
  return FakeEventSource.instances.find((es) => es.listeners.has(type));
}

beforeEach(() => {
  FakeEventSource.instances = [];
  vi.stubGlobal("EventSource", FakeEventSource);
  __eventBusTestHelpers.reset();
});

afterEach(() => {
  __eventBusTestHelpers.reset();
  vi.unstubAllGlobals();
});

function makeWrapper() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const invalidateSpy = vi.spyOn(queryClient, "invalidateQueries");
  const wrapper = ({ children }: { children: React.ReactNode }) => (
    <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  );
  return { wrapper, invalidateSpy, queryClient };
}

describe("useUserDataSync", () => {
  it("multiplexes all three subscriptions through ONE EventSource at /api/v1/me/events", () => {
    const { wrapper } = makeWrapper();
    renderHook(() => useUserDataSync(), { wrapper });
    // BEFORE the bus this opened three separate EventSources to the
    // same URL — three TCP connections per logged-in tab burning
    // through Chrome's ~6 SSE-per-origin cap. The bus collapses them
    // to one connection with refcounted listeners.
    expect(FakeEventSource.instances).toHaveLength(1);
    expect(FakeEventSource.instances[0].url).toBe("/api/v1/me/events");
    // The single connection still carries listeners for all three
    // event types — the bus dispatches by event name internally.
    const es = FakeEventSource.instances[0];
    expect(es.listeners.has("user.progress.updated")).toBe(true);
    expect(es.listeners.has("user.played.toggled")).toBe(true);
    expect(es.listeners.has("user.favorite.toggled")).toBe(true);
  });

  it("invalidates item + progress + continue-watching on user.progress.updated", () => {
    const { wrapper, invalidateSpy, queryClient } = makeWrapper();
    // Per-item keys only invalidate when something already fetched them.
    queryClient.setQueryData(queryKeys.item("it-7"), { id: "it-7" });
    queryClient.setQueryData(queryKeys.progress("it-7"), { position_ticks: 0 });
    renderHook(() => useUserDataSync(), { wrapper });

    const es = findInstance("user.progress.updated");
    expect(es).toBeDefined();
    es!.emit(
      "user.progress.updated",
      JSON.stringify({
        type: "user.progress.updated",
        data: { user_id: "u-1", item_id: "it-7", position_ticks: 12345 },
      }),
    );

    const calls = invalidateSpy.mock.calls.map((c) => c[0]);
    expect(calls).toContainEqual({ queryKey: queryKeys.item("it-7") });
    expect(calls).toContainEqual({ queryKey: queryKeys.progress("it-7") });
    expect(calls).toContainEqual({ queryKey: queryKeys.continueWatching });
  });

  it("invalidates item + continue-watching + next-up on user.played.toggled", () => {
    const { wrapper, invalidateSpy, queryClient } = makeWrapper();
    queryClient.setQueryData(queryKeys.item("it-9"), { id: "it-9" });
    renderHook(() => useUserDataSync(), { wrapper });

    const es = findInstance("user.played.toggled");
    es!.emit(
      "user.played.toggled",
      JSON.stringify({
        type: "user.played.toggled",
        data: { user_id: "u-1", item_id: "it-9", played: true },
      }),
    );

    const calls = invalidateSpy.mock.calls.map((c) => c[0]);
    expect(calls).toContainEqual({ queryKey: queryKeys.item("it-9") });
    expect(calls).toContainEqual({ queryKey: queryKeys.continueWatching });
    expect(calls).toContainEqual({ queryKey: queryKeys.nextUp });
  });

  it("invalidates item + favorites on user.favorite.toggled", () => {
    const { wrapper, invalidateSpy, queryClient } = makeWrapper();
    queryClient.setQueryData(queryKeys.item("it-3"), { id: "it-3" });
    renderHook(() => useUserDataSync(), { wrapper });

    const es = findInstance("user.favorite.toggled");
    es!.emit(
      "user.favorite.toggled",
      JSON.stringify({
        type: "user.favorite.toggled",
        data: { user_id: "u-1", item_id: "it-3", is_favorite: true },
      }),
    );

    const calls = invalidateSpy.mock.calls.map((c) => c[0]);
    expect(calls).toContainEqual({ queryKey: queryKeys.item("it-3") });
    expect(calls).toContainEqual({ queryKey: queryKeys.favorites });
  });

  it("skips per-item invalidations when no observer fetched the key, but still refreshes global rails", () => {
    // Nothing reads items/it-99 or progress/it-99 — the user is on
    // /home, not on the item detail page. We still want the
    // continue-watching rail (consumed by Home) to refresh; the
    // per-item invalidations are wasted work and we elide them.
    const { wrapper, invalidateSpy } = makeWrapper();
    renderHook(() => useUserDataSync(), { wrapper });

    const es = findInstance("user.progress.updated");
    es!.emit(
      "user.progress.updated",
      JSON.stringify({
        type: "user.progress.updated",
        data: { user_id: "u-1", item_id: "it-99", position_ticks: 99 },
      }),
    );

    const calls = invalidateSpy.mock.calls.map((c) => c[0]);
    expect(calls).not.toContainEqual({ queryKey: queryKeys.item("it-99") });
    expect(calls).not.toContainEqual({ queryKey: queryKeys.progress("it-99") });
    expect(calls).toContainEqual({ queryKey: queryKeys.continueWatching });
  });

  it("ignores malformed JSON and missing item_id without throwing", () => {
    const { wrapper, invalidateSpy } = makeWrapper();
    renderHook(() => useUserDataSync(), { wrapper });

    const es = findInstance("user.progress.updated");
    // Malformed JSON.
    expect(() => es!.emit("user.progress.updated", "not-json")).not.toThrow();
    // Valid JSON but data has no item_id.
    es!.emit(
      "user.progress.updated",
      JSON.stringify({ type: "user.progress.updated", data: { user_id: "u-1" } }),
    );
    // No invalidations should have fired from either drop.
    expect(invalidateSpy).not.toHaveBeenCalled();
  });

  it("closes the underlying EventSource on unmount once refcount hits zero", () => {
    const { wrapper } = makeWrapper();
    const { unmount } = renderHook(() => useUserDataSync(), { wrapper });
    expect(FakeEventSource.instances).toHaveLength(1);
    const es = FakeEventSource.instances[0];
    expect(es.closed).toBe(false);
    unmount();
    expect(es.closed).toBe(true);
    expect(__eventBusTestHelpers.channelCount()).toBe(0);
  });

  it("opens nothing when enabled=false", () => {
    const { wrapper } = makeWrapper();
    renderHook(() => useUserDataSync({ enabled: false }), { wrapper });
    expect(FakeEventSource.instances).toHaveLength(0);
  });
});
