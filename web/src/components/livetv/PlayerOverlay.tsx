import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import type { Channel, EPGProgram } from "@/api/types";
import { useNowTick } from "@/hooks/useNowTick";
import { ChannelLogo } from "./ChannelLogo";
import { ChannelPlayer } from "./ChannelPlayer";
import { NowPlayingCard } from "./NowPlayingCard";
import { OverlayHeader } from "./OverlayHeader";
import { formatTime, getNowPlaying, getUpNext } from "./epgHelpers";

interface PlayerOverlayProps {
  channel: Channel;
  /** All channels in the library — used to populate "Canales similares". */
  allChannels: Channel[];
  /** Full EPG map. Used to derive current + upcoming programs for the channel. */
  scheduleByChannel: Record<string, EPGProgram[]>;
  /** Favorite state + toggle. Both optional — the button only renders when both are provided. */
  isFavorite?: boolean;
  onToggleFavorite?: () => void;
  onClose: () => void;
  /** Called when the user picks a similar channel; lets the parent swap the player target. */
  onSelectChannel: (ch: Channel) => void;
}

type SidePanelTab = "guide" | "similar";

/**
 * PlayerOverlay — fullscreen live-TV player with a rich side panel.
 *
 * Layout:
 *   Header (OverlayHeader): close + channel identity + favorite toggle.
 *   Body:
 *     - Desktop (≥ lg): 2 columns — video left (flex-1), panel right (420 px).
 *     - Mobile: stacked — video on top (16:9), panel scrolls below.
 *
 * The side panel has two tabs:
 *   "Programación" — NowPlayingCard + UpcomingList (from the channel's EPG).
 *   "Canales similares" — SimilarGrid (other channels in the same canonical category).
 *
 * Keyboard: Escape closes (wired from the parent because the key handler
 * is a global effect that shouldn't live inside this component's lifecycle).
 */
export function PlayerOverlay({
  channel,
  allChannels,
  scheduleByChannel,
  isFavorite,
  onToggleFavorite,
  onClose,
  onSelectChannel,
}: PlayerOverlayProps) {
  const { t } = useTranslation();
  const [tab, setTab] = useState<SidePanelTab>("guide");

  // Clock tick — re-renders every 30 s so "what's on now" and the progress
  // bar on the now-playing card stay fresh. Matches the EPGGrid cadence.
  // Hoisted here (not in child components) so child `useMemo`s stay
  // pure-with-explicit-deps, which the React Compiler requires.
  const now = useNowTick(30_000);

  // Note: we don't auto-reset `tab` when the user picks a similar channel.
  // If they were on "Canales similares", landing there again is actually the
  // expected affordance — they're browsing the category.
  //
  // `programs` is memoised so downstream useMemos have a stable dependency
  // instead of a fresh `?? []` fallback array on every render.
  const programs = useMemo(
    () => scheduleByChannel[channel.id] ?? [],
    [scheduleByChannel, channel.id],
  );
  const nowPlaying = useMemo(() => getNowPlaying(programs), [programs]);
  const upNext = useMemo(() => getUpNext(programs), [programs]);
  const upcoming = useMemo(
    () =>
      programs
        .filter((p) => new Date(p.start_time).getTime() > now)
        .slice(0, 10),
    [programs, now],
  );

  const similar = useMemo(
    () =>
      allChannels
        .filter((c) => c.category === channel.category && c.id !== channel.id)
        .slice(0, 12),
    [allChannels, channel.category, channel.id],
  );

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-label={`${channel.name} — ${t("liveTV.live", { defaultValue: "En directo" })}`}
      className="fixed inset-0 z-50 flex flex-col bg-black/95 backdrop-blur"
      data-theme="tv"
      data-accent="lime"
    >
      <OverlayHeader
        channel={channel}
        isFavorite={isFavorite}
        onToggleFavorite={onToggleFavorite}
        onClose={onClose}
      />

      <div className="flex min-h-0 flex-1 flex-col overflow-hidden lg:flex-row">
        {/* Video pane */}
        <div className="relative bg-black lg:flex-1">
          <div className="aspect-video w-full lg:h-full lg:aspect-auto">
            <ChannelPlayer channel={channel} />
          </div>
        </div>

        {/* Side panel */}
        <aside className="flex min-h-0 flex-1 flex-col overflow-y-auto border-t border-tv-line bg-tv-bg-1 lg:w-[420px] lg:flex-none lg:border-l lg:border-t-0">
          <NowPlayingCard
            channel={channel}
            nowPlaying={nowPlaying}
            upNext={upNext}
            now={now}
          />

          <div className="flex gap-2 border-b border-tv-line px-4">
            <TabButton active={tab === "guide"} onClick={() => setTab("guide")}>
              {t("liveTV.panel.guide", { defaultValue: "Programación" })}
              <span className="ml-2 font-mono text-[10px] tabular-nums text-tv-fg-3">
                {upcoming.length}
              </span>
            </TabButton>
            <TabButton
              active={tab === "similar"}
              onClick={() => setTab("similar")}
            >
              {t("liveTV.panel.similar", { defaultValue: "Canales similares" })}
              <span className="ml-2 font-mono text-[10px] tabular-nums text-tv-fg-3">
                {similar.length}
              </span>
            </TabButton>
          </div>

          <div className="flex-1 overflow-y-auto p-4">
            {tab === "guide" ? (
              <UpcomingList items={upcoming} />
            ) : (
              <SimilarGrid items={similar} onSelect={onSelectChannel} />
            )}
          </div>
        </aside>
      </div>
    </div>
  );
}

