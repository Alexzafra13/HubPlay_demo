import { useEffect, useState } from "react";

/**
 * useNowTick — returns the current epoch-ms and re-renders the caller on a
 * fixed cadence. Used by Live TV surfaces (EPGGrid, PlayerOverlay,
 * HeroSpotlight) that display time-sensitive state like "now on air" and
 * program progress bars.
 *
 * 30 s is smooth enough for a minute-granularity guide without paying the
 * cost of per-second re-renders.
 */
export function useNowTick(intervalMs = 30_000): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const id = window.setInterval(() => setNow(Date.now()), intervalMs);
    return () => window.clearInterval(id);
  }, [intervalMs]);
  return now;
}
