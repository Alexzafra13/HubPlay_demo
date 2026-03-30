import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { renderHook, act } from "@testing-library/react";
import { useControlsVisibility } from "./useControlsVisibility";

describe("useControlsVisibility", () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("starts with controls visible", () => {
    const { result } = renderHook(() => useControlsVisibility(false));
    expect(result.current.controlsVisible).toBe(true);
  });

  it("hides controls after delay when playing", () => {
    const { result } = renderHook(() => useControlsVisibility(true));

    act(() => {
      result.current.showControls();
    });

    expect(result.current.controlsVisible).toBe(true);

    act(() => {
      vi.advanceTimersByTime(3000);
    });

    expect(result.current.controlsVisible).toBe(false);
  });

  it("does not hide controls when paused", () => {
    const { result } = renderHook(() => useControlsVisibility(false));

    act(() => {
      result.current.showControls();
    });

    act(() => {
      vi.advanceTimersByTime(5000);
    });

    expect(result.current.controlsVisible).toBe(true);
  });

  it("resets timer on repeated mouse move", () => {
    const { result } = renderHook(() => useControlsVisibility(true));

    act(() => {
      result.current.handleMouseMove();
    });

    // Advance 2s then move again
    act(() => {
      vi.advanceTimersByTime(2000);
    });
    act(() => {
      result.current.handleMouseMove();
    });

    // 2s after second move: should still be visible (timer reset)
    act(() => {
      vi.advanceTimersByTime(2000);
    });
    expect(result.current.controlsVisible).toBe(true);

    // 1s more (3s total since last move): should hide
    act(() => {
      vi.advanceTimersByTime(1000);
    });
    expect(result.current.controlsVisible).toBe(false);
  });

  it("handleMouseLeave hides after short delay when playing", () => {
    const { result } = renderHook(() => useControlsVisibility(true));

    act(() => {
      result.current.handleMouseLeave();
    });

    act(() => {
      vi.advanceTimersByTime(800);
    });

    expect(result.current.controlsVisible).toBe(false);
  });

  it("keepControlsVisible prevents hiding", () => {
    const { result } = renderHook(() => useControlsVisibility(true));

    act(() => {
      result.current.showControls();
    });

    // Before timer fires, keep controls visible
    act(() => {
      vi.advanceTimersByTime(2000);
    });
    act(() => {
      result.current.keepControlsVisible();
    });

    act(() => {
      vi.advanceTimersByTime(5000);
    });

    expect(result.current.controlsVisible).toBe(true);
  });
});
