import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useMySessions } from "./auth";
import { __eventBusTestHelpers } from "@/hooks/eventBus";
import { api } from "../client";

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
  vi.spyOn(api, "listMySessions").mockResolvedValue([]);
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

describe("useMySessions", () => {
  it("does NOT poll on a refetch interval", async () => {
    const { wrapper } = makeWrapper();
    renderHook(() => useMySessions(), { wrapper });
    await waitFor(() => {
      expect(api.listMySessions).toHaveBeenCalledTimes(1);
    });
    // Pre-migration this had refetchInterval: 30_000. The bar to
    // clear: the API call count does not climb on its own.
    expect(api.listMySessions).toHaveBeenCalledTimes(1);
  });

  it("subscribes to user.logged_in + user.logged_out on the user-scoped /me/events stream", () => {
    const { wrapper } = makeWrapper();
    renderHook(() => useMySessions(), { wrapper });
    expect(FakeEventSource.instances).toHaveLength(1);
    expect(FakeEventSource.instances[0].url).toBe("/api/v1/me/events");
    const es = FakeEventSource.instances[0];
    expect(es.listeners.has("user.logged_in")).toBe(true);
    expect(es.listeners.has("user.logged_out")).toBe(true);
  });

  it("invalidates the sessions list on user.logged_in", async () => {
    const { wrapper, invalidateSpy } = makeWrapper();
    renderHook(() => useMySessions(), { wrapper });
    await waitFor(() => {
      expect(api.listMySessions).toHaveBeenCalled();
    });

    const es = findInstance("user.logged_in");
    expect(es).toBeDefined();
    es!.emit(
      "user.logged_in",
      JSON.stringify({
        type: "user.logged_in",
        data: { user_id: "u-1", username: "alex", device_name: "iPhone" },
      }),
    );

    const calls = invalidateSpy.mock.calls.map((c) => c[0]);
    expect(calls).toContainEqual({ queryKey: ["me", "sessions"] });
  });

  it("invalidates the sessions list on user.logged_out", async () => {
    const { wrapper, invalidateSpy } = makeWrapper();
    renderHook(() => useMySessions(), { wrapper });
    await waitFor(() => {
      expect(api.listMySessions).toHaveBeenCalled();
    });

    const es = findInstance("user.logged_out");
    expect(es).toBeDefined();
    es!.emit(
      "user.logged_out",
      JSON.stringify({
        type: "user.logged_out",
        data: { user_id: "u-1", session_id: "s-1" },
      }),
    );

    const calls = invalidateSpy.mock.calls.map((c) => c[0]);
    expect(calls).toContainEqual({ queryKey: ["me", "sessions"] });
  });
});