// ───────────────────────────────────────────────────────────────────
// Upcoming list
// ───────────────────────────────────────────────────────────────────

function UpcomingList({ items }: { items: EPGProgram[] }) {
  const { t } = useTranslation();
  if (items.length === 0) {
    return (
      <div className="flex min-h-[200px] items-center justify-center text-sm text-tv-fg-3">
        {t("liveTV.noUpcoming", {
          defaultValue: "No hay más programación disponible.",
        })}
      </div>
    );
  }
  return (
    <ul className="flex flex-col gap-1">
      {items.map((p) => {
        const start = new Date(p.start_time).getTime();
        const end = new Date(p.end_time).getTime();
        const durationMin = Math.max(1, Math.round((end - start) / 60_000));
        return (
          <li
            key={p.id || p.start_time}
            className="flex items-center gap-3 rounded-tv-sm px-2 py-2 hover:bg-tv-bg-2/60"
          >
            <span className="w-12 shrink-0 font-mono text-xs tabular-nums text-tv-fg-2">
              {formatTime(p.start_time)}
            </span>
            <div className="min-w-0 flex-1">
              <div className="truncate text-sm text-tv-fg-0">{p.title}</div>
              <div className="truncate text-[11px] text-tv-fg-3">
                {p.category ? `${p.category} · ` : ""}
                {durationMin} {t("liveTV.min", { defaultValue: "min" })}
              </div>
            </div>
          </li>
        );
      })}
    </ul>
  );
}

// ───────────────────────────────────────────────────────────────────
// Similar channels grid
// ───────────────────────────────────────────────────────────────────

function SimilarGrid({
  items,
  onSelect,
}: {
  items: Channel[];
  onSelect: (ch: Channel) => void;
}) {
  const { t } = useTranslation();
  if (items.length === 0) {
    return (
      <div className="flex min-h-[200px] items-center justify-center text-sm text-tv-fg-3">
        {t("liveTV.noSimilar", {
          defaultValue: "No hay canales similares en esta biblioteca.",
        })}
      </div>
    );
  }
  return (
    <div className="grid grid-cols-2 gap-2">
      {items.map((ch) => (
        <button
          key={ch.id}
          type="button"
          onClick={() => onSelect(ch)}
          className="flex items-center gap-2.5 rounded-tv-sm border border-tv-line bg-tv-bg-2/50 p-2 text-left transition-colors hover:border-tv-line-strong hover:bg-tv-bg-2"
        >
          <ChannelLogo
            logoUrl={ch.logo_url}
            initials={ch.logo_initials}
            bg={ch.logo_bg}
            fg={ch.logo_fg}
            name={ch.name}
            className="h-9 w-9 shrink-0 rounded-tv-xs"
            textClassName="text-[10px] font-bold"
          />
          <div className="min-w-0 flex-1">
            <div className="truncate text-xs font-medium text-tv-fg-0">
              {ch.name}
            </div>
            <div className="truncate font-mono text-[10px] uppercase tracking-widest text-tv-fg-3">
              CH {ch.number}
            </div>
          </div>
        </button>
      ))}
    </div>
  );
}

// ───────────────────────────────────────────────────────────────────
// Small primitives
// ───────────────────────────────────────────────────────────────────

function TabButton({
  active,
  onClick,
  children,
}: {
  active: boolean;
  onClick: () => void;
  children: React.ReactNode;
}) {
  return (
    <button
      type="button"
      role="tab"
      aria-selected={active}
      onClick={onClick}
      className={[
        "-mb-px flex items-center border-b-2 px-1 py-3 text-xs font-medium transition-colors",
        active
          ? "border-tv-accent text-tv-fg-0"
          : "border-transparent text-tv-fg-2 hover:text-tv-fg-0",
      ].join(" ")}
    >
      {children}
    </button>
  );
}
