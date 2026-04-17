import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { useLibraries, useChannels, useBulkSchedule } from "@/api/hooks";
import type { Channel } from "@/api/types";
import {
  CategoryChip,
  ChannelCard,
  ChannelPlayer,
  ChannelStrip,
  ChannelTileSkeleton,
  CountrySelector,
  EPGGrid,
  NowPlayingCard,
  getNowPlaying,
  getUpNext,
  parseCategory,
} from "@/components/livetv";

type ViewMode = "carousel" | "grid";

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

  const [activeChannel, setActiveChannel] = useState<Channel | null>(
    () => rawChannels?.[0] ?? null,
  );
  const [search, setSearch] = useState("");
  const [activeCategory, setActiveCategory] = useState<string | null>(null);
  const [viewMode, setViewMode] = useState<ViewMode>("carousel");
  const [zapBuffer, setZapBuffer] = useState<string>("");
  const heroRef = useRef<HTMLDivElement>(null);
  const zapTimer = useRef<number | null>(null);

  const channelIds = useMemo(() => channels.map((c) => c.id), [channels]);
  const { data: scheduleData } = useBulkSchedule(channelIds);
  const scheduleByChannel = useMemo(() => scheduleData ?? {}, [scheduleData]);

  // Group channels by their *parsed* primary category. This replaces the
  // naive group-title bucketing that leaked values like "News;Public".
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

  // Category order: sort by channel count descending so the largest buckets
  // appear first. Ties broken alphabetically for stability.
  const categoryNames = useMemo(() => {
    const entries = Array.from(channelsByCategory.entries());
    entries.sort(
      (a, b) => b[1].length - a[1].length || a[0].localeCompare(b[0]),
    );
    return entries.map(([name]) => name);
  }, [channelsByCategory]);

  // Currently "live" channels: those with a programme airing right now.
  // Powers the "Airing now" showcase row at the top.
  const liveNowChannels = useMemo(() => {
    return channels.filter(
      (c) => getNowPlaying(scheduleByChannel[c.id]) !== null,
    );
  }, [channels, scheduleByChannel]);

  const searchResults = useMemo(() => {
    if (!search) return [];
    const q = search.toLowerCase();
    return channels.filter(
      (ch) =>
        ch.name.toLowerCase().includes(q) ||
        (ch.group ?? "").toLowerCase().includes(q) ||
        parseCategory(ch.group).primary.toLowerCase().includes(q),
    );
  }, [channels, search]);

  if (!activeChannel && channels.length > 0) {
    setActiveChannel(channels[0]);
  }

  const handleSelectChannel = useCallback((ch: Channel) => {
    setActiveChannel(ch);
    setSearch("");
    heroRef.current?.scrollIntoView({ behavior: "smooth", block: "start" });
  }, []);

  // ── Keyboard zapping ───────────────────────────────────────────
  useEffect(() => {
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
        setActiveChannel(
          channels[(idx + 1 + channels.length) % channels.length],
        );
        return;
      }
      if (e.key === "ArrowUp") {
        e.preventDefault();
        setActiveChannel(
          channels[(idx - 1 + channels.length) % channels.length],
        );
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
              if (match) setActiveChannel(match);
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
        if (match) setActiveChannel(match);
        setZapBuffer("");
        if (zapTimer.current) window.clearTimeout(zapTimer.current);
      }
    }
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [channels, activeChannel, zapBuffer]);

  const isLoading = librariesLoading || channelsLoading;

  if (isLoading) {
    return (
      <div className="flex flex-col gap-6 -mx-4 -mt-2 md:-mx-6">
        <div className="aspect-[16/9] max-h-[40vh] w-full animate-pulse bg-gradient-to-b from-white/[0.04] to-black md:max-h-[60vh]" />
        <div className="px-4 md:px-6">
          <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6">
            {Array.from({ length: 12 }).map((_, i) => (
              <ChannelTileSkeleton key={i} />
            ))}
          </div>
        </div>
      </div>
    );
  }

  if (!liveTvLibrary || channels.length === 0) {
    return <CountrySelector hasLibrary={!!liveTvLibrary} />;
  }

  const displayChannels = search
    ? searchResults
    : activeCategory
      ? (channelsByCategory.get(activeCategory) ?? [])
      : channels;

  const activePrograms = activeChannel
    ? scheduleByChannel[activeChannel.id]
    : undefined;
  const activeNowPlaying = getNowPlaying(activePrograms);
  const activeUpNext = getUpNext(activePrograms);

  return (
    <div className="flex flex-col gap-0 -mx-4 -mt-2 md:-mx-6">
      {/* ── Hero Player (clean, no overlay text) ─────────────────── */}
      <div
        ref={heroRef}
        className="relative w-full aspect-[16/9] max-h-[42vh] md:max-h-[62vh] bg-black overflow-hidden"
        aria-label={
          activeChannel ? `Now watching ${activeChannel.name}` : undefined
        }
      >
        {activeChannel && <ChannelPlayer channel={activeChannel} />}

        {/* Only the zap buffer floats over the player. Everything else lives
            below so it never competes with the native controls. */}
        {zapBuffer && (
          <div
            className="absolute right-4 top-4 rounded-lg bg-black/70 px-4 py-2 font-bold tabular-nums text-2xl text-white shadow-xl backdrop-blur-md ring-1 ring-white/20"
            aria-live="assertive"
          >
            {zapBuffer}
            <span className="animate-pulse">_</span>
          </div>
        )}

        {/* Soft bottom fade so the NowPlayingCard bleeds visually into the
            player without hard edges. */}
        <div className="pointer-events-none absolute inset-x-0 bottom-0 h-20 bg-gradient-to-t from-bg-base to-transparent md:h-28" />
      </div>

      {/* ── Now Playing info panel (floats below the hero) ───────── */}
      {activeChannel && (
        <NowPlayingCard
          channel={activeChannel}
          nowPlaying={activeNowPlaying}
          upNext={activeUpNext}
        />
      )}

      {/* ── Zap rail ───────────────────────────────────────────── */}
      {channels.length > 1 && viewMode === "carousel" && (
        <div className="mt-4">
          <ChannelStrip
            channels={channels}
            activeChannel={activeChannel}
            onSelect={handleSelectChannel}
          />
        </div>
      )}

      {/* ── Sticky toolbar: search + view mode ─────────────────── */}
      <div className="sticky top-[var(--topbar-height)] z-20 mt-4 bg-bg-base/80 backdrop-blur-xl border-b border-white/5">
        <div className="px-4 md:px-6 pt-3 pb-3">
          <div className="flex items-center gap-2">
            <div className="relative flex-1">
              <svg
                width="16"
                height="16"
                viewBox="0 0 20 20"
                fill="none"
                stroke="currentColor"
                strokeWidth="1.5"
                strokeLinecap="round"
                strokeLinejoin="round"
                className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-text-secondary"
                aria-hidden="true"
              >
                <circle cx="8.5" cy="8.5" r="5" />
                <path d="M12.5 12.5L17 17" />
              </svg>
              <label className="sr-only" htmlFor="channel-search">
                {t("liveTV.searchPlaceholder")}
              </label>
              <input
                id="channel-search"
                type="text"
                placeholder={t("liveTV.searchPlaceholder")}
                value={search}
                onChange={(e) => setSearch(e.target.value)}
                className="w-full rounded-xl border border-white/10 bg-white/5 py-2.5 pl-9 pr-3 text-sm text-text-primary placeholder:text-text-muted transition-all focus:border-accent focus:outline-none focus:ring-1 focus:ring-accent/30"
              />
            </div>

            <div
              className="flex shrink-0 overflow-hidden rounded-xl border border-white/10"
              role="tablist"
              aria-label={t("liveTV.viewMode")}
            >
              <button
                type="button"
                role="tab"
                aria-selected={viewMode === "carousel"}
                onClick={() => setViewMode("carousel")}
                className={[
                  "px-3 py-2 text-xs font-semibold transition-colors",
                  viewMode === "carousel"
                    ? "bg-accent/20 text-accent"
                    : "bg-white/5 text-text-secondary hover:bg-white/10",
                ].join(" ")}
              >
                {t("liveTV.viewCarousel")}
              </button>
              <button
                type="button"
                role="tab"
                aria-selected={viewMode === "grid"}
                onClick={() => setViewMode("grid")}
                className={[
                  "px-3 py-2 text-xs font-semibold transition-colors",
                  viewMode === "grid"
                    ? "bg-accent/20 text-accent"
                    : "bg-white/5 text-text-secondary hover:bg-white/10",
                ].join(" ")}
              >
                {t("liveTV.viewGrid")}
              </button>
            </div>
          </div>

          {!search && (
            <div className="scrollbar-hide -mx-4 mt-3 flex gap-1.5 overflow-x-auto px-4 pb-1 md:-mx-6 md:px-6">
              <CategoryChip
                label={t("liveTV.all")}
                icon="✨"
                count={channels.length}
                active={activeCategory === null}
                onClick={() => setActiveCategory(null)}
              />
              {categoryNames.map((name) => (
                <CategoryChip
                  key={name}
                  label={name}
                  count={channelsByCategory.get(name)?.length ?? 0}
                  active={activeCategory === name}
                  onClick={() => setActiveCategory(name)}
                />
              ))}
            </div>
          )}
        </div>
      </div>

      {/* ── Main body ────────────────────────────────────────── */}
      <div className="px-4 pb-16 pt-5 md:px-6">
        {viewMode === "grid" ? (
          <EPGGrid
            channels={
              activeCategory
                ? (channelsByCategory.get(activeCategory) ?? channels)
                : channels
            }
            scheduleByChannel={scheduleByChannel}
            activeChannelId={activeChannel?.id}
            onSelectChannel={handleSelectChannel}
          />
        ) : search ? (
          searchResults.length === 0 ? (
            <div className="py-16 text-center text-text-muted">
              {t("liveTV.noChannelsMatch", { search })}
            </div>
          ) : (
            <>
              <p className="mb-4 text-sm text-text-muted">
                {t("liveTV.channelsFound", { count: searchResults.length })}
              </p>
              <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6">
                {searchResults.map((ch) => (
                  <ChannelCard
                    key={ch.id}
                    channel={ch}
                    isActive={activeChannel?.id === ch.id}
                    nowPlaying={getNowPlaying(scheduleByChannel[ch.id])}
                    upNext={getUpNext(scheduleByChannel[ch.id])}
                    onClick={() => handleSelectChannel(ch)}
                  />
                ))}
              </div>
            </>
          )
        ) : activeCategory ? (
          <>
            <SectionHeader
              title={activeCategory}
              count={displayChannels.length}
              onSeeAll={() => setActiveCategory(null)}
              seeAllLabel={t("liveTV.backToAll")}
            />
            <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6">
              {displayChannels.map((ch) => (
                <ChannelCard
                  key={ch.id}
                  channel={ch}
                  isActive={activeChannel?.id === ch.id}
                  nowPlaying={getNowPlaying(scheduleByChannel[ch.id])}
                  upNext={getUpNext(scheduleByChannel[ch.id])}
                  onClick={() => handleSelectChannel(ch)}
                />
              ))}
            </div>
          </>
        ) : (
          <div className="flex flex-col gap-10">
            {/* "Airing now" featured row — skipped when we couldn't identify
                any live programmes (fresh install with no EPG yet, etc.). */}
            {liveNowChannels.length > 0 && (
              <section>
                <SectionHeader
                  title={t("liveTV.airingNow")}
                  count={liveNowChannels.length}
                  pulse
                />
                <div className="scrollbar-hide -mx-4 flex gap-3 overflow-x-auto px-4 pb-2 md:-mx-6 md:px-6">
                  {liveNowChannels.slice(0, 20).map((ch) => (
                    <div
                      key={ch.id}
                      className="w-44 shrink-0 sm:w-48 md:w-52"
                    >
                      <ChannelCard
                        channel={ch}
                        isActive={activeChannel?.id === ch.id}
                        nowPlaying={getNowPlaying(scheduleByChannel[ch.id])}
                        upNext={getUpNext(scheduleByChannel[ch.id])}
                        onClick={() => handleSelectChannel(ch)}
                      />
                    </div>
                  ))}
                </div>
              </section>
            )}

            {categoryNames.map((name) => {
              const groupChannels = channelsByCategory.get(name) ?? [];
              return (
                <section key={name}>
                  <SectionHeader
                    title={name}
                    count={groupChannels.length}
                    onSeeAll={() => setActiveCategory(name)}
                  />
                  <div className="scrollbar-hide -mx-4 flex gap-3 overflow-x-auto px-4 pb-2 md:-mx-6 md:px-6">
                    {groupChannels.map((ch) => (
                      <div
                        key={ch.id}
                        className="w-44 shrink-0 sm:w-48 md:w-52"
                      >
                        <ChannelCard
                          channel={ch}
                          isActive={activeChannel?.id === ch.id}
                          nowPlaying={getNowPlaying(scheduleByChannel[ch.id])}
                          upNext={getUpNext(scheduleByChannel[ch.id])}
                          onClick={() => handleSelectChannel(ch)}
                        />
                      </div>
                    ))}
                  </div>
                </section>
              );
            })}
          </div>
        )}
      </div>
    </div>
  );
}

function SectionHeader({
  title,
  count,
  onSeeAll,
  seeAllLabel,
  pulse = false,
}: {
  title: string;
  count?: number;
  onSeeAll?: () => void;
  seeAllLabel?: string;
  pulse?: boolean;
}) {
  const { t } = useTranslation();
  return (
    <div className="mb-3 flex items-center justify-between">
      <div className="flex items-baseline gap-2">
        <h2 className="text-base font-bold text-text-primary md:text-lg">
          {title}
        </h2>
        {typeof count === "number" && (
          <span className="flex items-center gap-1 rounded-full bg-white/5 px-2 py-0.5 text-[11px] font-semibold tabular-nums text-text-secondary">
            {pulse && (
              <span className="h-1.5 w-1.5 animate-pulse rounded-full bg-live" />
            )}
            {count}
          </span>
        )}
      </div>
      {onSeeAll && (
        <button
          type="button"
          onClick={onSeeAll}
          className="text-xs font-medium text-accent-light transition-colors hover:text-accent"
        >
          {seeAllLabel ?? t("common.seeAll")} →
        </button>
      )}
    </div>
  );
}
