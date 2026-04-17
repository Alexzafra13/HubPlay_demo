import { useEffect, useRef } from "react";
import type { Channel } from "@/api/types";
import { ChannelLogo } from "./ChannelLogo";

interface ChannelStripProps {
  channels: Channel[];
  activeChannel: Channel | null;
  onSelect: (ch: Channel) => void;
}

export function ChannelStrip({ channels, activeChannel, onSelect }: ChannelStripProps) {
  const scrollRef = useRef<HTMLDivElement>(null);
  const activeRef = useRef<HTMLButtonElement>(null);

  // Auto-scroll to active channel whenever it changes.
  useEffect(() => {
    if (activeRef.current && scrollRef.current) {
      const container = scrollRef.current;
      const el = activeRef.current;
      const scrollLeft = el.offsetLeft - container.clientWidth / 2 + el.clientWidth / 2;
      container.scrollTo({ left: scrollLeft, behavior: "smooth" });
    }
  }, [activeChannel?.id]);

  return (
    <div className="relative bg-bg-base/95 backdrop-blur-sm border-b border-white/5">
      <div className="absolute left-0 top-0 bottom-0 w-8 bg-gradient-to-r from-bg-base to-transparent z-10 pointer-events-none" />
      <div className="absolute right-0 top-0 bottom-0 w-8 bg-gradient-to-l from-bg-base to-transparent z-10 pointer-events-none" />

      <div
        ref={scrollRef}
        className="flex items-center gap-1 overflow-x-auto scrollbar-hide py-2 px-6"
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
              className={[
                "shrink-0 flex items-center gap-1.5 px-2.5 py-1.5 rounded-lg transition-all duration-200",
                isActive
                  ? "bg-accent/15 ring-1 ring-accent/50 scale-105"
                  : "bg-transparent hover:bg-white/5 focus-visible:bg-white/5 focus-visible:ring-1 focus-visible:ring-accent/40",
              ].join(" ")}
            >
              <ChannelLogo
                logoUrl={ch.logo_url}
                number={ch.number}
                name={ch.name}
                sizeClassName="w-6 h-6"
                fallbackTextClassName={[
                  "text-[10px] font-bold",
                  isActive ? "bg-accent/20 text-accent" : "bg-white/5",
                ].join(" ")}
                alt=""
              />
              <span
                className={[
                  "text-xs font-medium truncate max-w-[80px]",
                  isActive ? "text-accent" : "text-text-secondary",
                ].join(" ")}
              >
                {ch.name}
              </span>
              {isActive && (
                <span className="w-1.5 h-1.5 rounded-full bg-live animate-pulse shrink-0" />
              )}
            </button>
          );
        })}
      </div>
    </div>
  );
}
