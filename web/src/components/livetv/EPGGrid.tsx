import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import type { Channel, EPGProgram } from "@/api/types";
import { ChannelLogo } from "./ChannelLogo";
import { categoryMeta, parseCategory } from "./categoryHelpers";
import { formatTime } from "./epgHelpers";

/**
 * EPGGrid renders a Plex/Jellyfin-style programme guide: channels as rows,
 * time as columns, each programme a block with width proportional to its
 * duration. A 6-hour window scrolls horizontally; the time ruler and the
 * channel column are both sticky.
 *
 * Professional TV guides (Plex, Sky Q, YouTube TV, Xfinity) all share three
 * ingredients we model here:
 *   1. A time toolbar that lets the user jump the window forward, back, or
 *      snap to "now" / "primetime" — they rarely want to scrub by hand.
 *   2. A programme-detail popover (title, synopsis, time, category) that
 *      opens on click, so tapping a block doesn't immediately tune away.
 *   3. A persistent "now" line with the current time displayed.
 *
 * Performance: rows are plain DOM elements. A 200-channel × 30-programme
 * render is ~6k elements. If we ever need to support 1000+ channels we can
 * swap the rows for react-window — the per-row markup was kept flat to make
 * that future swap a one-line wrapper change.
 */

const PX_PER_MIN = 8;
const WINDOW_MINUTES = 6 * 60;
const CHANNEL_COL_WIDTH = 160;
const HEADER_HEIGHT = 36;
const ROW_HEIGHT = 64;
const PRIMETIME_HOUR = 20;

interface EPGGridProps {
  channels: Channel[];
  scheduleByChannel: Record<string, EPGProgram[]>;
  activeChannelId?: string;
  onSelectChannel: (ch: Channel) => void;
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
  // Minutes added to the auto-anchored window start. 0 = follow "now".
  const [timeOffset, setTimeOffset] = useState(0);
  const [selected, setSelected] = useState<{
    program: EPGProgram;
    channel: Channel;
  } | null>(null);

  // Re-tick every 30 s to keep the "now" line and "on air" highlights fresh.
  useEffect(() => {
    const id = window.setInterval(() => setNow(Date.now()), 30_000);
    return () => window.clearInterval(id);
  }, []);

  // Base anchor: most recent half-hour boundary minus 1 h of past, so the
  // grid always starts on a neat gridline regardless of clock time.
  const baseStart = useMemo(() => {
    const d = new Date(now);
    d.setMinutes(d.getMinutes() < 30 ? 0 : 30, 0, 0);
    d.setMinutes(d.getMinutes() - 60);
    return d.getTime();
  }, [now]);

  const windowStart = baseStart + timeOffset * 60_000;
  const windowEnd = windowStart + WINDOW_MINUTES * 60_000;
  const timelineWidth = WINDOW_MINUTES * PX_PER_MIN;

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

  const nowLineOffset = ((now - windowStart) / 60_000) * PX_PER_MIN;
  const nowLineVisible = nowLineOffset >= 0 && nowLineOffset <= timelineWidth;

  // Auto-scroll to "now" the first time we render and whenever the user
  // presses the "Now" button (which zeros the offset).
  const hasScrolledRef = useRef(false);
  useEffect(() => {
    if (!autoScrollToNow) return;
    const el = scrollRef.current;
    if (!el) return;
    if (!hasScrolledRef.current) {
      const target = Math.max(0, nowLineOffset - 200);
      el.scrollTo({ left: target, behavior: "auto" });
      hasScrolledRef.current = true;
    }
  }, [autoScrollToNow, nowLineOffset]);

  // Time-toolbar handlers.
  const jumpToNow = useCallback(() => {
    setTimeOffset(0);
    const el = scrollRef.current;
    if (el) {
      const target = Math.max(0, nowLineOffset - 200);
      el.scrollTo({ left: target, behavior: "smooth" });
    }
  }, [nowLineOffset]);

  const shiftWindow = useCallback((deltaMin: number) => {
    setTimeOffset((o) => o + deltaMin);
  }, []);

  const jumpToPrimetime = useCallback(() => {
    // Snap to today's 20:00 local. Compute the offset from baseStart.
    const prime = new Date(now);
    prime.setHours(PRIMETIME_HOUR, 0, 0, 0);
    // If 20:00 already passed today, go to tomorrow's primetime.
    if (prime.getTime() <= now - 60 * 60_000) {
      prime.setDate(prime.getDate() + 1);
    }
    const offsetMin = Math.round((prime.getTime() - baseStart) / 60_000) - 30;
    setTimeOffset(offsetMin);
    const el = scrollRef.current;
    if (el) {
      // 30-minute lead-in so the hour tick is visible on the left.
      el.scrollTo({ left: 0, behavior: "smooth" });
    }
  }, [baseStart, now]);

