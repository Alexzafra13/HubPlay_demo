import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { renderHook } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useUserDataSync } from "./useUserDataSync";
import { queryKeys } from "@/api/queryKeys";

// jsdom has no EventSource — fake it the same way useEventStream's
// own tests do. Each (type, handler) pair maps to its own fake; the
// orchestrator subscribes to three types so we expect three instances.
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
});

afterEach(() => {
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
  it("opens one EventSource per event type at /api/v1/me/events", () => {
    const { wrapper } = makeWrapper();
    renderHook(() => useUserDataSync(), { wrapper });
    // Three subscriptions → three fake EventSource instances.
    expect(FakeEventSource.instances).toHaveLength(3);
    for (const es of FakeEventSource.instances) {
      expect(es.url).toBe("/api/v1/me/events");
    }
  });

  it("invalidates item + progress + continue-watching on user.progress.updated", () => {
    const { wrapper, invalidateSpy } = makeWrapper();
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
    const { wrapper, invalidateSpy } = makeWrapper();
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
    const { wrapper, invalidateSpy } = makeWrapper();
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

  it("closes all subscriptions on unmount", () => {
    const { wrapper } = makeWrapper();
    const { unmount } = renderHook(() => useUserDataSync(), { wrapper });
    expect(FakeEventSource.instances).toHaveLength(3);
    unmount();
    for (const es of FakeEventSource.instances) {
      expect(es.closed).toBe(true);
    }
  });

  it("opens nothing when enabled=false", () => {
    const { wrapper } = makeWrapper();
    renderHook(() => useUserDataSync({ enabled: false }), { wrapper });
    expect(FakeEventSource.instances).toHaveLength(0);
  });
});
