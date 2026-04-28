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
});
