import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createElement, type ReactNode } from "react";
import { useTrickplay, type TrickplayManifest } from "./useTrickplay";

const sampleManifest: TrickplayManifest = {
  interval_sec: 10,
  thumb_width: 320,
  thumb_height: 180,
  columns: 10,
  rows: 10,
  total: 100,
};

function makeWrapper() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0, staleTime: 0 } },
  });
  const wrapper = ({ children }: { children: ReactNode }) =>
    createElement(QueryClientProvider, { client: queryClient }, children);
  return { wrapper, queryClient };
}

beforeEach(() => {
  vi.stubGlobal("fetch", vi.fn());
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("useTrickplay", () => {
  it("returns unavailable + does not fetch when itemId is empty", async () => {
    const { wrapper } = makeWrapper();
    const { result } = renderHook(() => useTrickplay(""), { wrapper });
    expect(fetch).not.toHaveBeenCalled();
    expect(result.current).toEqual({
      manifest: null,
      spriteURL: "",
      available: false,
    });
  });

  it("populates manifest + sprite URL on a 200 response", async () => {
    (fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
      ok: true,
      status: 200,
      json: () => Promise.resolve({ data: sampleManifest }),
    });

    const { wrapper } = makeWrapper();
    const { result } = renderHook(() => useTrickplay("item-42"), { wrapper });
    await waitFor(() => expect(result.current.available).toBe(true));

    expect(result.current.manifest).toEqual(sampleManifest);
    expect(result.current.spriteURL).toBe(
      "/api/v1/items/item-42/trickplay.png",
    );
    expect(fetch).toHaveBeenCalledWith(
      "/api/v1/items/item-42/trickplay.json",
      expect.objectContaining({ credentials: "same-origin" }),
    );
  });

  // Regression for the "tolerate either shape" comment in the hook.
  // The trickplay JSON is ServeFile'd from disk so it lacks the standard
  // {data: ...} envelope; if the backend ever wraps it, the hook must
  // still work — the contract reads "fetch a manifest, project it",
  // not "decode this exact envelope".
  it("accepts a bare manifest body without the {data} envelope", async () => {
    (fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
      ok: true,
      status: 200,
      json: () => Promise.resolve(sampleManifest),
    });

    const { wrapper } = makeWrapper();
    const { result } = renderHook(() => useTrickplay("item-bare"), { wrapper });
    await waitFor(() => expect(result.current.available).toBe(true));
    expect(result.current.manifest).toEqual(sampleManifest);
  });

  it("flips available=false on a non-OK response (e.g. 503 disabled)", async () => {
    (fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
      ok: false,
      status: 503,
      json: () => Promise.resolve({}),
    });

    const { wrapper } = makeWrapper();
    const { result } = renderHook(() => useTrickplay("item-503"), { wrapper });
    await waitFor(() =>
      expect(result.current).toEqual({
        manifest: null,
        spriteURL: "",
        available: false,
      }),
    );
  });

  it("flips available=false on network error", async () => {
    (fetch as ReturnType<typeof vi.fn>).mockRejectedValueOnce(
      new Error("network"),
    );

    const { wrapper } = makeWrapper();
    const { result } = renderHook(() => useTrickplay("item-net"), { wrapper });
    await waitFor(() => expect(result.current.available).toBe(false));
    expect(result.current.manifest).toBeNull();
  });

  // The cold-start ffmpeg pass is 5-30s; coming back to the same item
  // within a session must not re-pay it. This is the whole reason the
  // hook moved to TanStack Query.
  it("reuses the cached manifest on a remount of the same item", async () => {
    (fetch as ReturnType<typeof vi.fn>).mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({ data: sampleManifest }),
    });

    const queryClient = new QueryClient({
      defaultOptions: {
        queries: { retry: false, staleTime: 5 * 60_000 },
      },
    });
    const wrapper = ({ children }: { children: ReactNode }) =>
      createElement(QueryClientProvider, { client: queryClient }, children);

    const first = renderHook(() => useTrickplay("item-cache"), { wrapper });
    await waitFor(() => expect(first.result.current.available).toBe(true));
    expect(fetch).toHaveBeenCalledTimes(1);
    first.unmount();

    const second = renderHook(() => useTrickplay("item-cache"), { wrapper });
    // Cached: no second network call, manifest immediately available.
    await waitFor(() => expect(second.result.current.available).toBe(true));
    expect(fetch).toHaveBeenCalledTimes(1);
    expect(second.result.current.manifest).toEqual(sampleManifest);
  });

  it("re-fetches when itemId changes", async () => {
    (fetch as ReturnType<typeof vi.fn>)
      .mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve({ data: sampleManifest }),
      })
      .mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve({ data: sampleManifest }),
      });

    const { wrapper } = makeWrapper();
    const { result, rerender } = renderHook(
      ({ id }: { id: string }) => useTrickplay(id),
      { initialProps: { id: "first" }, wrapper },
    );
    await waitFor(() => expect(result.current.available).toBe(true));
    expect(result.current.spriteURL).toBe("/api/v1/items/first/trickplay.png");

    rerender({ id: "second" });
    await waitFor(() =>
      expect(result.current.spriteURL).toBe(
        "/api/v1/items/second/trickplay.png",
      ),
    );
    expect(fetch).toHaveBeenCalledTimes(2);
  });

  // Item ids could in theory contain slashes (UUIDs don't, but the
  // type is `string` and a future format change shouldn't break the
  // URL). encodeURIComponent in the hook protects against that.
  it("URL-encodes the item id", async () => {
    (fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
      ok: true,
      json: () => Promise.resolve({ data: sampleManifest }),
    });
    const { wrapper } = makeWrapper();
    renderHook(() => useTrickplay("weird/id with space"), { wrapper });
    await waitFor(() => expect(fetch).toHaveBeenCalled());
    const calledWith = (fetch as ReturnType<typeof vi.fn>).mock.calls[0][0];
    expect(calledWith).toBe(
      "/api/v1/items/weird%2Fid%20with%20space/trickplay.json",
    );
  });

});
