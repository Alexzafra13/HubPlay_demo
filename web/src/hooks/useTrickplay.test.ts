import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { renderHook, waitFor } from "@testing-library/react";
import { useTrickplay, type TrickplayManifest } from "./useTrickplay";

const sampleManifest: TrickplayManifest = {
  interval_sec: 10,
  thumb_width: 320,
  thumb_height: 180,
  columns: 10,
  rows: 10,
  total: 100,
};

beforeEach(() => {
  vi.stubGlobal("fetch", vi.fn());
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("useTrickplay", () => {
  it("returns unavailable + does not fetch when itemId is empty", () => {
    const { result } = renderHook(() => useTrickplay(""));
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

    const { result } = renderHook(() => useTrickplay("item-42"));
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

    const { result } = renderHook(() => useTrickplay("item-bare"));
    await waitFor(() => expect(result.current.available).toBe(true));
    expect(result.current.manifest).toEqual(sampleManifest);
  });

  it("flips available=false on a non-OK response (e.g. 503 disabled)", async () => {
    (fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
      ok: false,
      status: 503,
      json: () => Promise.resolve({}),
    });

    const { result } = renderHook(() => useTrickplay("item-503"));
    // Microtask drain — catch() runs immediately on rejection.
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

    const { result } = renderHook(() => useTrickplay("item-net"));
    await waitFor(() => expect(result.current.available).toBe(false));
    expect(result.current.manifest).toBeNull();
  });

  it("aborts the fetch on unmount so a fast nav doesn't leak in-flight requests", () => {
    const abortSpy = vi.fn();
    (fetch as ReturnType<typeof vi.fn>).mockImplementation((_url, init) => {
      const sig = (init as { signal?: AbortSignal }).signal;
      sig?.addEventListener("abort", abortSpy);
      // Return a never-resolving promise so the fetch is genuinely
      // in-flight when the hook unmounts.
      return new Promise(() => {});
    });

    const { unmount } = renderHook(() => useTrickplay("item-leak"));
    unmount();
    expect(abortSpy).toHaveBeenCalled();
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

    const { result, rerender } = renderHook(
      ({ id }: { id: string }) => useTrickplay(id),
      { initialProps: { id: "first" } },
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
    renderHook(() => useTrickplay("weird/id with space"));
    await waitFor(() => expect(fetch).toHaveBeenCalled());
    const calledWith = (fetch as ReturnType<typeof vi.fn>).mock.calls[0][0];
    expect(calledWith).toBe(
      "/api/v1/items/weird%2Fid%20with%20space/trickplay.json",
    );
  });
});