  const rangeLabel = useMemo(() => {
    const start = formatTime(new Date(windowStart).toISOString());
    const end = formatTime(new Date(windowEnd).toISOString());
    const dayLabel = new Date(windowStart).toLocaleDateString(undefined, {
      weekday: "short",
      day: "numeric",
      month: "short",
    });
    return { start, end, dayLabel };
  }, [windowStart, windowEnd]);

  const currentClock = useMemo(
    () =>
      new Date(now).toLocaleTimeString([], {
        hour: "2-digit",
        minute: "2-digit",
      }),
    [now],
  );

  return (
    <div className="flex flex-col gap-3">
      {/* ── Time-navigation toolbar ────────────────────────────── */}
      <div className="flex flex-wrap items-center gap-2 rounded-xl border border-white/10 bg-white/[0.02] px-3 py-2">
        <button
          type="button"
          onClick={jumpToNow}
          disabled={timeOffset === 0}
          className={[
            "inline-flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-xs font-semibold transition-all",
            timeOffset === 0
              ? "bg-live/15 text-live ring-1 ring-live/40 cursor-default"
              : "bg-accent text-white hover:bg-accent-hover shadow-sm shadow-accent/20",
          ].join(" ")}
        >
          <span className="h-1.5 w-1.5 animate-pulse rounded-full bg-current" />
          {t("liveTV.nowLabel")}
          <span className="ml-1 font-normal tabular-nums opacity-70">
            {currentClock}
          </span>
        </button>

        <div className="flex overflow-hidden rounded-lg border border-white/10">
          <button
            type="button"
            onClick={() => shiftWindow(-120)}
            aria-label={t("liveTV.shiftBack")}
            className="bg-white/5 px-2.5 py-1.5 text-xs font-semibold text-text-secondary transition-colors hover:bg-white/10 hover:text-text-primary"
          >
            − 2h
          </button>
          <button
            type="button"
            onClick={() => shiftWindow(120)}
            aria-label={t("liveTV.shiftForward")}
            className="border-l border-white/10 bg-white/5 px-2.5 py-1.5 text-xs font-semibold text-text-secondary transition-colors hover:bg-white/10 hover:text-text-primary"
          >
            + 2h
          </button>
        </div>

        <button
          type="button"
          onClick={jumpToPrimetime}
          className="inline-flex items-center gap-1 rounded-lg border border-white/10 bg-white/5 px-2.5 py-1.5 text-xs font-semibold text-text-secondary transition-colors hover:bg-white/10 hover:text-text-primary"
        >
          <span aria-hidden="true">🌙</span>
          {t("liveTV.primetime")}
        </button>

        <div className="ml-auto flex items-baseline gap-2 text-[11px] md:text-xs">
          <span className="font-medium capitalize text-text-secondary">
            {rangeLabel.dayLabel}
          </span>
          <span className="tabular-nums text-text-muted">
            {rangeLabel.start} — {rangeLabel.end}
          </span>
        </div>
      </div>

      {/* ── Grid ──────────────────────────────────────────────── */}
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
          {/* ── Header row: time ruler ─────────────────────────── */}
          <div
            className="sticky top-0 z-20 flex border-b border-white/10 bg-bg-base/95 backdrop-blur-sm"
            style={{ height: HEADER_HEIGHT }}
            role="row"
          >
            <div
              className="sticky left-0 z-30 flex shrink-0 items-center justify-center border-r border-white/10 bg-bg-base/95 text-[10px] uppercase tracking-wide text-text-muted"
              style={{ width: CHANNEL_COL_WIDTH, height: HEADER_HEIGHT }}
              role="columnheader"
            >
              {t("liveTV.channel")}
            </div>
            <div
              className="relative"
              style={{ width: timelineWidth, height: HEADER_HEIGHT }}
            >
              {ticks.map((tick, i) => (
                <div
                  key={i}
                  className="absolute bottom-0 top-0 flex items-center text-[11px] tabular-nums text-text-muted"
                  style={{ left: tick.offsetPx }}
                  role="columnheader"
                >
                  <span className="flex h-full items-center border-l border-white/10 pl-1 pr-3 font-medium">
                    {tick.label}
                  </span>
                </div>
              ))}

              {/* "Now" marker inside the header so the current time is
                  always visible even if the user scrolls the ruler. */}
              {nowLineVisible && (
                <div
                  className="pointer-events-none absolute inset-y-0 z-10 flex items-center"
                  style={{ left: nowLineOffset - 22 }}
                  aria-hidden="true"
                >
                  <span className="flex items-center gap-1 rounded-md bg-live px-1.5 py-0.5 text-[9px] font-bold uppercase tracking-wider text-white shadow-md shadow-live/40">
                    <span className="h-1.5 w-1.5 animate-pulse rounded-full bg-white" />
                    {currentClock}
                  </span>
                </div>
              )}
            </div>
          </div>

          {/* ── Body rows ────────────────────────────────────── */}
          <div className="relative">
            {channels.map((channel) => {
              const programs = scheduleByChannel[channel.id] ?? [];
              const isActive = channel.id === activeChannelId;
              const chCategory = parseCategory(channel.group);
              const chMeta = categoryMeta(chCategory.primary);
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
                  <button
                    type="button"
                    onClick={() => onSelectChannel(channel)}
                    aria-pressed={isActive}
                    className={[
                      "sticky left-0 z-10 flex shrink-0 items-center gap-2 border-r border-white/10 px-3 text-left",
                      isActive ? "bg-accent/10" : "bg-bg-base/95",
                    ].join(" ")}
                    style={{ width: CHANNEL_COL_WIDTH }}
                    role="gridcell"
                  >
                    <div
                      className={[
                        "flex h-9 w-9 shrink-0 items-center justify-center rounded-lg",
                        chMeta.tint,
                      ].join(" ")}
                    >
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
                          "truncate text-xs font-semibold",
                          isActive ? "text-accent" : "text-text-primary",
                        ].join(" ")}
                      >
                        {channel.name}
                      </div>
                      <div className="flex items-center gap-1 text-[10px] text-text-muted">
                        <span className="tabular-nums">CH.{channel.number}</span>
                        <span aria-hidden="true">·</span>
                        <span className="truncate">{chCategory.primary}</span>
                      </div>
                    </div>
                  </button>

                  <div
                    className="relative"
                    style={{ width: timelineWidth, height: ROW_HEIGHT }}
                    role="gridcell"
                  >
                    {programs.map((p) => {
                      const start = new Date(p.start_time).getTime();
                      const end = new Date(p.end_time).getTime();
                      if (end <= windowStart || start >= windowEnd) return null;
                      const clampedStart = Math.max(start, windowStart);
                      const clampedEnd = Math.min(end, windowEnd);
                      const left =
                        ((clampedStart - windowStart) / 60_000) * PX_PER_MIN;
                      const width = Math.max(
                        ((clampedEnd - clampedStart) / 60_000) * PX_PER_MIN - 2,
                        40,
                      );
                      const isAiring = start <= now && end > now;
                      const progMeta = p.category
                        ? categoryMeta(p.category)
                        : chMeta;
                      return (
                        <button
                          key={p.id || `${channel.id}-${p.start_time}`}
                          type="button"
                          onClick={() =>
                            setSelected({ program: p, channel })
                          }
                          className={[
                            "absolute bottom-1 top-1 overflow-hidden rounded-lg px-2 py-1 text-left transition-all",
                            isAiring
                              ? `${progMeta.accent} ring-1 shadow-sm hover:brightness-110`
                              : "border border-white/5 bg-white/[0.04] hover:bg-white/[0.09]",
                          ].join(" ")}
                          style={{ left, width }}
                          aria-label={`${p.title} on ${channel.name}`}
                        >
                          <div
                            className={[
                              "truncate text-[11px] font-semibold",
                              isAiring ? "" : "text-text-secondary",
                            ].join(" ")}
                          >
                            {p.title}
                          </div>
                          {p.category && (
                            <div className="flex items-center gap-1 truncate text-[9px] text-text-muted">
                              <span aria-hidden="true">{progMeta.icon}</span>
                              <span className="truncate">{p.category}</span>
                            </div>
                          )}
                        </button>
                      );
                    })}

                    {programs.length === 0 && (
                      <div className="absolute inset-y-2 left-2 right-2 flex items-center px-2 text-[10px] italic text-text-muted/60">
                        {t("liveTV.noEPG")}
                      </div>
                    )}
                  </div>
                </div>
              );
            })}

            {nowLineVisible && (
              <div
                className="pointer-events-none absolute bottom-0 top-0 z-[5]"
                style={{
                  left: CHANNEL_COL_WIDTH + nowLineOffset,
                  width: 2,
                }}
                aria-hidden="true"
              >
                <div className="h-full w-0.5 bg-gradient-to-b from-live via-live/80 to-live/60 shadow-[0_0_12px_rgba(239,68,68,0.7)]" />
              </div>
            )}
          </div>
        </div>
      </div>

      {/* ── Program-detail popover ────────────────────────────── */}
      {selected && (
        <ProgramDetailPopover
          program={selected.program}
          channel={selected.channel}
          now={now}
          onClose={() => setSelected(null)}
          onWatch={() => {
            onSelectChannel(selected.channel);
            setSelected(null);
          }}
        />
      )}
    </div>
  );
}

