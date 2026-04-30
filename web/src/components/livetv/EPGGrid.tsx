import { useCallback, useEffect, useMemo, useRef } from "react";
import { useTranslation } from "react-i18next";
import { useVirtualizer } from "@tanstack/react-virtual";
import type { Channel, EPGProgram } from "@/api/types";
import { useNowTick } from "@/hooks/useNowTick";
import { ChannelLogo } from "./ChannelLogo";

/**
 * EPGGrid — Plex/Jellyfin-style programme guide for Live TV.
 *
 * Layout: a single horizontally scrolling container. The channel column
 * and the hours ruler are both sticky (`position: sticky`) — the ruler
 * pins to the top, the channel column pins to the left. The "now" line
 * is an absolutely positioned overlay spanning all rows.
 *
 * Sizing constants mirror the /diseño/ prototype: 160 px/h (so 15 min =
 * 40 px) on a 24-hour window anchored at midnight local. Auto-scrolls to
 * a few pixels before "now" on first render; later `now` re-ticks do not
 * re-scroll (respects user's browsing position).
 *
 * Click a programme → opens the player for its channel. There's no
 * dedicated "programme detail" surface yet; when there is, swap the
 * handler for one that routes to that instead.
 *
 * Performance: the row layer is virtualised with @tanstack/react-virtual
 * once the channel count crosses VIRTUALIZE_THRESHOLD. Below that, we
 * render every row directly — fewer moving parts, friendlier for tests
 * and screen readers, and the DOM cost is trivial at typical homelab
 * sizes (20-50 channels). Above the threshold (large IPTV catalogues:
 * 5k+ channels) only the visible rows + overscan are mounted, so the
 * grid stays responsive regardless of catalogue size.
 */
const VIRTUALIZE_THRESHOLD = 50;

// Design-system constants. Changing these cascades through the whole grid;
// keep them all in one place so the geometry stays consistent.
const PX_PER_HOUR = 160;
const HOURS_IN_WINDOW = 24;
const CHANNEL_COL_WIDTH = 220;
const HEADER_HEIGHT = 36;
const ROW_HEIGHT = 68;

const TIMELINE_WIDTH = HOURS_IN_WINDOW * PX_PER_HOUR;
const PX_PER_MS = PX_PER_HOUR / (60 * 60 * 1000);

interface EPGGridProps {
  channels: Channel[];
  scheduleByChannel: Record<string, EPGProgram[]>;
  activeChannelId?: string;
  onSelectChannel: (ch: Channel) => void;
  /**
   * Optional handler fired when the user clicks a programme block
   * (NOT the sticky channel cell on the left). When omitted the
   * grid falls back to onSelectChannel — preserves the pre-modal
   * "click anywhere on the row → play channel" UX.
   *
   * Wired by LiveTV.tsx to open ProgramDetailModal so the user can
   * read sinopsis / up-next before committing to a zap.
   */
  onSelectProgram?: (channel: Channel, program: EPGProgram) => void;
  /** Centre the grid on "now" when first rendered. Defaults to true. */
  autoScrollToNow?: boolean;
}

