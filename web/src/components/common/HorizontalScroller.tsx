// HorizontalScroller — Plex/Netflix-style horizontal rail wrapper.
//
// The native scrollbar is hidden everywhere (touch keeps swipe-to-
// scroll, desktop gets the hover arrows). When the content overflows
// the viewport, a left/right chevron appears on hover. Clicking it
// pages the row by ~85% of the visible width — the same "show me the
// next batch" gesture every catalog UI uses.
//
// The arrows hide automatically at the start / end of the row so we
// never invite a click that does nothing. We track scroll state with
// a passive listener + a ResizeObserver so the visibility flips when
// the user resizes the window or new content streams in (the latest
// rail re-renders when its query resolves).

import {
  useEffect,
  useRef,
  useState,
  type ReactNode,
} from "react";

interface HorizontalScrollerProps {
  children: ReactNode;
  /**
   * Tailwind class for the inner flex row. Callers that need a
   * custom layout (e.g. ChannelRail uses `grid auto-cols-[260px]
   * grid-flow-col`) override the default `flex gap-4` here.
   */
  innerClassName?: string;
  /**
   * Bottom-padding class for the inner row. Defaults to `pb-2` so
   * card hover-zoom doesn't get clipped by the row's overflow box.
   */
  paddingClassName?: string;
  /** Optional aria-label for the scroll region. */
  ariaLabel?: string;
}

export function HorizontalScroller({
  children,
  innerClassName = "flex gap-4",
  paddingClassName = "pb-2",
  ariaLabel,
}: HorizontalScrollerProps) {
  const scrollRef = useRef<HTMLDivElement | null>(null);
  const [canScrollLeft, setCanScrollLeft] = useState(false);
  const [canScrollRight, setCanScrollRight] = useState(false);

  useEffect(() => {
    const el = scrollRef.current;
    if (!el) return;
    const update = () => {
      // 1px tolerance — sub-pixel rounding makes scrollLeft jitter
      // around the real boundary when the user lands exactly on it,
      // which would otherwise flicker the arrow on / off.
      setCanScrollLeft(el.scrollLeft > 1);
      setCanScrollRight(el.scrollLeft + el.clientWidth < el.scrollWidth - 1);
    };
    update();
    el.addEventListener("scroll", update, { passive: true });
    // ResizeObserver covers two cases: the viewport changes width
    // (window resize, sidebar collapse) AND the content itself grows
    // when a query resolves and the rail swaps skeletons for cards.
    const ro = new ResizeObserver(update);
    ro.observe(el);
    for (const child of Array.from(el.children)) ro.observe(child);
    return () => {
      el.removeEventListener("scroll", update);
      ro.disconnect();
    };
  }, []);

  const scrollBy = (dir: -1 | 1) => {
    const el = scrollRef.current;
    if (!el) return;
    // 85% of the visible width keeps a sliver of the previous batch
    // visible after the page-jump so the user can see the connection.
    const delta = el.clientWidth * 0.85 * dir;
    el.scrollBy({ left: delta, behavior: "smooth" });
  };

  return (
    <div className="group/scroller relative">
      <div
        ref={scrollRef}
        className={`overflow-x-auto scrollbar-hide ${paddingClassName}`}
        aria-label={ariaLabel}
      >
        <div className={innerClassName}>{children}</div>
      </div>

      {/* Arrows — desktop-only, fade in on row hover. The fade is
          driven by the parent group/scroller; on touch devices the
          row works without them via swipe. */}
      <ScrollArrow
        direction="left"
        visible={canScrollLeft}
        onClick={() => scrollBy(-1)}
      />
      <ScrollArrow
        direction="right"
        visible={canScrollRight}
        onClick={() => scrollBy(1)}
      />
    </div>
  );
}

interface ScrollArrowProps {
  direction: "left" | "right";
  visible: boolean;
  onClick: () => void;
}

function ScrollArrow({ direction, visible, onClick }: ScrollArrowProps) {
  if (!visible) return null;
  const isLeft = direction === "left";
  return (
    <button
      type="button"
      onClick={onClick}
      aria-label={isLeft ? "Scroll left" : "Scroll right"}
      className={[
        "hidden md:flex absolute top-0 bottom-0 z-10 w-12 items-center justify-center",
        isLeft ? "left-0" : "right-0",
        "bg-gradient-to-r",
        isLeft
          ? "from-bg-base/80 via-bg-base/40 to-transparent"
          : "from-transparent via-bg-base/40 to-bg-base/80",
        // Fade in on row hover and on focus-visible so keyboard users
        // can still find the control. Slight delay-on-leave keeps the
        // arrow from disappearing as the cursor passes through the
        // gradient strip.
        "opacity-0 transition-opacity duration-200 group-hover/scroller:opacity-100 focus-visible:opacity-100",
      ].join(" ")}
    >
      <span className="flex size-10 items-center justify-center rounded-full bg-black/60 text-white shadow-lg backdrop-blur-sm transition-transform hover:scale-110">
        <svg
          className="size-5"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth={2.5}
          strokeLinecap="round"
          strokeLinejoin="round"
        >
          {isLeft ? (
            <polyline points="15 18 9 12 15 6" />
          ) : (
            <polyline points="9 18 15 12 9 6" />
          )}
        </svg>
      </span>
    </button>
  );
}
