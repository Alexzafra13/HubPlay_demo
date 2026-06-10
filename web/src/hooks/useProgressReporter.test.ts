import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { renderHook } from "@testing-library/react";
import { useProgressReporter } from "./useProgressReporter";
import { api } from "@/api/client";
import type { RefObject } from "react";

vi.mock("@/api/client", () => ({
  api: {
    updateProgress: vi.fn().mockResolvedValue({}),
  },
}));

describe("useProgressReporter", () => {
  let mockVideo: Partial<HTMLVideoElement>;
  let videoRef: RefObject<HTMLVideoElement | null>;
  let videoListeners: Record<string, EventListener[]>;

  beforeEach(() => {
    vi.useFakeTimers();
    vi.clearAllMocks();
    mockVideo = {
      currentTime: 42.5,
    };
    Object.defineProperty(mockVideo, "paused", {
      value: false,
      writable: true,
      configurable: true,
    });
    Object.defineProperty(mockVideo, "seeking", {
      value: false,
      writable: true,
      configurable: true,
    });
    // PB-18: el hook engancha 'pause' al <video>; el mock registra los
    // listeners para que los tests puedan dispararlos.
    videoListeners = {};
    mockVideo.addEventListener = vi.fn((ev: string, fn: EventListenerOrEventListenerObject) => {
      (videoListeners[ev] ??= []).push(fn as EventListener);
    }) as HTMLVideoElement["addEventListener"];
    mockVideo.removeEventListener = vi.fn() as HTMLVideoElement["removeEventListener"];
    videoRef = { current: mockVideo as HTMLVideoElement };
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("saves progress every 10 seconds while playing", () => {
    renderHook(() => useProgressReporter(videoRef, "item-1"));

    vi.advanceTimersByTime(10_000);

    expect(api.updateProgress).toHaveBeenCalledWith("item-1", {
      position_ticks: Math.floor(42.5 * 10_000_000),
    });
  });

  it("does not save progress when paused", () => {
    Object.defineProperty(mockVideo, "paused", { value: true, writable: true, configurable: true });
    renderHook(() => useProgressReporter(videoRef, "item-1"));

    vi.advanceTimersByTime(10_000);

    expect(api.updateProgress).not.toHaveBeenCalled();
  });

  it("does not save progress when currentTime is 0", () => {
    mockVideo.currentTime = 0;
    renderHook(() => useProgressReporter(videoRef, "item-1"));

    vi.advanceTimersByTime(10_000);

    expect(api.updateProgress).not.toHaveBeenCalled();
  });

  it("saves progress multiple times over intervals", () => {
    renderHook(() => useProgressReporter(videoRef, "item-1"));

    vi.advanceTimersByTime(30_000);

    expect(api.updateProgress).toHaveBeenCalledTimes(3);
  });

  it("saves final progress on unmount with keepalive so it survives navigation", () => {
    const { unmount } = renderHook(() =>
      useProgressReporter(videoRef, "item-1"),
    );

    vi.clearAllMocks();
    unmount();

    // Final position is reported with keepalive: true so the browser
    // commits the request even if the user is closing the tab. Without
    // it, navigating away from the player aborts the in-flight fetch
    // and the last 10 seconds of watch progress get lost.
    expect(api.updateProgress).toHaveBeenCalledWith(
      "item-1",
      { position_ticks: Math.floor(42.5 * 10_000_000) },
      { keepalive: true },
    );
  });

  it("does not save on unmount when currentTime is 0", () => {
    mockVideo.currentTime = 0;
    const { unmount } = renderHook(() =>
      useProgressReporter(videoRef, "item-1"),
    );

    vi.clearAllMocks();
    unmount();

    expect(api.updateProgress).not.toHaveBeenCalled();
  });

  it("clears interval on unmount", () => {
    const { unmount } = renderHook(() =>
      useProgressReporter(videoRef, "item-1"),
    );

    unmount();
    vi.clearAllMocks();

    vi.advanceTimersByTime(20_000);

    expect(api.updateProgress).not.toHaveBeenCalled();
  });

  // Mid-seek the <video>'s currentTime briefly reports the pre-seek
  // sample (the new buffer hasn't landed yet), so persisting it would
  // corrupt resume — the user clicked away from that point but the
  // server would think they're still there. Pinning this gate keeps
  // the bug we shipped 2026-05-07 from regressing silently.
  it("does not save while a seek is in flight", () => {
    Object.defineProperty(mockVideo, "seeking", {
      value: true,
      writable: true,
      configurable: true,
    });
    renderHook(() => useProgressReporter(videoRef, "item-1"));

    vi.advanceTimersByTime(10_000);

    expect(api.updateProgress).not.toHaveBeenCalled();
  });

  // Same gate on the unmount path: a player that closes mid-seek
  // (user pressed the back button while ffmpeg was still respawning)
  // shouldn't write the pre-seek sample as the "final" position.
  it("does not save on unmount while a seek is in flight", () => {
    Object.defineProperty(mockVideo, "seeking", {
      value: true,
      writable: true,
      configurable: true,
    });
    const { unmount } = renderHook(() =>
      useProgressReporter(videoRef, "item-1"),
    );

    vi.clearAllMocks();
    unmount();

    expect(api.updateProgress).not.toHaveBeenCalled();
  });

  // ─── PB-18: persistencia en pause y pagehide ───

  it("guarda la posición al pausar (el interval salta las muestras en pausa)", () => {
    renderHook(() => useProgressReporter(videoRef, "item-1"));

    for (const fn of videoListeners["pause"] ?? []) fn(new Event("pause"));

    expect(api.updateProgress).toHaveBeenCalledWith("item-1", {
      position_ticks: Math.floor(42.5 * 10_000_000),
    }, undefined);
  });

  it("guarda con keepalive en pagehide (cerrar pestaña no desmonta React)", () => {
    renderHook(() => useProgressReporter(videoRef, "item-1"));

    window.dispatchEvent(new Event("pagehide"));

    expect(api.updateProgress).toHaveBeenCalledWith(
      "item-1",
      { position_ticks: Math.floor(42.5 * 10_000_000) },
      { keepalive: true },
    );
  });

  it("pause/pagehide tampoco persisten posiciones mid-seek", () => {
    Object.defineProperty(mockVideo, "seeking", { value: true, configurable: true });
    renderHook(() => useProgressReporter(videoRef, "item-1"));

    for (const fn of videoListeners["pause"] ?? []) fn(new Event("pause"));
    window.dispatchEvent(new Event("pagehide"));

    expect(api.updateProgress).not.toHaveBeenCalled();
  });
});