export function EPGGrid({
  channels,
  scheduleByChannel,
  activeChannelId,
  onSelectChannel,
  onSelectProgram,
  autoScrollToNow = true,
}: EPGGridProps) {
  const { t } = useTranslation();
  const scrollRef = useRef<HTMLDivElement>(null);

  // Virtualisation of the row layer is opt-in by channel count. The
  // hook lives in a child component (VirtualizedRows) so it only
  // mounts above threshold — keeps tests on the simple path and
  // avoids ResizeObserver overhead for small catalogues.
  const shouldVirtualize = channels.length >= VIRTUALIZE_THRESHOLD;

  // 30 s cadence: smooth enough for the now-line to creep without jumping,
  // cheap enough to ignore. Minute granularity is fine visually but 30 s
  // hides the ragged edge when a programme ends mid-view.
  const now = useNowTick(30_000);

  // Window anchored at midnight local. The grid spans 24 h; whatever day
  // the user is looking at is determined by the scroll position, not by
  // a date selector. A day selector (Ayer / Hoy / Mañana) is a reasonable
  // Phase-5 addition — slots in cleanly by re-anchoring `windowStart`.
  const windowStart = useMemo(() => {
    const d = new Date(now);
    d.setHours(0, 0, 0, 0);
    return d.getTime();
  }, [now]);
  const windowEnd = windowStart + HOURS_IN_WINDOW * 60 * 60 * 1000;

  // Hour labels along the top ruler.
  const hours = useMemo(() => {
    const out: { hour: number; label: string; isNow: boolean }[] = [];
    const nowHour = new Date(now).getHours();
    for (let h = 0; h < HOURS_IN_WINDOW; h++) {
      out.push({
        hour: h,
        label: `${String(h).padStart(2, "0")}:00`,
        isNow: h === nowHour,
      });
    }
    return out;
  }, [now]);

  // Now-line X position. Clamped out-of-range to hide the line when the
  // user has scrolled past midnight or before it (shouldn't happen on the
  // current day but the math stays honest).
  const nowLineOffset = (now - windowStart) * PX_PER_MS;
  const nowLineVisible =
    nowLineOffset >= 0 && nowLineOffset <= TIMELINE_WIDTH;

  const nowLabel = useMemo(() => {
    const d = new Date(now);
    return `${String(d.getHours()).padStart(2, "0")}:${String(
      d.getMinutes(),
    ).padStart(2, "0")}`;
  }, [now]);

  // One-shot auto-scroll to "now" on first render. Subsequent re-ticks do
  // not re-scroll — if the user scrolled away on purpose, we respect that.
  const hasScrolledRef = useRef(false);
  useEffect(() => {
    if (!autoScrollToNow || hasScrolledRef.current) return;
    const el = scrollRef.current;
    if (!el) return;
    const target = Math.max(0, nowLineOffset - 120);
    el.scrollTo({ left: target, behavior: "auto" });
    hasScrolledRef.current = true;
  }, [autoScrollToNow, nowLineOffset]);

  const jumpToNow = useCallback(() => {
    const el = scrollRef.current;
    if (!el) return;
    const target = Math.max(0, nowLineOffset - 120);
    el.scrollTo({ left: target, behavior: "smooth" });
  }, [nowLineOffset]);

  return (
    <div className="flex flex-col gap-3">
      {/* ── Topbar ──────────────────────────────────────────────────── */}
      <div className="flex items-center justify-between gap-3">
        <div className="text-xs text-tv-fg-2">
          {t("liveTV.guideSubtitle", {
            defaultValue: "Programación de las próximas 24h",
          })}
        </div>
        <button
          type="button"
          onClick={jumpToNow}
          className="flex items-center gap-2 rounded-full border border-tv-accent/40 bg-tv-accent/[0.12] px-3 py-1.5 text-xs font-semibold text-tv-fg-0 transition-colors hover:bg-tv-accent/[0.2]"
        >
          <span className="h-1.5 w-1.5 animate-pulse rounded-full bg-tv-live shadow-[0_0_6px_var(--tv-live)]" />
          {t("liveTV.now", { defaultValue: "Ahora" })} · {nowLabel}
        </button>
      </div>

      {/* ── Grid ────────────────────────────────────────────────────── */}
      <div
        className="relative overflow-hidden rounded-tv-lg border border-tv-line bg-tv-bg-1"
        role="grid"
        aria-label={t("liveTV.epgGridLabel", {
          defaultValue: "Guía de programación",
        })}
      >
        <div
          ref={scrollRef}
          className="relative overflow-auto"
          style={{ maxHeight: "70vh" }}
        >
          {/* Header (sticky top) */}
          <div
            className="sticky top-0 z-20 flex border-b border-tv-line bg-tv-bg-1/95 backdrop-blur"
            style={{ height: HEADER_HEIGHT }}
            role="row"
          >
            {/* Sticky corner */}
            <div
              className="sticky left-0 z-30 flex shrink-0 items-center border-r border-tv-line bg-tv-bg-1/95 px-4 text-[10px] font-semibold uppercase tracking-widest text-tv-fg-3"
              style={{ width: CHANNEL_COL_WIDTH, height: HEADER_HEIGHT }}
              role="columnheader"
            >
              {t("liveTV.channel", { defaultValue: "Canal" })}
            </div>
            {/* Hour ruler */}
            <div
              className="relative shrink-0"
              style={{ width: TIMELINE_WIDTH, height: HEADER_HEIGHT }}
            >
              {hours.map((h) => (
                <div
                  key={h.hour}
                  className={[
                    "absolute top-0 flex h-full items-center border-l border-tv-line px-2 font-mono text-[11px] tabular-nums",
                    h.isNow ? "text-tv-accent" : "text-tv-fg-3",
                  ].join(" ")}
                  style={{ left: h.hour * PX_PER_HOUR, width: PX_PER_HOUR }}
                  role="columnheader"
                >
                  {h.label}
                </div>
              ))}
            </div>
          </div>

          {/* Rows — virtualised above VIRTUALIZE_THRESHOLD channels.
              Below threshold every row renders directly. The empty
              state and the now-line live alongside the rows so they
              line up with the grid's coordinate system. */}
          {channels.length === 0 ? (
            <div className="px-6 py-10 text-center text-sm text-tv-fg-2">
              {t("liveTV.noChannels", {
                defaultValue: "No hay canales disponibles.",
              })}
            </div>
          ) : shouldVirtualize ? (
            <VirtualizedRows
              channels={channels}
              scheduleByChannel={scheduleByChannel}
              windowStart={windowStart}
              windowEnd={windowEnd}
              now={now}
              activeChannelId={activeChannelId}
              onSelectChannel={onSelectChannel}
              onSelectProgram={onSelectProgram}
              scrollRef={scrollRef}
              noEpgLabel={t("liveTV.noEPG", {
                defaultValue: "Sin guía disponible",
              })}
            >
              {nowLineVisible && <NowLine offset={nowLineOffset} />}
            </VirtualizedRows>
          ) : (
            <div className="relative">
              {channels.map((channel) => (
                <ChannelRow
                  key={channel.id}
                  channel={channel}
                  programs={scheduleByChannel[channel.id] ?? []}
                  windowStart={windowStart}
                  windowEnd={windowEnd}
                  now={now}
                  isActive={channel.id === activeChannelId}
                  onSelect={onSelectChannel}
                  onSelectProgram={onSelectProgram}
                  noEpgLabel={t("liveTV.noEPG", {
                    defaultValue: "Sin guía disponible",
                  })}
                />
              ))}
              {nowLineVisible && <NowLine offset={nowLineOffset} />}
            </div>
          )}
        </div>
      </div>
    </div>
  );
}

