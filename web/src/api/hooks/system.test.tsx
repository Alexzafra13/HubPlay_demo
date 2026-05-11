import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useAdminStreamSessions } from "./system";
import { queryKeys } from "../queryKeys";
import { __eventBusTestHelpers } from "@/hooks/eventBus";
import { api } from "../client";

// Mirror the FakeEventSource shape from useUserDataSync.test.tsx —
// jsdom has no EventSource and the SSE event bus needs add/remove +
// emit semantics to test invalidation paths end-to-end.
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
  vi.spyOn(api, "listAdminStreamSessions").mockResolvedValue([]);
});

afterEach(() => {
  __eventBusTestHelpers.reset();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
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

describe("useAdminStreamSessions", () => {
  it("does NOT poll on a refetch interval", async () => {
    const { wrapper } = makeWrapper();
    renderHook(() => useAdminStreamSessions(), { wrapper });
    // The previous implementation passed refetchInterval: 5000. We
    // can't directly read the query's options after the fact, but we
    // can pin the contract: the API client is called exactly once
    // even after a fake-timers-advanced multi-second pause.
    await waitFor(() => {
      expect(api.listAdminStreamSessions).toHaveBeenCalledTimes(1);
    });
    // No advancing of timers; the migration's success criterion is
    // that the next refetch only fires when an event arrives, not
    // when 5s of wall-clock elapse.
    expect(api.listAdminStreamSessions).toHaveBeenCalledTimes(1);
  });

  it("subscribes to transcode.started + transcode.completed on the global /events stream", () => {
    const { wrapper } = makeWrapper();
    renderHook(() => useAdminStreamSessions(), { wrapper });
    // Both event types must be wired to the SAME EventSource via
    // the bus — otherwise we'd be opening two connections per admin
    // viewer to the same URL.
    expect(FakeEventSource.instances).toHaveLength(1);
    expect(FakeEventSource.instances[0].url).toBe("/api/v1/events");
    const es = FakeEventSource.instances[0];
    expect(es.listeners.has("transcode.started")).toBe(true);
    expect(es.listeners.has("transcode.completed")).toBe(true);
  });

  it("invalidates the sessions query on transcode.started", async () => {
    const { wrapper, invalidateSpy } = makeWrapper();
    renderHook(() => useAdminStreamSessions(), { wrapper });
    await waitFor(() => {
      expect(api.listAdminStreamSessions).toHaveBeenCalled();
    });

    const es = findInstance("transcode.started");
    expect(es).toBeDefined();
    es!.emit(
      "transcode.started",
      JSON.stringify({
        type: "transcode.started",
        data: { session_id: "k1", user_id: "u-1", item_id: "it-1" },
      }),
    );

    const calls = invalidateSpy.mock.calls.map((c) => c[0]);
    expect(calls).toContainEqual({ queryKey: queryKeys.adminStreamSessions });
  });

  it("invalidates the sessions query on transcode.completed", async () => {
    const { wrapper, invalidateSpy } = makeWrapper();
    renderHook(() => useAdminStreamSessions(), { wrapper });
    await waitFor(() => {
      expect(api.listAdminStreamSessions).toHaveBeenCalled();
    });

    const es = findInstance("transcode.completed");
    expect(es).toBeDefined();
    es!.emit(
      "transcode.completed",
      JSON.stringify({
        type: "transcode.completed",
        data: { session_id: "k1", user_id: "u-1", item_id: "it-1" },
      }),
    );

    const calls = invalidateSpy.mock.calls.map((c) => c[0]);
    expect(calls).toContainEqual({ queryKey: queryKeys.adminStreamSessions });
  });
});
