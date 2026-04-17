import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { useLibraries, useChannels, useBulkSchedule } from "@/api/hooks";
import type { Channel } from "@/api/types";
import {
  BrowseView,
  ChannelTileSkeleton,
  CountrySelector,
  EPGGrid,
  WatchingView,
  parseCategory,
} from "@/components/livetv";
import { useChannelFavorites } from "@/hooks/useChannelFavorites";
import { useLastChannel } from "@/hooks/useLastChannel";

type ViewState = "browse" | "watching" | "guide";

/**
 * Live TV orchestrator. Delegates three mutually exclusive experiences to
 * child components so each stays simple to reason about:
 *
 *   • "browse" — landing: featured hero, continue-watching, favourites,
 *                per-category shelves. No player attached.
 *   • "watching" — split-pane player + channel-detail (schedule, favourite
 *                  toggle). Back button returns to browse.
 *   • "guide"  — fullscreen EPG grid. Clicking a programme either opens a
 *                detail popover or switches to watching mode.
 *
 * This orchestrator owns the cross-cutting state (active channel, search,
 * favourites, zapping) and hands it down; the children are presentational.
 */
export default function LiveTV() {
  const { t } = useTranslation();
  const { data: libraries, isLoading: librariesLoading } = useLibraries();
  const liveTvLibrary = useMemo(
    () => libraries?.find((l) => l.content_type === "livetv"),
    [libraries],
  );
  const { data: rawChannels, isLoading: channelsLoading } = useChannels(
    liveTvLibrary?.id,
  );

  const channels = useMemo(
    () => (rawChannels ?? []).filter((c) => c.is_active !== false),
    [rawChannels],
  );

  // Persisted UI state.
  const { favorites, toggleFavorite } = useChannelFavorites();
  const { lastChannelId, setLastChannel } = useLastChannel();

  // View-state machine.
  const [viewState, setViewState] = useState<ViewState>("browse");
  const [activeChannel, setActiveChannel] = useState<Channel | null>(null);
  const [search, setSearch] = useState("");
  const [activeCategory, setActiveCategory] = useState<string | null>(null);
  const [zapBuffer, setZapBuffer] = useState<string>("");
  const zapTimer = useRef<number | null>(null);

  const channelIds = useMemo(() => channels.map((c) => c.id), [channels]);
  const { data: scheduleData } = useBulkSchedule(channelIds);
  const scheduleByChannel = useMemo(() => scheduleData ?? {}, [scheduleData]);

  // Group channels by parsed primary category (replaces the old group-title
  // bucketing that leaked values like "News;Public").
  const channelsByCategory = useMemo(() => {
    const map = new Map<string, Channel[]>();
    for (const ch of channels) {
      const { primary } = parseCategory(ch.group);
      const list = map.get(primary) ?? [];
      list.push(ch);
      map.set(primary, list);
    }
    return map;
  }, [channels]);

  // Category order: sort by channel count descending; ties alphabetical.
  const categoryNames = useMemo(() => {
    const entries = Array.from(channelsByCategory.entries());
    entries.sort(
      (a, b) => b[1].length - a[1].length || a[0].localeCompare(b[0]),
    );
    return entries.map(([name]) => name);
  }, [channelsByCategory]);

  // ── Selection handler: moves into "watching" mode and records the
  //    channel as the last-watched so it reappears in Continue Watching.
  const handleSelectChannel = useCallback(
    (ch: Channel) => {
      setActiveChannel(ch);
      setLastChannel(ch.id);
      setSearch("");
      setViewState("watching");
    },
    [setLastChannel],
  );

  const handleBackToBrowse = useCallback(() => {
    setViewState("browse");
  }, []);

  const handleOpenGuide = useCallback(() => {
    setViewState("guide");
  }, []);

  // ── Keyboard zapping (only active while watching) ───────────────
  useEffect(() => {
    if (viewState !== "watching") return;

    function onKey(e: KeyboardEvent) {
      const target = e.target as HTMLElement | null;
      if (
        target &&
        (target.tagName === "INPUT" ||
          target.tagName === "TEXTAREA" ||
          target.isContentEditable)
      ) {
        return;
      }
      if (channels.length === 0) return;
      const idx = channels.findIndex((c) => c.id === activeChannel?.id);

      if (e.key === "ArrowDown") {
        e.preventDefault();
        const next = channels[(idx + 1 + channels.length) % channels.length];
        setActiveChannel(next);
        setLastChannel(next.id);
        return;
      }
      if (e.key === "ArrowUp") {
        e.preventDefault();
        const next = channels[(idx - 1 + channels.length) % channels.length];
        setActiveChannel(next);
        setLastChannel(next.id);
        return;
      }
      if (e.key >= "0" && e.key <= "9") {
        e.preventDefault();
        setZapBuffer((prev) => prev + e.key);
        if (zapTimer.current) window.clearTimeout(zapTimer.current);
        zapTimer.current = window.setTimeout(() => {
          setZapBuffer((num) => {
            if (num) {
              const n = parseInt(num, 10);
              const match = channels.find((c) => c.number === n);
              if (match) {
                setActiveChannel(match);
                setLastChannel(match.id);
              }
            }
            return "";
          });
        }, 1000);
        return;
      }
      if (e.key === "Enter" && zapBuffer) {
        e.preventDefault();
        const n = parseInt(zapBuffer, 10);
        const match = channels.find((c) => c.number === n);
        if (match) {
          setActiveChannel(match);
          setLastChannel(match.id);
        }
        setZapBuffer("");
        if (zapTimer.current) window.clearTimeout(zapTimer.current);
      }
      if (e.key === "Escape") {
        e.preventDefault();
        setViewState("browse");
      }
    }
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [viewState, channels, activeChannel, zapBuffer, setLastChannel]);

  // ── Loading / empty states ──────────────────────────────────────
  const isLoading = librariesLoading || channelsLoading;

  if (isLoading) {
    return (
      <div className="flex flex-col gap-6 px-4 pt-4 md:px-6">
        <div className="h-40 w-full animate-pulse rounded-3xl bg-white/[0.04] md:h-56" />
        <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6">
          {Array.from({ length: 12 }).map((_, i) => (
            <ChannelTileSkeleton key={i} />
          ))}
        </div>
      </div>
    );
  }

  if (!liveTvLibrary || channels.length === 0) {
    return <CountrySelector hasLibrary={!!liveTvLibrary} />;
  }

  // ── Route to the active view ────────────────────────────────────
  if (viewState === "watching" && activeChannel) {
    return (
      <div className="-mx-4 -mt-2 md:-mx-6">
        <WatchingView
          activeChannel={activeChannel}
          channels={channels}
          scheduleByChannel={scheduleByChannel}
          onBack={handleBackToBrowse}
          onSelectChannel={(ch) => {
            setActiveChannel(ch);
            setLastChannel(ch.id);
          }}
          zapBuffer={zapBuffer}
          favorites={favorites}
          onToggleFavorite={toggleFavorite}
        />
      </div>
    );
  }

  if (viewState === "guide") {
    return (
      <div className="flex flex-col gap-4 px-4 pb-16 pt-4 md:px-6">
        <div className="flex items-center justify-between">
          <button
            type="button"
            onClick={handleBackToBrowse}
            className="inline-flex items-center gap-1.5 rounded-lg px-2 py-1 text-sm font-medium text-text-secondary transition-colors hover:bg-white/5 hover:text-text-primary"
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
          <h1 className="text-base font-bold text-text-primary md:text-lg">
            {t("liveTV.guideTitle")}
          </h1>
        </div>
        <EPGGrid
          channels={channels}
          scheduleByChannel={scheduleByChannel}
          activeChannelId={activeChannel?.id}
          onSelectChannel={handleSelectChannel}
        />
      </div>
    );
  }

  // Default: browse view.
  return (
    <BrowseView
      channels={channels}
      scheduleByChannel={scheduleByChannel}
      channelsByCategory={channelsByCategory}
      categoryNames={categoryNames}
      search={search}
      onSearchChange={setSearch}
      activeCategory={activeCategory}
      onCategoryChange={setActiveCategory}
      activeChannelId={activeChannel?.id}
      onSelectChannel={handleSelectChannel}
      lastChannelId={lastChannelId}
      favorites={favorites}
      onToggleFavorite={toggleFavorite}
      onOpenGuide={handleOpenGuide}
    />
  );
}
