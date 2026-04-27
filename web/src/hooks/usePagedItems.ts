import { useCallback, useEffect, useRef, useState } from "react";

interface UsePagedItemsResult<T> {
  /** The slice currently visible to the user. */
  visible: T[];
  /** True iff there are more items beyond the current page. */
  hasMore: boolean;
  /** Manually advance one page (e.g. wired to a "Show more" button). */
  loadMore: () => void;
  /**
   * Ref to attach to a sentinel element placed at the bottom of the
   * list. When it scrolls into view (with `rootMargin` slack), the
   * hook auto-advances. Pair it with a small invisible div:
   *   `<div ref={sentinelRef} aria-hidden />`
   */
  sentinelRef: React.RefObject<HTMLDivElement | null>;
  /** Total length of the source array — useful for "X of Y" counters. */
  total: number;
}

/**
 * usePagedItems — render a slice of a large array, growing the slice
 * as the user scrolls. Handles three concerns the grids on /live-tv
 * needed to scale past ~22000 channels:
 *
 *   1. **Bounded DOM**: rendering 22k cards locks the main thread.
 *      We slice to `step` items (default 60) and grow on demand. The
 *      browser only ever has a few hundred ChannelCard nodes alive
 *      even for huge libraries.
 *
 *   2. **Reset on input change**: when the parent's filter changes
 *      (a new search query or category) the source array reference
 *      changes and we reset the page back to `step`. Done with the
 *      React docs "compare during render" pattern (no
 *      setState-in-effect, no extra commit).
 *
 *   3. **Auto-advance via IntersectionObserver**: a sentinel element
 *      placed at the bottom of the visible list fires `loadMore` when
 *      it scrolls into view. The 400 px `rootMargin` triggers slightly
 *      before the user reaches the bottom so the next page is already
 *      mounting by the time their cursor catches up — feels seamless.
 */
export function usePagedItems<T>(
  all: T[],
  step = 60,
): UsePagedItemsResult<T> {
  const [size, setSize] = useState(step);

  // Reset on input change. Tracking via state (not just an effect) so
  // the new size is visible in the same render — avoids a flash where
  // the user sees the previous filter's deeper page count for one
  // frame.
  const [lastAll, setLastAll] = useState(all);
  if (lastAll !== all) {
    setLastAll(all);
    setSize(step);
  }

  const visible = all.slice(0, size);
  const hasMore = size < all.length;

  const loadMore = useCallback(() => {
    setSize((s) => Math.min(s + step, all.length));
  }, [all.length, step]);

  const sentinelRef = useRef<HTMLDivElement | null>(null);
  useEffect(() => {
    if (!hasMore) return;
    const el = sentinelRef.current;
    if (!el) return;
    const observer = new IntersectionObserver(
      (entries) => {
        if (entries.some((e) => e.isIntersecting)) loadMore();
      },
      { rootMargin: "400px" },
    );
    observer.observe(el);
    return () => observer.disconnect();
  }, [hasMore, loadMore]);

  return { visible, hasMore, loadMore, sentinelRef, total: all.length };
}