interface ProgramDetailPopoverProps {
  program: EPGProgram;
  channel: Channel;
  now: number;
  onClose: () => void;
  onWatch: () => void;
}

function ProgramDetailPopover({
  program,
  channel,
  now,
  onClose,
  onWatch,
}: ProgramDetailPopoverProps) {
  const { t } = useTranslation();
  const chCategory = parseCategory(channel.group);
  const progMeta = categoryMeta(program.category ?? chCategory.primary);
  const start = new Date(program.start_time).getTime();
  const end = new Date(program.end_time).getTime();
  const airing = start <= now && end > now;

  // Close on Escape — standard dialog affordance.
  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      if (e.key === "Escape") onClose();
    }
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose]);

  return (
    <div
      className="fixed inset-0 z-50 flex items-end justify-center bg-black/70 p-4 backdrop-blur-sm md:items-center"
      role="dialog"
      aria-modal="true"
      aria-labelledby="program-detail-title"
      onClick={onClose}
    >
      <div
        className="w-full max-w-xl overflow-hidden rounded-2xl border border-white/10 bg-bg-card shadow-2xl"
        onClick={(e) => e.stopPropagation()}
      >
        <div
          className={[
            "relative flex items-start gap-3 border-b border-white/10 p-5",
            progMeta.tint,
          ].join(" ")}
        >
          <div className="flex h-14 w-14 shrink-0 items-center justify-center rounded-xl bg-black/20 backdrop-blur-sm">
            <ChannelLogo
              logoUrl={channel.logo_url}
              number={channel.number}
              name={channel.name}
              sizeClassName="w-11 h-11"
              fallbackTextClassName="text-base font-bold"
            />
          </div>
          <div className="min-w-0 flex-1">
            <div className="flex items-center gap-2 text-[11px] font-semibold uppercase tracking-wider text-white/70">
              <span>CH.{channel.number}</span>
              <span aria-hidden="true">·</span>
              <span className="truncate">{channel.name}</span>
              {airing && (
                <span className="flex items-center gap-1 rounded-md bg-live/90 px-1.5 py-0.5 text-[10px] text-white shadow-sm">
                  <span className="h-1.5 w-1.5 animate-pulse rounded-full bg-white" />
                  {t("liveTV.live")}
                </span>
              )}
            </div>
            <h3
              id="program-detail-title"
              className="mt-1 text-lg font-bold text-text-primary md:text-xl"
            >
              {program.title}
            </h3>
            <div className="mt-1 flex items-center gap-2 text-xs text-text-secondary">
              <span className="tabular-nums">
                {formatTime(program.start_time)} — {formatTime(program.end_time)}
              </span>
              {program.category && (
                <>
                  <span aria-hidden="true">·</span>
                  <span className="inline-flex items-center gap-1">
                    <span aria-hidden="true">{progMeta.icon}</span>
                    {program.category}
                  </span>
                </>
              )}
            </div>
          </div>
          <button
            type="button"
            onClick={onClose}
            aria-label={t("common.close")}
            className="shrink-0 rounded-lg p-1.5 text-white/70 transition-colors hover:bg-black/30 hover:text-white"
          >
            <svg
              width="18"
              height="18"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              strokeWidth="2"
              strokeLinecap="round"
              strokeLinejoin="round"
              aria-hidden="true"
            >
              <path d="M18 6L6 18M6 6l12 12" />
            </svg>
          </button>
        </div>

        <div className="p-5">
          {program.description ? (
            <p className="text-sm leading-relaxed text-text-secondary">
              {program.description}
            </p>
          ) : (
            <p className="text-sm italic text-text-muted">
              {t("liveTV.noDescription")}
            </p>
          )}

          <div className="mt-5 flex items-center justify-end gap-2">
            <button
              type="button"
              onClick={onClose}
              className="rounded-lg border border-white/10 px-4 py-2 text-sm font-medium text-text-secondary transition-colors hover:bg-white/5 hover:text-text-primary"
            >
              {t("common.close")}
            </button>
            <button
              type="button"
              onClick={onWatch}
              className="inline-flex items-center gap-1.5 rounded-lg bg-accent px-4 py-2 text-sm font-semibold text-white shadow-sm shadow-accent/20 transition-colors hover:bg-accent-hover"
            >
              <svg
                width="14"
                height="14"
                viewBox="0 0 24 24"
                fill="currentColor"
                aria-hidden="true"
              >
                <path d="M8 5v14l11-7z" />
              </svg>
              {t("liveTV.watchChannel")}
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}
