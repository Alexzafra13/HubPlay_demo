import { useEffect, useRef } from "react";
import type { Channel } from "@/api/types";
import { ChannelLogo } from "./ChannelLogo";

interface ChannelStripProps {
  channels: Channel[];
  activeChannel: Channel | null;
  onSelect: (ch: Channel) => void;
}

/**
 * Horizontal zapping rail: compact logo-first tiles that keep the active
 * channel centred as the user zaps with arrows / number keys. Sits directly
 * under the "now playing" card so the user can jump around without leaving
 * the hero area.
 */
export function ChannelStrip({
  channels,
  activeChannel,
  onSelect,
}: ChannelStripProps) {
  const scrollRef = useRef<HTMLDivElement>(null);
  const activeRef = useRef<HTMLButtonElement>(null);

  useEffect(() => {
    if (!activeRef.current || !scrollRef.current) return;
    const container = scrollRef.current;
    const el = activeRef.current;
    const scrollLeft =
      el.offsetLeft - container.clientWidth / 2 + el.clientWidth / 2;
    container.scrollTo({ left: scrollLeft, behavior: "smooth" });
  }, [activeChannel?.id]);

  return (
    <div className="relative">
      <div className="pointer-events-none absolute inset-y-0 left-0 z-10 w-10 bg-gradient-to-r from-bg-base to-transparent" />
      <div className="pointer-events-none absolute inset-y-0 right-0 z-10 w-10 bg-gradient-to-l from-bg-base to-transparent" />

      <div
        ref={scrollRef}
        className="scrollbar-hide flex items-center gap-2 overflow-x-auto px-4 py-2 md:px-6"
      >
        {channels.map((ch) => {
          const isActive = activeChannel?.id === ch.id;
          return (
            <button
              key={ch.id}
              ref={isActive ? activeRef : null}
              type="button"
              onClick={() => onSelect(ch)}
              title={ch.name}
              aria-pressed={isActive}
              aria-label={ch.name}
              className={[
                "group relative flex h-14 w-14 shrink-0 items-center justify-center rounded-xl transition-all duration-200 sm:h-16 sm:w-16",
                isActive
                  ? "bg-accent/15 ring-2 ring-accent shadow-lg shadow-accent/20 scale-105"
                  : "bg-white/[0.04] ring-1 ring-white/5 hover:bg-white/[0.08] hover:ring-white/15 focus-visible:ring-accent/60",
              ].join(" ")}
            >
              <ChannelLogo
                logoUrl={ch.logo_url}
                number={ch.number}
                name={ch.name}
                sizeClassName="w-10 h-10 sm:w-11 sm:h-11"
                fallbackTextClassName="text-sm font-bold"
                alt=""
              />
              <span className="absolute -bottom-1 left-1/2 -translate-x-1/2 rounded-full bg-black/60 px-1 py-0 text-[9px] font-semibold tabular-nums text-white/80 backdrop-blur-sm">
                {ch.number}
              </span>
              {isActive && (
                <span className="absolute -right-1 -top-1 h-2.5 w-2.5 animate-pulse rounded-full bg-live ring-2 ring-bg-base" />
              )}
            </button>
          );
        })}
      </div>
    </div>
  );
}
