import { useState } from "react";
import { useTranslation } from "react-i18next";
import type { Channel, EPGProgram } from "@/api/types";
import { ChannelDetailPanel } from "./ChannelDetailPanel";
import { ChannelPlayer } from "./ChannelPlayer";
import { ChannelStrip } from "./ChannelStrip";
import { NowPlayingCard } from "./NowPlayingCard";
import { getNowPlaying, getUpNext } from "./epgHelpers";

interface WatchingViewProps {
  activeChannel: Channel;
  channels: Channel[];
  scheduleByChannel: Record<string, EPGProgram[]>;
  onBack: () => void;
  onSelectChannel: (channel: Channel) => void;
  zapBuffer: string;
  favorites: Set<string>;
  onToggleFavorite: (channelId: string) => void;
}

type MobileTab = "player" | "schedule";

/**
 * Full-screen-ish watching experience. Desktop uses a 60/40 split — player
 * + NowPlayingCard on the left, ChannelDetailPanel + zap-rail on the right.
 * Mobile stacks them and exposes a tab switcher so users can flip between
 * "watching" and "schedule" without losing the stream.
 */
export function WatchingView({
  activeChannel,
  channels,
  scheduleByChannel,
  onBack,
  onSelectChannel,
  zapBuffer,
  favorites,
  onToggleFavorite,
}: WatchingViewProps) {
  const { t } = useTranslation();
  const [mobileTab, setMobileTab] = useState<MobileTab>("player");
  const programs = scheduleByChannel[activeChannel.id];
  const nowPlaying = getNowPlaying(programs);
  const upNext = getUpNext(programs);
  const isFav = favorites.has(activeChannel.id);

  return (
    <div className="flex min-h-[calc(100vh-var(--topbar-height))] flex-col">
      {/* ── Back bar ───────────────────────────────────────────── */}
      <div className="flex items-center justify-between gap-2 border-b border-white/5 bg-bg-base/80 px-4 py-2 backdrop-blur-md md:px-6">
        <button
          type="button"
          onClick={onBack}
          className="inline-flex items-center gap-1.5 rounded-lg px-2 py-1 text-sm font-medium text-text-secondary transition-colors hover:bg-white/5 hover:text-text-primary"
          aria-label={t("liveTV.backToChannels")}
        >
          <svg
            width="16"
            height="16"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth="2"
            strokeLinecap="round"
            strokeLinejoin="round"
            aria-hidden="true"
          >
            <path d="M19 12H5M12 19l-7-7 7-7" />
          </svg>
          {t("liveTV.backToChannels")}
        </button>

        {/* Mobile tab switcher — hidden on md+ because both panes are
            visible side-by-side. */}
        <div
          role="tablist"
          aria-label={t("liveTV.viewMode")}
          className="flex overflow-hidden rounded-lg border border-white/10 md:hidden"
        >
          <button
            type="button"
            role="tab"
            aria-selected={mobileTab === "player"}
            onClick={() => setMobileTab("player")}
            className={[
              "px-3 py-1 text-xs font-semibold transition-colors",
              mobileTab === "player"
                ? "bg-accent/20 text-accent"
                : "bg-white/5 text-text-secondary hover:bg-white/10",
            ].join(" ")}
          >
            {t("liveTV.tabPlayer")}
          </button>
          <button
            type="button"
            role="tab"
            aria-selected={mobileTab === "schedule"}
            onClick={() => setMobileTab("schedule")}
            className={[
              "px-3 py-1 text-xs font-semibold transition-colors",
              mobileTab === "schedule"
                ? "bg-accent/20 text-accent"
                : "bg-white/5 text-text-secondary hover:bg-white/10",
            ].join(" ")}
          >
            {t("liveTV.schedule")}
          </button>
        </div>
      </div>

      {/* ── Split layout ───────────────────────────────────────── */}
      <div className="flex min-h-0 flex-1 flex-col md:grid md:grid-cols-[1.6fr_1fr] md:gap-5 md:px-6 md:pt-4">
        {/* ─── Left pane: player + NowPlaying + zap-rail ─────── */}
        <div
          className={[
            "flex min-w-0 flex-col",
            mobileTab === "player" ? "flex" : "hidden md:flex",
          ].join(" ")}
        >
          <div
            className="relative aspect-[16/9] w-full overflow-hidden bg-black md:rounded-2xl"
            aria-label={`Now watching ${activeChannel.name}`}
          >
            <ChannelPlayer channel={activeChannel} />

            {zapBuffer && (
              <div
                className="absolute right-4 top-4 rounded-lg bg-black/70 px-4 py-2 font-bold tabular-nums text-2xl text-white shadow-xl backdrop-blur-md ring-1 ring-white/20"
                aria-live="assertive"
              >
                {zapBuffer}
                <span className="animate-pulse">_</span>
              </div>
            )}

            <div className="pointer-events-none absolute inset-x-0 bottom-0 h-20 bg-gradient-to-t from-bg-base to-transparent md:hidden" />
          </div>

          <NowPlayingCard
            channel={activeChannel}
            nowPlaying={nowPlaying}
            upNext={upNext}
          />

          {channels.length > 1 && (
            <div className="mt-4">
              <ChannelStrip
                channels={channels}
                activeChannel={activeChannel}
                onSelect={onSelectChannel}
              />
            </div>
          )}
        </div>

        {/* ─── Right pane: channel detail / schedule ─────────── */}
        <div
          className={[
            "min-h-0 flex-1 px-4 pb-6 pt-4 md:flex md:flex-col md:px-0 md:pt-0 md:pb-2",
            mobileTab === "schedule" ? "flex flex-col" : "hidden md:flex",
          ].join(" ")}
        >
          <ChannelDetailPanel
            channel={activeChannel}
            programs={programs}
            isFavorite={isFav}
            onToggleFavorite={() => onToggleFavorite(activeChannel.id)}
          />
        </div>
      </div>
    </div>
  );
}
