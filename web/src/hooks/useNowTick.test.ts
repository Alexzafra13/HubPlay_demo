import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { act, renderHook } from "@testing-library/react";
import { useNowTick } from "./useNowTick";

// All tests run against a fixed system clock so `Date.now()` inside the
// hook is deterministic. Timer advancements fire the interval AND bump
// the mocked clock, so `setNow(Date.now())` reads the new instant.
const T0 = new Date("2026-04-24T12:00:00Z").getTime();

describe("useNowTick", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(T0);
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it("returns the current time on first render", () => {
    const { result } = renderHook(() => useNowTick());
    expect(result.current).toBe(T0);
  });

  it("re-renders after the interval elapses (default 30 s)", () => {
    const { result } = renderHook(() => useNowTick());
    expect(result.current).toBe(T0);

    // Just before the tick — still the original value.
    act(() => {
      vi.advanceTimersByTime(29_999);
    });
    expect(result.current).toBe(T0);

    // At exactly 30 s, the interval fires and we re-render with the
    // advanced clock.
    act(() => {
      vi.advanceTimersByTime(1);
    });
    expect(result.current).toBe(T0 + 30_000);
  });

  it("honours a custom interval", () => {
    const { result } = renderHook(() => useNowTick(1_000));
    act(() => {
      vi.advanceTimersByTime(1_000);
    });
    expect(result.current).toBe(T0 + 1_000);
  });

  it("keeps re-ticking on every interval window", () => {
    const { result } = renderHook(() => useNowTick(1_000));
    act(() => {
      vi.advanceTimersByTime(3_000);
    });
    // Three intervals elapsed.
    expect(result.current).toBe(T0 + 3_000);
  });

  it("clears the interval on unmount (no stray timers)", () => {
    const { unmount } = renderHook(() => useNowTick(1_000));
    unmount();
    // If the interval leaked, `vi.getTimerCount()` would still report ≥1.
    expect(vi.getTimerCount()).toBe(0);
  });

  it("resets the interval when intervalMs changes", () => {
    const { result, rerender } = renderHook(
      ({ ms }: { ms: number }) => useNowTick(ms),
      { initialProps: { ms: 1_000 } },
    );

    // Switch to a faster cadence.
    rerender({ ms: 500 });

    act(() => {
      vi.advanceTimersByTime(500);
    });
    expect(result.current).toBe(T0 + 500);
  });
});
