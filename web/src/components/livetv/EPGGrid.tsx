import { useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import type { Channel, EPGProgram } from "@/api/types";
import { ChannelLogo } from "./ChannelLogo";

/**
 * EPGGrid renders a Plex/Jellyfin-style programme guide: channels as rows,
 * time as columns, each programme a block with width proportional to its
 * duration. Scrolls horizontally across a 6-hour window by default; the
 * header (time ruler) and the channel column both stick.
 *
 * Click a programme → jumps to its channel (there's no recording/reminder
 * feature yet, so that's the only sensible action).
 *
 * Performance: rows are plain DOM elements. A 200-channel × 30-programme
 * render is ~6k elements, well within what React can do without virtualising.
 * If we ever need to support 1000+ channels we can swap the rows for
 * react-window — the per-row markup was kept flat to make that future swap
 * a one-line wrapper change.
 */

// Pixels per minute. 8 gives ~3 hours across a 1440-px viewport at 200%
// zoom-out, which keeps programme titles readable.
const PX_PER_MIN = 8;
// Window length we render (minutes). 6 hours.
const WINDOW_MINUTES = 6 * 60;
// Channel column width on the left.
const CHANNEL_COL_WIDTH = 160;
// Header row height (time ruler).
const HEADER_HEIGHT = 32;
// Row height per channel.
const ROW_HEIGHT = 64;

interface EPGGridProps {
  channels: Channel[];
  scheduleByChannel: Record<string, EPGProgram[]>;
  activeChannelId?: string;
  onSelectChannel: (ch: Channel) => void;
  /** Optional: centre the grid on "now" when first rendered. Defaults to true. */
  autoScrollToNow?: boolean;
}

export function EPGGrid({
  channels,
  scheduleByChannel,
  activeChannelId,
  onSelectChannel,
  autoScrollToNow = true,
}: EPGGridProps) {
  const { t } = useTranslation();
  const scrollRef = useRef<HTMLDivElement>(null);
  const [now, setNow] = useState(() => Date.now());

  // Re-tick every 30 s to move the "now" line and refresh which programme is
  // highlighted as "on air". Minute granularity would be enough visually; 30 s
  // hides the occasional scroll jump when a programme ends mid-view.
  useEffect(() => {
    const id = window.setInterval(() => setNow(Date.now()), 30_000);
    return () => window.clearInterval(id);
  }, []);

  // Window: anchor at the most recent half-hour so the grid always starts on
  // a neat gridline regardless of the current clock time.
  const windowStart = useMemo(() => {
    const d = new Date(now);
    d.setMinutes(d.getMinutes() < 30 ? 0 : 30, 0, 0);
    d.setMinutes(d.getMinutes() - 60); // show 1 hour of past
    return d.getTime();
  }, [now]);
  const windowEnd = windowStart + WINDOW_MINUTES * 60_000;
  const timelineWidth = WINDOW_MINUTES * PX_PER_MIN;

  // Time-axis tick marks every 30 minutes.
  const ticks = useMemo(() => {
    const out: { label: string; offsetPx: number }[] = [];
    for (let m = 0; m <= WINDOW_MINUTES; m += 30) {
      const tickTime = new Date(windowStart + m * 60_000);
      out.push({
        label: tickTime.toLocaleTimeString([], {
          hour: "2-digit",
          minute: "2-digit",
        }),
        offsetPx: m * PX_PER_MIN,
      });
    }
    return out;
  }, [windowStart]);

  // "Now" line position; negative if the current time is left of the window.
  const nowLineOffset = ((now - windowStart) / 60_000) * PX_PER_MIN;
  const nowLineVisible = nowLineOffset >= 0 && nowLineOffset <= timelineWidth;

  // Auto-scroll to "now" the first time we render and the scroll container is
  // ready. Later re-ticks do NOT re-scroll — users who scrolled away stay put.
  const hasScrolledRef = useRef(false);
  useEffect(() => {
    if (!autoScrollToNow || hasScrolledRef.current) return;
    const el = scrollRef.current;
    if (!el) return;
    // Leave 1h of past visible on the left; clamp to 0.
    const target = Math.max(0, nowLineOffset - 200);
    el.scrollTo({ left: target, behavior: "auto" });
    hasScrolledRef.current = true;
  }, [autoScrollToNow, nowLineOffset]);

  return (
    <div
      className="relative w-full overflow-hidden rounded-xl border border-white/10 bg-white/[0.02]"
      role="grid"
      aria-label={t("liveTV.epgGridLabel")}
    >
      <div
        ref={scrollRef}
        className="relative overflow-auto"
        style={{ maxHeight: "70vh" }}
      >
        {/* ── Header row: time ruler ─────────────────────────────── */}
        <div
          className="sticky top-0 z-20 flex bg-bg-base/95 backdrop-blur-sm border-b border-white/10"
          style={{ height: HEADER_HEIGHT }}
          role="row"
        >
          {/* Channel column header (sticky corner) */}
          <div
            className="sticky left-0 z-30 shrink-0 flex items-center justify-center bg-bg-base/95 text-[10px] uppercase tracking-wide text-text-muted border-r border-white/10"
            style={{ width: CHANNEL_COL_WIDTH, height: HEADER_HEIGHT }}
            role="columnheader"
          >
            {t("liveTV.channel")}
          </div>
          {/* Tick marks */}
          <div className="relative" style={{ width: timelineWidth, height: HEADER_HEIGHT }}>
            {ticks.map((tick, i) => (
              <div
                key={i}
                className="absolute top-0 bottom-0 flex items-center text-[11px] text-text-muted tabular-nums"
                style={{ left: tick.offsetPx }}
                role="columnheader"
              >
                <span className="border-l border-white/10 pl-1 pr-3 h-full flex items-center">
                  {tick.label}
                </span>
              </div>
            ))}
          </div>
        </div>

        {/* ── Body rows ──────────────────────────────────────────── */}
        <div className="relative">
          {channels.map((channel) => {
            const programs = scheduleByChannel[channel.id] ?? [];
            const isActive = channel.id === activeChannelId;
            return (
              <div
                key={channel.id}
                className={[
                  "flex border-b border-white/5 transition-colors",
                  isActive ? "bg-accent/5" : "hover:bg-white/[0.02]",
                ].join(" ")}
                style={{ height: ROW_HEIGHT }}
                role="row"
              >
                {/* Channel cell (sticky left) */}
                <button
                  type="button"
                  onClick={() => onSelectChannel(channel)}
                  aria-pressed={isActive}
                  className={[
                    "sticky left-0 z-10 shrink-0 flex items-center gap-2 px-3 border-r border-white/10 text-left",
                    isActive ? "bg-accent/10" : "bg-bg-base/95",
                  ].join(" ")}
                  style={{ width: CHANNEL_COL_WIDTH }}
                  role="gridcell"
                >
                  <div className="w-8 h-8 rounded bg-white/5 flex items-center justify-center shrink-0">
                    <ChannelLogo
                      logoUrl={channel.logo_url}
                      number={channel.number}
                      name={channel.name}
                      sizeClassName="w-7 h-7"
                      fallbackTextClassName="text-xs font-bold"
                      alt=""
                    />
                  </div>
                  <div className="min-w-0 flex-1">
                    <div
                      className={[
                        "text-xs font-medium truncate",
                        isActive ? "text-accent" : "text-text-primary",
                      ].join(" ")}
                    >
                      {channel.name}
                    </div>
                    <div className="text-[10px] text-text-muted truncate">
                      Ch. {channel.number}
                    </div>
                  </div>
                </button>

                {/* Programmes track */}
                <div
                  className="relative"
                  style={{ width: timelineWidth, height: ROW_HEIGHT }}
                  role="gridcell"
                >
                  {programs.map((p) => {
                    const start = new Date(p.start_time).getTime();
                    const end = new Date(p.end_time).getTime();
                    // Skip programmes entirely outside the window.
                    if (end <= windowStart || start >= windowEnd) return null;
                    const clampedStart = Math.max(start, windowStart);
                    const clampedEnd = Math.min(end, windowEnd);
                    const left = ((clampedStart - windowStart) / 60_000) * PX_PER_MIN;
                    const width = Math.max(
                      ((clampedEnd - clampedStart) / 60_000) * PX_PER_MIN - 2,
                      40,
                    );
                    const isAiring = start <= now && end > now;
                    return (
                      <button
                        key={p.id || `${channel.id}-${p.start_time}`}
                        type="button"
                        onClick={() => onSelectChannel(channel)}
                        title={`${p.title}\n${new Date(p.start_time).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })} – ${new Date(p.end_time).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })}`}
                        className={[
                          "absolute top-1 bottom-1 rounded-md px-2 py-1 text-left overflow-hidden transition-colors",
                          isAiring
                            ? "bg-accent/20 hover:bg-accent/30 ring-1 ring-accent/40"
                            : "bg-white/5 hover:bg-white/10",
                        ].join(" ")}
                        style={{ left, width }}
                        aria-label={`${p.title} on ${channel.name}`}
                      >
                        <div
                          className={[
                            "text-[11px] font-medium truncate",
                            isAiring ? "text-text-primary" : "text-text-secondary",
                          ].join(" ")}
                        >
                          {p.title}
                        </div>
                        {p.category && (
                          <div className="text-[9px] text-text-muted truncate">
                            {p.category}
                          </div>
                        )}
                      </button>
                    );
                  })}

                  {/* Fallback when a channel has no EPG data in window */}
                  {programs.length === 0 && (
                    <div className="absolute inset-y-2 left-2 right-2 flex items-center px-2 text-[10px] text-text-muted/60 italic">
                      {t("liveTV.noEPG")}
                    </div>
                  )}
                </div>
              </div>
            );
          })}

          {/* "Now" line — rendered once, stretches full body height via sticky
              positioning on the wrapper so it floats above the programme
              cells but under the channel column. */}
          {nowLineVisible && (
            <div
              className="pointer-events-none absolute top-0 bottom-0 z-[5]"
              style={{
                left: CHANNEL_COL_WIDTH + nowLineOffset,
                width: 2,
              }}
              aria-hidden="true"
            >
              <div className="h-full w-0.5 bg-live shadow-[0_0_8px_rgba(255,0,80,0.5)]" />
              <div className="absolute -top-1 -left-1 w-2 h-2 rounded-full bg-live" />
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
