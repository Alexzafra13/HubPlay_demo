import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { useLibraries, useChannels, useBulkSchedule } from "@/api/hooks";
import type { Channel } from "@/api/types";
import { Spinner } from "@/components/common";
import {
  ChannelCard,
  ChannelPlayer,
  ChannelStrip,
  CountrySelector,
  EPGGrid,
  formatTime,
  getNowPlaying,
  getProgramProgress,
  getUpNext,
} from "@/components/livetv";

// View modes: the carousel layout (default, mobile-friendly) or the
// Plex/Jellyfin-style programme-guide grid.
// Defined inside the file because it's used in exactly one place and moving
// it to a separate file just to satisfy react-refresh/only-export-components
// would be ceremony with no reader benefit.
// eslint-disable-next-line react-refresh/only-export-components
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

  // Filter: inactive channels are surfaced by the backend but always fail on
  // playback. Hide them from the UI to avoid dead clicks.
  const channels = useMemo(
    () => (rawChannels ?? []).filter((c) => c.is_active !== false),
    [rawChannels],
  );

  // Lazy init: pick the first available channel on mount. Avoids the
  // set-inside-effect pattern the React Compiler plugin flags.
  const [activeChannel, setActiveChannel] = useState<Channel | null>(
    () => (rawChannels?.[0] ?? null),
  );
  const [search, setSearch] = useState("");
  const [activeGroup, setActiveGroup] = useState<string | null>(null);
  const [viewMode, setViewMode] = useState<ViewMode>("carousel");
  const [zapBuffer, setZapBuffer] = useState<string>(""); // digits buffered for number-entry zap
  const heroRef = useRef<HTMLDivElement>(null);
  const zapTimer = useRef<number | null>(null);

  // EPG for all channels.
  const channelIds = useMemo(() => channels.map((c) => c.id), [channels]);
  const { data: scheduleData } = useBulkSchedule(channelIds);
  const scheduleByChannel = useMemo(() => scheduleData ?? {}, [scheduleData]);

  // Groups by category.
  const groups = useMemo(() => {
    const map = new Map<string, Channel[]>();
    for (const ch of channels) {
      const group = ch.group ?? "General";
      const list = map.get(group) ?? [];
      list.push(ch);
      map.set(group, list);
    }
    return map;
  }, [channels]);

  const groupNames = useMemo(() => Array.from(groups.keys()), [groups]);

  // Search filter.
  const searchResults = useMemo(() => {
    if (!search) return [];
    const q = search.toLowerCase();
    return channels.filter(
      (ch) =>
        ch.name.toLowerCase().includes(q) ||
        (ch.group ?? "").toLowerCase().includes(q),
    );
  }, [channels, search]);

  // If the channel list loads after the initial render (typical for
  // React Query), set the first one. Still guarded against overwriting a
  // user selection.
  if (!activeChannel && channels.length > 0) {
    // Intentional: this is a lazy-init guard, not an effect. Calling setState
    // during render is valid when computing initial state from a derived
    // value. React will re-render once and the condition will be false next
    // time.
    setActiveChannel(channels[0]);
  }

  const handleSelectChannel = useCallback((ch: Channel) => {
    setActiveChannel(ch);
    setSearch("");
    heroRef.current?.scrollIntoView({ behavior: "smooth", block: "start" });
  }, []);

  // ── Keyboard zapping ───────────────────────────────────────────
  // ArrowUp / ArrowDown: previous / next channel.
  // Digits: buffer, jump to matching channel number after 1s of silence OR Enter.
  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      // Don't fight text inputs or textareas.
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
        return;
      }
      if (e.key === "ArrowUp") {
        e.preventDefault();
        const prev = channels[(idx - 1 + channels.length) % channels.length];
        setActiveChannel(prev);
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
      <div className="flex min-h-[60vh] items-center justify-center">
        <Spinner size="lg" />
      </div>
    );
  }

  if (!liveTvLibrary || channels.length === 0) {
    return <CountrySelector hasLibrary={!!liveTvLibrary} />;
  }

  const displayChannels = search
    ? searchResults
    : activeGroup
      ? (groups.get(activeGroup) ?? [])
      : channels;

  const activePrograms = activeChannel
    ? scheduleByChannel[activeChannel.id]
    : undefined;
  const activeNowPlaying = getNowPlaying(activePrograms);
  const activeUpNext = getUpNext(activePrograms);

  return (
    <div className="flex flex-col gap-0 -mx-4 -mt-2 md:-mx-6">
      {/* ── Hero Player ────────────────────────────────────────────── */}
      <div
        ref={heroRef}
        className="relative w-full aspect-[16/9] max-h-[40vh] md:max-h-[65vh] bg-black overflow-hidden"
        aria-live="polite"
        aria-atomic="true"
        aria-label={activeChannel ? `Now watching ${activeChannel.name}` : undefined}
      >
        {activeChannel && <ChannelPlayer channel={activeChannel} />}
        <div className="absolute inset-x-0 bottom-0 h-24 md:h-40 bg-gradient-to-t from-bg-base via-bg-base/60 to-transparent pointer-events-none" />
        {activeChannel && (
          <div className="absolute left-0 bottom-0 right-0 p-3 md:p-8 pointer-events-none">
            <div className="flex items-end gap-3 md:gap-4">
              {activeChannel.logo_url && (
                <img
                  src={activeChannel.logo_url}
                  alt=""
                  className="h-8 w-8 md:h-14 md:w-14 rounded-lg md:rounded-xl object-contain bg-white/10 backdrop-blur-sm p-1 md:p-1.5 shrink-0"
                  onError={(e) => {
                    e.currentTarget.style.display = "none";
                  }}
                />
              )}
              <div className="min-w-0 flex-1">
                <div className="flex items-center gap-2 mb-0.5">
                  <h1 className="text-sm md:text-2xl font-bold text-white truncate drop-shadow-lg">
                    {activeChannel.name}
                  </h1>
                  <span className="shrink-0 flex items-center gap-1 px-1.5 py-0.5 rounded bg-live/90 text-[10px] md:text-xs font-bold text-white uppercase tracking-wider">
                    <span className="w-1.5 h-1.5 rounded-full bg-white animate-pulse" />
                    {t("liveTV.live")}
                  </span>
                </div>

                {activeNowPlaying ? (
                  <div className="space-y-1">
                    <p className="text-xs md:text-sm text-white/80 truncate">
                      <span className="text-white/50">
                        {t("liveTV.nowPlaying")}:
                      </span>{" "}
                      {activeNowPlaying.title}
                    </p>
                    <div className="flex items-center gap-2 max-w-xs md:max-w-md">
                      <div
                        className="flex-1 h-1 rounded-full bg-white/20 overflow-hidden"
                        role="progressbar"
                        aria-valuemin={0}
                        aria-valuemax={100}
                        aria-valuenow={Math.round(
                          getProgramProgress(activeNowPlaying),
                        )}
                      >
                        <div
                          className="h-full rounded-full bg-accent transition-all duration-1000"
                          style={{
                            width: `${getProgramProgress(activeNowPlaying)}%`,
                          }}
                        />
                      </div>
                      <span className="text-[10px] md:text-xs text-white/40 tabular-nums shrink-0">
                        {formatTime(activeNowPlaying.end_time)}
                      </span>
                    </div>
                    {activeUpNext && (
                      <p className="text-[10px] md:text-xs text-white/40 truncate">
                        {t("liveTV.upNext")}: {activeUpNext.title}{" "}
                        {t("liveTV.at")} {formatTime(activeUpNext.start_time)}
                      </p>
                    )}
                  </div>
                ) : activeChannel.group ? (
                  <p className="text-xs md:text-sm text-white/50 truncate">
                    {activeChannel.group}
                  </p>
                ) : null}
              </div>
            </div>
          </div>
        )}

        {/* Channel number input hint when zapping */}
        {zapBuffer && (
          <div className="absolute top-4 right-4 bg-black/60 backdrop-blur-sm rounded-lg px-3 py-2 text-white text-2xl font-bold tabular-nums tracking-wider">
            {zapBuffer}
            <span className="animate-pulse">_</span>
          </div>
        )}
      </div>

      {/* ── Channel Strip (Zapping) ───────────────────────────────── */}
      {channels.length > 1 && (
        <ChannelStrip
          channels={channels}
          activeChannel={activeChannel}
          onSelect={handleSelectChannel}
        />
      )}

      {/* ── Search + Category Tabs + View Mode toggle ───────────────── */}
      <div className="sticky top-[var(--topbar-height)] z-20 bg-bg-base/80 backdrop-blur-xl border-b border-white/5">
        <div className="px-4 md:px-6 pt-3 pb-0">
          <div className="flex items-center gap-2 mb-3">
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
                className="absolute left-3 top-1/2 -translate-y-1/2 text-text-secondary pointer-events-none"
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
                className="w-full pl-9 pr-3 py-2.5 rounded-xl bg-white/5 border border-white/10 text-sm text-text-primary placeholder:text-text-muted focus:border-accent focus:outline-none focus:ring-1 focus:ring-accent/30 transition-all"
              />
            </div>

            {/* View mode toggle */}
            <div
              className="flex rounded-xl border border-white/10 overflow-hidden shrink-0"
              role="tablist"
              aria-label={t("liveTV.viewMode")}
            >
              <button
                type="button"
                role="tab"
                aria-selected={viewMode === "carousel"}
                onClick={() => setViewMode("carousel")}
                className={[
                  "px-3 py-2 text-xs font-medium transition-colors",
                  viewMode === "carousel"
                    ? "bg-accent/15 text-accent"
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
                  "px-3 py-2 text-xs font-medium transition-colors",
                  viewMode === "grid"
                    ? "bg-accent/15 text-accent"
                    : "bg-white/5 text-text-secondary hover:bg-white/10",
                ].join(" ")}
              >
                {t("liveTV.viewGrid")}
              </button>
            </div>
          </div>

          {!search && viewMode === "carousel" && (
            <div className="flex gap-1 overflow-x-auto pb-3 scrollbar-hide -mx-4 px-4 md:-mx-6 md:px-6">
              <button
                type="button"
                onClick={() => setActiveGroup(null)}
                className={[
                  "shrink-0 px-4 py-1.5 rounded-full text-sm font-medium transition-all",
                  activeGroup === null
                    ? "bg-accent text-white shadow-lg shadow-accent/20"
                    : "bg-white/5 text-text-secondary hover:bg-white/10 hover:text-text-primary",
                ].join(" ")}
              >
                {t("liveTV.all")}
              </button>
              {groupNames.map((name) => (
                <button
                  key={name}
                  type="button"
                  onClick={() => setActiveGroup(name)}
                  className={[
                    "shrink-0 px-4 py-1.5 rounded-full text-sm font-medium transition-all whitespace-nowrap",
                    activeGroup === name
                      ? "bg-accent text-white shadow-lg shadow-accent/20"
                      : "bg-white/5 text-text-secondary hover:bg-white/10 hover:text-text-primary",
                  ].join(" ")}
                >
                  {name}
                </button>
              ))}
            </div>
          )}
        </div>
      </div>

      {/* ── Main body: grid OR carousel ─────────────────────────── */}
      <div className="px-4 md:px-6 pb-8 pt-4">
        {viewMode === "grid" ? (
          <EPGGrid
            channels={channels}
            scheduleByChannel={scheduleByChannel}
            activeChannelId={activeChannel?.id}
            onSelectChannel={handleSelectChannel}
          />
        ) : search ? (
          <>
            <p className="text-sm text-text-muted py-4">
              {t("liveTV.channelsFound", { count: searchResults.length })}
            </p>
            <div className="grid grid-cols-1 sm:grid-cols-2 md:grid-cols-3 lg:grid-cols-4 xl:grid-cols-5 gap-2">
              {searchResults.map((ch) => (
                <ChannelCard
                  key={ch.id}
                  channel={ch}
                  isActive={activeChannel?.id === ch.id}
                  nowPlaying={getNowPlaying(scheduleByChannel[ch.id])}
                  onClick={() => handleSelectChannel(ch)}
                />
              ))}
            </div>
          </>
        ) : activeGroup ? (
          <div>
            <div className="grid grid-cols-1 sm:grid-cols-2 md:grid-cols-3 lg:grid-cols-4 xl:grid-cols-5 gap-2">
              {displayChannels.map((ch) => (
                <ChannelCard
                  key={ch.id}
                  channel={ch}
                  isActive={activeChannel?.id === ch.id}
                  nowPlaying={getNowPlaying(scheduleByChannel[ch.id])}
                  onClick={() => handleSelectChannel(ch)}
                />
              ))}
            </div>
          </div>
        ) : (
          <div className="flex flex-col gap-6">
            {groupNames.map((groupName) => {
              const groupChannels = groups.get(groupName) ?? [];
              return (
                <section key={groupName}>
                  <div className="flex items-center justify-between mb-3">
                    <h2 className="text-base md:text-lg font-semibold text-text-primary">
                      {groupName}
                    </h2>
                    <button
                      type="button"
                      onClick={() => setActiveGroup(groupName)}
                      className="text-xs text-text-muted hover:text-accent transition-colors"
                    >
                      {t("common.seeAll")}
                    </button>
                  </div>
                  <div className="flex gap-2 overflow-x-auto pb-2 scrollbar-hide -mx-4 px-4 md:-mx-6 md:px-6">
                    {groupChannels.map((ch) => (
                      <div key={ch.id} className="shrink-0 w-52 md:w-60">
                        <ChannelCard
                          channel={ch}
                          isActive={activeChannel?.id === ch.id}
                          nowPlaying={getNowPlaying(scheduleByChannel[ch.id])}
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

        {search && searchResults.length === 0 && (
          <div className="py-16 text-center text-text-muted">
            {t("liveTV.noChannelsMatch", { search })}
          </div>
        )}
      </div>
    </div>
  );
}