// NowLine — the absolute-positioned vertical bar marking "right now"
// across all rows. Extracted so both the virtualised and non-
// virtualised branches share the exact same DOM (same z-index, same
// shadow) without copy-pasting.
function NowLine({ offset }: { offset: number }) {
  return (
    <div
      className="pointer-events-none absolute top-0 bottom-0 z-[5]"
      style={{ left: CHANNEL_COL_WIDTH + offset, width: 2 }}
      aria-hidden="true"
    >
      <div className="h-full w-0.5 bg-tv-live shadow-[0_0_8px_var(--tv-live)]" />
      <div className="absolute -top-1 -left-[3px] h-2 w-2 rounded-full bg-tv-live" />
    </div>
  );
}

// VirtualizedRows — wraps useVirtualizer, only mounts when the grid
// has enough rows to justify it. Below VIRTUALIZE_THRESHOLD the
// parent renders rows directly (and never instantiates this
// component / its hook), keeping the simpler render path
// available for tests and small catalogues.
interface VirtualizedRowsProps {
  channels: Channel[];
  scheduleByChannel: Record<string, EPGProgram[]>;
  windowStart: number;
  windowEnd: number;
  now: number;
  activeChannelId?: string;
  onSelectChannel: (ch: Channel) => void;
  onSelectProgram?: (channel: Channel, program: EPGProgram) => void;
  scrollRef: React.RefObject<HTMLDivElement | null>;
  noEpgLabel: string;
  children?: React.ReactNode;
}

