import { useCallback, useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import type { Channel } from "@/api/types";
import { ChannelLogo } from "./ChannelLogo";

interface ChannelStripProps {
  channels: Channel[];
  activeChannel: Channel | null;
  onSelect: (ch: Channel) => void;
}

/**
 * Horizontal zapping rail. Three interaction models, matched to device:
 *
 *   • Touch: native swipe + snap-to-tile so flings land cleanly.
 *   • Desktop mouse: grab-and-drag with pointer capture, plus left/right
 *     nav buttons that paginate by roughly one viewport.
 *   • Keyboard / remote: the parent drives the active channel through
 *     arrow keys and we scroll that tile into view.
 */
export function ChannelStrip({
  channels,
  activeChannel,
  onSelect,
}: ChannelStripProps) {
  const { t } = useTranslation();
  const scrollRef = useRef<HTMLDivElement>(null);
  const activeRef = useRef<HTMLButtonElement>(null);
  const [canScrollLeft, setCanScrollLeft] = useState(false);
  const [canScrollRight, setCanScrollRight] = useState(false);

  // We track drag state in refs because React re-renders aren't needed —
  // the event handlers close over them synchronously and the DOM scroll
  // position updates imperatively.
  const dragState = useRef<{
    isDown: boolean;
    startX: number;
    startScroll: number;
    moved: boolean;
  }>({ isDown: false, startX: 0, startScroll: 0, moved: false });

  const updateScrollAffordances = useCallback(() => {
    const el = scrollRef.current;
    if (!el) return;
    // 2px tolerance avoids flickering at exact edges.
    setCanScrollLeft(el.scrollLeft > 2);
    setCanScrollRight(el.scrollLeft + el.clientWidth < el.scrollWidth - 2);
  }, []);

  useEffect(() => {
    updateScrollAffordances();
    const el = scrollRef.current;
    if (!el) return;
    const onScroll = () => updateScrollAffordances();
    el.addEventListener("scroll", onScroll, { passive: true });
    window.addEventListener("resize", updateScrollAffordances);
    return () => {
      el.removeEventListener("scroll", onScroll);
      window.removeEventListener("resize", updateScrollAffordances);
    };
  }, [updateScrollAffordances, channels.length]);

  // Centre the active tile whenever the user zaps to a different channel.
  useEffect(() => {
    if (!activeRef.current || !scrollRef.current) return;
    const container = scrollRef.current;
    const el = activeRef.current;
    const scrollLeft =
      el.offsetLeft - container.clientWidth / 2 + el.clientWidth / 2;
    container.scrollTo({ left: scrollLeft, behavior: "smooth" });
  }, [activeChannel?.id]);

  const scrollByPage = useCallback((dir: 1 | -1) => {
    const el = scrollRef.current;
    if (!el) return;
    // Leave ~10% overlap between pages so the user doesn't lose visual
    // context of the channel they were just inspecting.
    const step = Math.max(200, el.clientWidth * 0.9);
    el.scrollBy({ left: dir * step, behavior: "smooth" });
  }, []);

  // ── Drag-to-scroll (desktop pointer). On touch we let the native
  //    scroll + snap handle things — desktop browsers don't do that,
  //    which is why this handler exists.
  const onPointerDown = useCallback((e: React.PointerEvent<HTMLDivElement>) => {
    if (e.pointerType === "touch") return;
    const el = scrollRef.current;
    if (!el) return;
    dragState.current = {
      isDown: true,
      startX: e.clientX,
      startScroll: el.scrollLeft,
      moved: false,
    };
    el.setPointerCapture(e.pointerId);
  }, []);

  const onPointerMove = useCallback((e: React.PointerEvent<HTMLDivElement>) => {
    const st = dragState.current;
    if (!st.isDown) return;
    const el = scrollRef.current;
    if (!el) return;
    const dx = e.clientX - st.startX;
    if (Math.abs(dx) > 4) st.moved = true;
    el.scrollLeft = st.startScroll - dx;
  }, []);

  const endDrag = useCallback((e: React.PointerEvent<HTMLDivElement>) => {
    const st = dragState.current;
    if (!st.isDown) return;
    st.isDown = false;
    const el = scrollRef.current;
    if (el && el.hasPointerCapture(e.pointerId)) {
      el.releasePointerCapture(e.pointerId);
    }
  }, []);

  return (
    <div className="relative">
      {/* Edge fades hint at overflow. Hidden on the sides that can't
          scroll so the rail doesn't look clipped at the ends. */}
      {canScrollLeft && (
        <div className="pointer-events-none absolute inset-y-0 left-0 z-10 w-12 bg-gradient-to-r from-bg-base to-transparent" />
      )}
      {canScrollRight && (
        <div className="pointer-events-none absolute inset-y-0 right-0 z-10 w-12 bg-gradient-to-l from-bg-base to-transparent" />
      )}

      {/* Nav buttons — desktop only. Touch users swipe directly. */}
      <button
        type="button"
        onClick={() => scrollByPage(-1)}
        disabled={!canScrollLeft}
        aria-label={t("liveTV.scrollLeft")}
        className={[
          "absolute left-2 top-1/2 z-20 hidden -translate-y-1/2 items-center justify-center rounded-full bg-black/60 p-2 text-white shadow-lg backdrop-blur-md transition-all md:flex",
          canScrollLeft
            ? "opacity-100 hover:bg-black/80"
            : "pointer-events-none opacity-0",
        ].join(" ")}
      >
        <svg
          width="18"
          height="18"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="2.5"
          strokeLinecap="round"
          strokeLinejoin="round"
          aria-hidden="true"
        >
          <path d="M15 18l-6-6 6-6" />
        </svg>
      </button>
      <button
        type="button"
        onClick={() => scrollByPage(1)}
        disabled={!canScrollRight}
        aria-label={t("liveTV.scrollRight")}
        className={[
          "absolute right-2 top-1/2 z-20 hidden -translate-y-1/2 items-center justify-center rounded-full bg-black/60 p-2 text-white shadow-lg backdrop-blur-md transition-all md:flex",
          canScrollRight
            ? "opacity-100 hover:bg-black/80"
            : "pointer-events-none opacity-0",
        ].join(" ")}
      >
        <svg
          width="18"
          height="18"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="2.5"
          strokeLinecap="round"
          strokeLinejoin="round"
          aria-hidden="true"
        >
          <path d="M9 18l6-6-6-6" />
        </svg>
      </button>

      <div
        ref={scrollRef}
        onPointerDown={onPointerDown}
        onPointerMove={onPointerMove}
        onPointerUp={endDrag}
        onPointerCancel={endDrag}
        onPointerLeave={endDrag}
        role="listbox"
        aria-label={t("liveTV.channelStrip")}
        className="scrollbar-hide flex snap-x snap-mandatory items-center gap-3 overflow-x-auto px-4 py-3 md:px-12 md:py-4 md:cursor-grab md:active:cursor-grabbing select-none"
      >
        {channels.map((ch) => {
          const isActive = activeChannel?.id === ch.id;
          return (
            <button
              key={ch.id}
              ref={isActive ? activeRef : null}
              type="button"
              onClick={(e) => {
                // Suppress click if the user was dragging the rail so the
                // drag gesture doesn't accidentally zap channels.
                if (dragState.current.moved) {
                  e.preventDefault();
                  dragState.current.moved = false;
                  return;
                }
                onSelect(ch);
              }}
              title={ch.name}
              role="option"
              aria-selected={isActive}
              aria-label={ch.name}
              className={[
                "group relative flex h-20 w-20 shrink-0 snap-center items-center justify-center rounded-2xl transition-all duration-200 sm:h-24 sm:w-24",
                isActive
                  ? "bg-accent/15 ring-2 ring-accent shadow-xl shadow-accent/25 scale-105"
                  : "bg-white/[0.04] ring-1 ring-white/5 hover:bg-white/[0.08] hover:ring-white/20 focus-visible:ring-accent/60",
              ].join(" ")}
            >
              <ChannelLogo
                logoUrl={ch.logo_url}
                number={ch.number}
                name={ch.name}
                sizeClassName="w-14 h-14 sm:w-16 sm:h-16 pointer-events-none"
                fallbackTextClassName="text-lg font-bold"
                alt=""
              />
              <span className="absolute -bottom-1 left-1/2 -translate-x-1/2 rounded-full bg-black/70 px-1.5 py-0.5 text-[10px] font-semibold tabular-nums text-white/90 backdrop-blur-sm ring-1 ring-white/10">
                {ch.number}
              </span>
              {isActive && (
                <span className="absolute -right-1 -top-1 h-3 w-3 animate-pulse rounded-full bg-live ring-2 ring-bg-base" />
              )}
            </button>
          );
        })}
      </div>
    </div>
  );
}