function VirtualizedRows({
  channels,
  scheduleByChannel,
  windowStart,
  windowEnd,
  now,
  activeChannelId,
  onSelectChannel,
  onSelectProgram,
  scrollRef,
  noEpgLabel,
  children,
}: VirtualizedRowsProps) {
  const rowVirtualizer = useVirtualizer({
    count: channels.length,
    getScrollElement: () => scrollRef.current,
    estimateSize: () => ROW_HEIGHT,
    overscan: 4,
  });

  return (
    <div
      className="relative"
      style={{ height: rowVirtualizer.getTotalSize() }}
    >
      {rowVirtualizer.getVirtualItems().map((virtualRow) => {
        const channel = channels[virtualRow.index];
        return (
          <div
            key={channel.id}
            className="absolute top-0 left-0 w-full"
            style={{ transform: `translateY(${virtualRow.start}px)` }}
          >
            <ChannelRow
              channel={channel}
              programs={scheduleByChannel[channel.id] ?? []}
              windowStart={windowStart}
              windowEnd={windowEnd}
              now={now}
              isActive={channel.id === activeChannelId}
              onSelect={onSelectChannel}
              onSelectProgram={onSelectProgram}
              noEpgLabel={noEpgLabel}
            />
          </div>
        );
      })}
      {children}
    </div>
  );
}

// ───────────────────────────────────────────────────────────────────
// Row
// ───────────────────────────────────────────────────────────────────

interface ChannelRowProps {
  channel: Channel;
  programs: EPGProgram[];
  windowStart: number;
  windowEnd: number;
  now: number;
  isActive: boolean;
  onSelect: (ch: Channel) => void;
  onSelectProgram?: (channel: Channel, program: EPGProgram) => void;
  noEpgLabel: string;
}

function ChannelRow({
  channel,
  programs,
  windowStart,
  windowEnd,
  now,
  isActive,
  onSelect,
  onSelectProgram,
  noEpgLabel,
}: ChannelRowProps) {
  return (
    <div
      className={[
        "flex border-b border-tv-line transition-colors",
        isActive ? "bg-tv-accent/[0.06]" : "hover:bg-tv-bg-2/50",
      ].join(" ")}
      style={{ height: ROW_HEIGHT }}
      role="row"
    >
      {/* Sticky channel cell */}
      <button
        type="button"
        onClick={() => onSelect(channel)}
        aria-pressed={isActive}
        className={[
          "sticky left-0 z-10 flex shrink-0 items-center gap-3 border-r border-tv-line px-3 text-left",
          isActive ? "bg-tv-accent/10" : "bg-tv-bg-1/95",
        ].join(" ")}
        style={{ width: CHANNEL_COL_WIDTH }}
        role="gridcell"
      >
        <ChannelLogo
          logoUrl={channel.logo_url}
          initials={channel.logo_initials}
          bg={channel.logo_bg}
          fg={channel.logo_fg}
          name={channel.name}
          className="h-9 w-9 rounded-tv-sm"
          textClassName="text-[11px] font-bold"
        />
        <div className="min-w-0 flex-1">
          <div
            className={[
              "truncate text-sm font-medium",
              isActive ? "text-tv-accent" : "text-tv-fg-0",
            ].join(" ")}
          >
            {channel.name}
          </div>
          <div className="truncate font-mono text-[10px] uppercase tracking-widest text-tv-fg-3">
            CH {channel.number}
            {channel.category ? ` · ${channel.category}` : ""}
          </div>
        </div>
      </button>

      {/* Programmes track */}
      <div
        className="relative shrink-0"
        style={{ width: TIMELINE_WIDTH, height: ROW_HEIGHT }}
        role="gridcell"
      >
        {programs.length === 0 ? (
          <div className="absolute inset-y-2 left-3 right-3 flex items-center px-2 text-[11px] italic text-tv-fg-3">
            {noEpgLabel}
          </div>
        ) : (
          programs.map((p) => (
            <ProgramBlock
              key={p.id || `${channel.id}-${p.start_time}`}
              program={p}
              windowStart={windowStart}
              windowEnd={windowEnd}
              now={now}
              // Programme-block clicks open the detail modal when a
              // handler is provided; without one we fall back to the
              // legacy "click anywhere → play channel" behaviour so
              // existing tests + the channel-only-mode still work.
              onSelect={
                onSelectProgram
                  ? () => onSelectProgram(channel, p)
                  : () => onSelect(channel)
              }
              channelName={channel.name}
            />
          ))
        )}
      </div>
    </div>
  );
}

// ───────────────────────────────────────────────────────────────────
// Program
// ───────────────────────────────────────────────────────────────────

interface ProgramBlockProps {
  program: EPGProgram;
  windowStart: number;
  windowEnd: number;
  now: number;
  onSelect: () => void;
  channelName: string;
}

function ProgramBlock({
  program,
  windowStart,
  windowEnd,
  now,
  onSelect,
  channelName,
}: ProgramBlockProps) {
  const start = new Date(program.start_time).getTime();
  const end = new Date(program.end_time).getTime();

  // Skip programmes entirely outside the current 24 h window.
  if (end <= windowStart || start >= windowEnd) return null;

  const clampedStart = Math.max(start, windowStart);
  const clampedEnd = Math.min(end, windowEnd);
  const left = (clampedStart - windowStart) * PX_PER_MS;
  // Subtract 4 px so adjacent blocks have a hairline gap instead of touching.
  const width = Math.max((clampedEnd - clampedStart) * PX_PER_MS - 4, 36);

  const isLive = start <= now && end > now;
  const isPast = end <= now;
  const progress = isLive ? (now - start) / (end - start) : 0;

  const timeLabel = `${fmtTime(start)} – ${fmtTime(end)}`;

  return (
    <button
      type="button"
      onClick={onSelect}
      title={`${program.title}\n${timeLabel}`}
      aria-label={`${program.title} en ${channelName}, ${timeLabel}`}
      className={[
        "absolute top-1.5 bottom-1.5 flex flex-col justify-center overflow-hidden rounded-tv-xs px-2.5 text-left transition",
        isLive
          ? "bg-tv-accent/[0.18] ring-1 ring-tv-accent/50 hover:bg-tv-accent/[0.25]"
          : isPast
            ? "bg-tv-bg-2/60 hover:bg-tv-bg-3"
            : "bg-tv-bg-2 hover:bg-tv-bg-3",
      ].join(" ")}
      style={{ left, width }}
    >
      <div
        className={[
          "truncate text-[12px] font-medium",
          isLive
            ? "text-tv-fg-0"
            : isPast
              ? "text-tv-fg-3"
              : "text-tv-fg-1",
        ].join(" ")}
      >
        {program.title}
      </div>
      <div className="truncate font-mono text-[10px] tabular-nums text-tv-fg-3">
        {timeLabel}
        {program.category ? ` · ${program.category}` : ""}
      </div>
      {isLive && (
        <div
          className="absolute inset-x-0 bottom-0 h-0.5 bg-tv-accent/20"
          aria-hidden="true"
        >
          <div
            className="h-full bg-tv-accent"
            style={{ width: `${progress * 100}%` }}
          />
        </div>
      )}
    </button>
  );
}

function fmtTime(ms: number): string {
  const d = new Date(ms);
  return `${String(d.getHours()).padStart(2, "0")}:${String(
    d.getMinutes(),
  ).padStart(2, "0")}`;
}
