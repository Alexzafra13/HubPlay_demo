import { useState, useMemo, useEffect, useRef, useCallback } from "react";
import { useTranslation } from "react-i18next";
import Hls from "hls.js";
import { useChannels, useLibraries, usePublicCountries, useImportPublicIPTV, useBulkSchedule } from "@/api/hooks";
import type { Channel, EPGProgram, PublicCountry } from "@/api/types";
import { Spinner } from "@/components/common";

// ─── Country auto-detection ──────────────────────────────────────────────────

function detectCountryCode(): string {
  try {
    const tz = Intl.DateTimeFormat().resolvedOptions().timeZone;
    const tzCountry: Record<string, string> = {
      "Europe/Madrid": "es", "Europe/London": "gb", "Europe/Paris": "fr",
      "Europe/Berlin": "de", "Europe/Rome": "it", "Europe/Lisbon": "pt",
      "Europe/Amsterdam": "nl", "Europe/Brussels": "be", "Europe/Zurich": "ch",
      "Europe/Vienna": "at", "Europe/Warsaw": "pl", "Europe/Stockholm": "se",
      "Europe/Oslo": "no", "Europe/Copenhagen": "dk", "Europe/Helsinki": "fi",
      "Europe/Dublin": "ie", "Europe/Athens": "gr", "Europe/Bucharest": "ro",
      "Europe/Prague": "cz", "Europe/Budapest": "hu", "Europe/Sofia": "bg",
      "Europe/Zagreb": "hr", "Europe/Belgrade": "rs", "Europe/Istanbul": "tr",
      "Europe/Moscow": "ru", "Europe/Kiev": "ua", "Europe/Minsk": "by",
      "America/New_York": "us", "America/Chicago": "us", "America/Denver": "us",
      "America/Los_Angeles": "us", "America/Mexico_City": "mx",
      "America/Sao_Paulo": "br", "America/Argentina/Buenos_Aires": "ar",
      "America/Bogota": "co", "America/Lima": "pe", "America/Santiago": "cl",
      "America/Caracas": "ve", "America/Toronto": "ca", "America/Vancouver": "ca",
      "Asia/Tokyo": "jp", "Asia/Shanghai": "cn", "Asia/Seoul": "kr",
      "Asia/Kolkata": "in", "Asia/Bangkok": "th", "Asia/Singapore": "sg",
      "Asia/Jakarta": "id", "Asia/Manila": "ph", "Asia/Taipei": "tw",
      "Asia/Dubai": "ae", "Asia/Riyadh": "sa", "Asia/Tehran": "ir",
      "Australia/Sydney": "au", "Pacific/Auckland": "nz",
      "Africa/Cairo": "eg", "Africa/Lagos": "ng", "Africa/Johannesburg": "za",
      "Atlantic/Canary": "es",
    };
    if (tzCountry[tz]) return tzCountry[tz];
  } catch { /* ignore */ }

  const lang = navigator.language || "";
  const parts = lang.split("-");
  if (parts.length >= 2) return parts[1].toLowerCase();
  return "us";
}

// ─── EPG Helpers ─────────────────────────────────────────────────────────────

function getNowPlaying(programs: EPGProgram[] | undefined): EPGProgram | null {
  if (!programs || programs.length === 0) return null;
  const now = Date.now();
  return programs.find(p =>
    new Date(p.start_time).getTime() <= now &&
    new Date(p.end_time).getTime() > now,
  ) ?? null;
}

function getUpNext(programs: EPGProgram[] | undefined): EPGProgram | null {
  if (!programs || programs.length === 0) return null;
  const now = Date.now();
  // Find the first program that starts after now
  const sorted = [...programs].sort((a, b) =>
    new Date(a.start_time).getTime() - new Date(b.start_time).getTime(),
  );
  return sorted.find(p => new Date(p.start_time).getTime() > now) ?? null;
}

function getProgramProgress(program: EPGProgram): number {
  const now = Date.now();
  const start = new Date(program.start_time).getTime();
  const end = new Date(program.end_time).getTime();
  const duration = end - start;
  if (duration <= 0) return 0;
  return Math.min(100, Math.max(0, ((now - start) / duration) * 100));
}

function formatTime(dateStr: string): string {
  return new Date(dateStr).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
}

// ─── Main Component ──────────────────────────────────────────────────────────

export default function LiveTV() {
  const { t } = useTranslation();
  const { data: libraries, isLoading: librariesLoading } = useLibraries();
  const liveTvLibrary = useMemo(
    () => libraries?.find((l) => l.content_type === "livetv"),
    [libraries],
  );

  const { data: channels, isLoading: channelsLoading } = useChannels(liveTvLibrary?.id);
  const [activeChannel, setActiveChannel] = useState<Channel | null>(null);
  const [search, setSearch] = useState("");
  const [activeGroup, setActiveGroup] = useState<string | null>(null);
  const heroRef = useRef<HTMLDivElement>(null);

  // Fetch EPG for all channels
  const channelIds = useMemo(() => channels?.map(ch => ch.id) ?? [], [channels]);
  const { data: scheduleData } = useBulkSchedule(channelIds);

  // Group channels by category
  const groups = useMemo(() => {
    if (!channels) return new Map<string, Channel[]>();
    const map = new Map<string, Channel[]>();
    for (const ch of channels) {
      const group = ch.group ?? "General";
      const list = map.get(group) ?? [];
      list.push(ch);
      map.set(group, list);
    }
    return map;
  }, [channels]);

  // Filtered channels for search
  const searchResults = useMemo(() => {
    if (!channels || !search) return [];
    const q = search.toLowerCase();
    return channels.filter(
      (ch) =>
        ch.name.toLowerCase().includes(q) ||
        (ch.group ?? "").toLowerCase().includes(q),
    );
  }, [channels, search]);

  const groupNames = useMemo(() => Array.from(groups.keys()), [groups]);

  // Select first channel if none selected
  useEffect(() => {
    if (!activeChannel && channels && channels.length > 0) {
      setActiveChannel(channels[0]);
    }
  }, [channels, activeChannel]);

  const handleSelectChannel = useCallback((ch: Channel) => {
    setActiveChannel(ch);
    setSearch("");
    heroRef.current?.scrollIntoView({ behavior: "smooth", block: "start" });
  }, []);

  const isLoading = librariesLoading || channelsLoading;

  if (isLoading) {
    return (
      <div className="flex min-h-[60vh] items-center justify-center">
        <Spinner size="lg" />
      </div>
    );
  }

  if (!liveTvLibrary || !channels || channels.length === 0) {
    return <CountrySelector hasLibrary={!!liveTvLibrary} />;
  }

  const displayChannels = search
    ? searchResults
    : activeGroup
      ? groups.get(activeGroup) ?? []
      : channels;

  // EPG for the active channel
  const activePrograms = activeChannel ? scheduleData?.[activeChannel.id] : undefined;
  const activeNowPlaying = getNowPlaying(activePrograms);
  const activeUpNext = getUpNext(activePrograms);

  return (
    <div className="flex flex-col gap-0 -mx-4 -mt-2 md:-mx-6">
      {/* ── Hero Player ────────────────────────────────────────────── */}
      <div ref={heroRef} className="relative w-full aspect-[16/9] max-h-[40vh] md:max-h-[65vh] bg-black overflow-hidden">
        {activeChannel && <ChannelPlayer channel={activeChannel} />}

        {/* Gradient overlay at bottom */}
        <div className="absolute inset-x-0 bottom-0 h-24 md:h-40 bg-gradient-to-t from-bg-base via-bg-base/60 to-transparent pointer-events-none" />

        {/* Channel info overlay with EPG */}
        {activeChannel && (
          <div className="absolute left-0 bottom-0 right-0 p-3 md:p-8 pointer-events-none">
            <div className="flex items-end gap-3 md:gap-4">
              {activeChannel.logo_url && (
                <img
                  src={activeChannel.logo_url}
                  alt=""
                  className="h-8 w-8 md:h-14 md:w-14 rounded-lg md:rounded-xl object-contain bg-white/10 backdrop-blur-sm p-1 md:p-1.5 shrink-0"
                />
              )}
              <div className="min-w-0 flex-1">
                <div className="flex items-center gap-2 mb-0.5">
                  <h1 className="text-sm md:text-2xl font-bold text-white truncate drop-shadow-lg">
                    {activeChannel.name}
                  </h1>
                  <span className="shrink-0 flex items-center gap-1 px-1.5 py-0.5 rounded bg-live/90 text-[10px] md:text-xs font-bold text-white uppercase tracking-wider">
                    <span className="w-1.5 h-1.5 rounded-full bg-white animate-pulse" />
                    {t('liveTV.live')}
                  </span>
                </div>

                {/* Now Playing program info */}
                {activeNowPlaying ? (
                  <div className="space-y-1">
                    <p className="text-xs md:text-sm text-white/80 truncate">
                      <span className="text-white/50">{t('liveTV.nowPlaying')}:</span>{" "}
                      {activeNowPlaying.title}
                    </p>
                    {/* Progress bar */}
                    <div className="flex items-center gap-2 max-w-xs md:max-w-md">
                      <div className="flex-1 h-1 rounded-full bg-white/20 overflow-hidden">
                        <div
                          className="h-full rounded-full bg-accent transition-all duration-1000"
                          style={{ width: `${getProgramProgress(activeNowPlaying)}%` }}
                        />
                      </div>
                      <span className="text-[10px] md:text-xs text-white/40 tabular-nums shrink-0">
                        {formatTime(activeNowPlaying.end_time)}
                      </span>
                    </div>
                    {/* Up next */}
                    {activeUpNext && (
                      <p className="text-[10px] md:text-xs text-white/40 truncate">
                        {t('liveTV.upNext')}: {activeUpNext.title} {t('liveTV.at')} {formatTime(activeUpNext.start_time)}
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
      </div>

      {/* ── Channel Strip (Zapping) ───────────────────────────────── */}
      {channels.length > 1 && (
        <ChannelStrip
          channels={channels}
          activeChannel={activeChannel}
          onSelect={handleSelectChannel}
        />
      )}

      {/* ── Search + Category Tabs ─────────────────────────────────── */}
      <div className="sticky top-[var(--topbar-height)] z-20 bg-bg-base/80 backdrop-blur-xl border-b border-white/5">
        <div className="px-4 md:px-6 pt-3 pb-0">
          {/* Search bar */}
          <div className="relative mb-3">
            <svg
              width="16" height="16" viewBox="0 0 20 20" fill="none" stroke="currentColor"
              strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round"
              className="absolute left-3 top-1/2 -translate-y-1/2 text-text-secondary pointer-events-none"
            >
              <circle cx="8.5" cy="8.5" r="5" />
              <path d="M12.5 12.5L17 17" />
            </svg>
            <input
              type="text"
              placeholder={t('liveTV.searchPlaceholder')}
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              className="w-full pl-9 pr-3 py-2.5 rounded-xl bg-white/5 border border-white/10 text-sm text-text-primary placeholder:text-text-muted focus:border-accent focus:outline-none focus:ring-1 focus:ring-accent/30 transition-all"
            />
          </div>

          {/* Category tabs - horizontal scroll */}
          {!search && (
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
                {t('liveTV.all')}
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

      {/* ── Channel Grid / Category Rows ──────────────────────────── */}
      <div className="px-4 md:px-6 pb-8">
        {search ? (
          <>
            <p className="text-sm text-text-muted py-4">
              {t('liveTV.channelsFound', { count: searchResults.length })}
            </p>
            <div className="grid grid-cols-1 sm:grid-cols-2 md:grid-cols-3 lg:grid-cols-4 xl:grid-cols-5 gap-2">
              {searchResults.map((ch) => (
                <ChannelCard
                  key={ch.id}
                  channel={ch}
                  isActive={activeChannel?.id === ch.id}
                  nowPlaying={getNowPlaying(scheduleData?.[ch.id])}
                  onClick={() => handleSelectChannel(ch)}
                />
              ))}
            </div>
          </>
        ) : activeGroup ? (
          <div className="pt-4">
            <div className="grid grid-cols-1 sm:grid-cols-2 md:grid-cols-3 lg:grid-cols-4 xl:grid-cols-5 gap-2">
              {displayChannels.map((ch) => (
                <ChannelCard
                  key={ch.id}
                  channel={ch}
                  isActive={activeChannel?.id === ch.id}
                  nowPlaying={getNowPlaying(scheduleData?.[ch.id])}
                  onClick={() => handleSelectChannel(ch)}
                />
              ))}
            </div>
          </div>
        ) : (
          <div className="flex flex-col gap-6 pt-4">
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
                      {t('common.seeAll')}
                    </button>
                  </div>
                  <div className="flex gap-2 overflow-x-auto pb-2 scrollbar-hide -mx-4 px-4 md:-mx-6 md:px-6">
                    {groupChannels.map((ch) => (
                      <div key={ch.id} className="shrink-0 w-52 md:w-60">
                        <ChannelCard
                          channel={ch}
                          isActive={activeChannel?.id === ch.id}
                          nowPlaying={getNowPlaying(scheduleData?.[ch.id])}
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
            {t('liveTV.noChannelsMatch', { search })}
          </div>
        )}
      </div>
    </div>
  );
}

// ─── Channel Strip (Zapping) ────────────────────────────────────────────────

function ChannelStrip({
  channels,
  activeChannel,
  onSelect,
}: {
  channels: Channel[];
  activeChannel: Channel | null;
  onSelect: (ch: Channel) => void;
}) {
  const scrollRef = useRef<HTMLDivElement>(null);
  const activeRef = useRef<HTMLButtonElement>(null);

  // Auto-scroll to active channel
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
      {/* Fade edges */}
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
              className={[
                "shrink-0 flex items-center gap-1.5 px-2.5 py-1.5 rounded-lg transition-all duration-200",
                isActive
                  ? "bg-accent/15 ring-1 ring-accent/50 scale-105"
                  : "bg-transparent hover:bg-white/5",
              ].join(" ")}
            >
              {ch.logo_url ? (
                <img
                  src={ch.logo_url}
                  alt=""
                  className="w-6 h-6 object-contain rounded"
                  loading="lazy"
                />
              ) : (
                <span className={[
                  "w-6 h-6 rounded flex items-center justify-center text-[10px] font-bold",
                  isActive ? "bg-accent/20 text-accent" : "bg-white/5 text-text-muted",
                ].join(" ")}>
                  {ch.number}
                </span>
              )}
              <span className={[
                "text-xs font-medium truncate max-w-[80px]",
                isActive ? "text-accent" : "text-text-secondary",
              ].join(" ")}>
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

// ─── Channel Card ────────────────────────────────────────────────────────────

function ChannelCard({
  channel,
  isActive,
  nowPlaying,
  onClick,
}: {
  channel: Channel;
  isActive: boolean;
  nowPlaying: EPGProgram | null;
  onClick: () => void;
}) {
  const progress = nowPlaying ? getProgramProgress(nowPlaying) : 0;

  return (
    <button
      type="button"
      onClick={onClick}
      className={[
        "group relative flex items-center gap-2.5 rounded-xl p-2.5 transition-all duration-200 text-left w-full overflow-hidden",
        isActive
          ? "bg-accent/10 ring-1 ring-accent/30"
          : "bg-white/[0.03] hover:bg-white/[0.07]",
      ].join(" ")}
    >
      {/* Logo */}
      <div className={[
        "w-10 h-10 md:w-11 md:h-11 rounded-lg flex items-center justify-center shrink-0 relative",
        isActive ? "bg-accent/10" : "bg-white/5",
      ].join(" ")}>
        {channel.logo_url ? (
          <img
            src={channel.logo_url}
            alt={channel.name}
            className="w-7 h-7 md:w-8 md:h-8 object-contain"
            loading="lazy"
          />
        ) : (
          <span className="text-sm font-bold text-text-muted/50">
            {channel.number}
          </span>
        )}
        {isActive && (
          <div className="absolute -top-0.5 -right-0.5 w-2 h-2 rounded-full bg-live animate-pulse" />
        )}
      </div>

      {/* Info */}
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-1.5">
          <p className="text-xs md:text-sm font-medium text-text-primary truncate">
            {channel.name}
          </p>
          {isActive && (
            <span className="shrink-0 w-1.5 h-1.5 rounded-full bg-live animate-pulse" />
          )}
        </div>

        {/* Now playing or channel number */}
        {nowPlaying ? (
          <p className="text-[10px] md:text-xs text-text-muted truncate mt-0.5">
            {nowPlaying.title}
          </p>
        ) : (
          <p className="text-[10px] md:text-xs text-text-muted truncate mt-0.5">
            Ch. {channel.number}{channel.group ? ` · ${channel.group}` : ""}
          </p>
        )}
      </div>

      {/* EPG progress bar at bottom of card */}
      {nowPlaying && (
        <div className="absolute bottom-0 left-0 right-0 h-0.5 bg-white/5">
          <div
            className={[
              "h-full rounded-r-full transition-all duration-1000",
              isActive ? "bg-accent" : "bg-accent/50",
            ].join(" ")}
            style={{ width: `${progress}%` }}
          />
        </div>
      )}
    </button>
  );
}

// ─── Channel Player ──────────────────────────────────────────────────────────

function ChannelPlayer({ channel }: { channel: Channel }) {
  const { t } = useTranslation();
  const videoRef = useRef<HTMLVideoElement>(null);
  const hlsRef = useRef<Hls | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const networkRetryCount = useRef(0);

  const loadChannel = useCallback(() => {
    const video = videoRef.current;
    if (!video) return;
    setError(null);
    setLoading(true);
    networkRetryCount.current = 0;

    // Clean up previous instance
    if (hlsRef.current) {
      hlsRef.current.destroy();
      hlsRef.current = null;
    }
    video.removeAttribute("src");
    video.load();

    // Auth is handled via HTTP-only cookies for same-origin requests.
    const authedUrl = channel.stream_url;

    let playing = false;
    const timeout = setTimeout(() => {
      if (!playing) setError(t('liveTV.channelUnavailable'));
    }, 20000);

    const onPlaying = () => { playing = true; setLoading(false); clearTimeout(timeout); };
    video.addEventListener("playing", onPlaying);

    function startDirectPlayback() {
      video!.src = authedUrl;
      video!.load();
      video!.play().catch(() => {});
      video!.addEventListener("error", () => setError(t('liveTV.channelUnavailable')), { once: true });
    }

    if (Hls.isSupported()) {
      const hls = new Hls({
        enableWorker: true,
        lowLatencyMode: false,
        maxBufferLength: 30,
        maxMaxBufferLength: 60,
        backBufferLength: 30,
        liveSyncDurationCount: 3,
        liveMaxLatencyDurationCount: 6,
        manifestLoadingMaxRetry: 6,
        manifestLoadingRetryDelay: 1000,
        manifestLoadingMaxRetryTimeout: 8000,
        levelLoadingMaxRetry: 6,
        levelLoadingRetryDelay: 1000,
        levelLoadingMaxRetryTimeout: 8000,
        fragLoadingMaxRetry: 6,
        fragLoadingRetryDelay: 1000,
        fragLoadingMaxRetryTimeout: 8000,
        xhrSetup: (xhr) => {
          // Auth is handled via HTTP-only cookies.
          xhr.withCredentials = true;
        },
      });
      hlsRef.current = hls;

      hls.loadSource(authedUrl);
      hls.attachMedia(video);

      hls.on(Hls.Events.MANIFEST_PARSED, () => {
        video.play().catch(() => {});
      });

      hls.on(Hls.Events.ERROR, (_event, data) => {
        if (data.fatal) {
          if (data.type === Hls.ErrorTypes.MEDIA_ERROR) {
            hls.recoverMediaError();
          } else if (data.type === Hls.ErrorTypes.NETWORK_ERROR) {
            if (networkRetryCount.current < 3) {
              networkRetryCount.current++;
              hls.startLoad();
            } else {
              hls.destroy();
              hlsRef.current = null;
              startDirectPlayback();
            }
          } else {
            hls.destroy();
            hlsRef.current = null;
            startDirectPlayback();
          }
        }
      });
    } else if (video.canPlayType("application/vnd.apple.mpegurl")) {
      video.src = authedUrl;
      video.load();
      video.play().catch(() => {});
      video.addEventListener("error", () => setError(t('liveTV.channelUnavailable')), { once: true });
    } else {
      startDirectPlayback();
    }

    return () => {
      clearTimeout(timeout);
      video.removeEventListener("playing", onPlaying);
      if (hlsRef.current) {
        hlsRef.current.destroy();
        hlsRef.current = null;
      }
    };
  }, [channel.stream_url, t]);

  useEffect(() => {
    return loadChannel();
  }, [loadChannel]);

  return (
    <div className="relative h-full w-full bg-black">
      {loading && !error && (
        <div className="absolute inset-0 flex items-center justify-center z-10">
          <Spinner size="lg" />
        </div>
      )}
      {error && (
        <div className="absolute inset-0 flex items-center justify-center z-10 bg-black/60">
          <div className="text-center px-4">
            <svg width="40" height="40" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1" className="mx-auto mb-3 text-text-muted/40">
              <rect x="2" y="4" width="20" height="14" rx="2" />
              <path d="M7 22h10M12 18v4" />
              <path d="M8 11l8 0M8 11l2-2M8 11l2 2" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
            </svg>
            <p className="text-sm text-text-muted mb-3">{error}</p>
            <button
              type="button"
              onClick={loadChannel}
              className="px-5 py-2 rounded-lg bg-accent/20 text-sm font-medium text-accent hover:bg-accent/30 transition-all"
            >
              {t('common.retry')}
            </button>
          </div>
        </div>
      )}
      <video
        ref={videoRef}
        controls
        className="h-full w-full object-contain"
        playsInline
      />
    </div>
  );
}

// ─── Country Selector (with auto-detect) ─────────────────────────────────────

function CountrySelector({ hasLibrary }: { hasLibrary: boolean }) {
  const { t } = useTranslation();
  const { data: countries, isLoading } = usePublicCountries();
  const importMutation = useImportPublicIPTV();
  const [selectedCountry, setSelectedCountry] = useState<PublicCountry | null>(null);
  const [countrySearch, setCountrySearch] = useState("");
  const [autoDetected, setAutoDetected] = useState(false);

  useEffect(() => {
    if (!countries || countries.length === 0 || autoDetected) return;
    const code = detectCountryCode();
    const match = countries.find((c) => c.code === code);
    if (match) {
      setSelectedCountry(match);
    }
    setAutoDetected(true);
  }, [countries, autoDetected]);

  const filtered = useMemo(() => {
    if (!countries) return [];
    if (!countrySearch) return countries;
    return countries.filter((c) =>
      c.name.toLowerCase().includes(countrySearch.toLowerCase()) ||
      c.code.toLowerCase().includes(countrySearch.toLowerCase()),
    );
  }, [countries, countrySearch]);

  const handleImport = () => {
    if (!selectedCountry) return;
    importMutation.mutate(
      { country: selectedCountry.code },
      {
        onSuccess: () => {
          window.location.reload();
        },
      },
    );
  };

  return (
    <div className="flex min-h-[60vh] items-center justify-center px-4">
      <div className="w-full max-w-lg">
        <div className="mb-8 text-center">
          <div className="mx-auto mb-5 w-20 h-20 rounded-2xl bg-accent/10 flex items-center justify-center">
            <svg width="40" height="40" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5" className="text-accent">
              <rect x="2" y="4" width="20" height="14" rx="2" />
              <path d="M7 22h10M12 18v4" />
            </svg>
          </div>
          <h2 className="text-2xl font-bold text-text-primary">
            {hasLibrary ? t('liveTV.noChannelsLoaded') : t('liveTV.setupLiveTV')}
          </h2>
          <p className="mt-2 text-sm text-text-muted max-w-sm mx-auto">
            {t('liveTV.importDescription')}
            {selectedCountry && !countrySearch && (
              <span className="block mt-1 text-accent">
                {t('liveTV.detectedCountry', { flag: selectedCountry.flag, country: selectedCountry.name })}
              </span>
            )}
          </p>
        </div>

        <div className="rounded-2xl border border-white/10 bg-white/[0.03] backdrop-blur-sm p-5">
          <input
            type="text"
            placeholder={t('liveTV.searchCountries')}
            value={countrySearch}
            onChange={(e) => setCountrySearch(e.target.value)}
            className="mb-4 w-full rounded-xl bg-white/5 border border-white/10 px-4 py-2.5 text-sm text-text-primary placeholder:text-text-muted focus:border-accent focus:outline-none focus:ring-1 focus:ring-accent/30 transition-all"
          />

          {isLoading ? (
            <div className="flex justify-center py-8">
              <Spinner size="md" />
            </div>
          ) : (
            <div className="grid max-h-60 grid-cols-2 gap-2 overflow-y-auto sm:grid-cols-3 pr-1">
              {filtered.map((country) => (
                <button
                  key={country.code}
                  type="button"
                  onClick={() => setSelectedCountry(country)}
                  className={[
                    "rounded-xl border px-3 py-2.5 text-left text-sm transition-all",
                    selectedCountry?.code === country.code
                      ? "border-accent bg-accent/10 text-text-primary ring-1 ring-accent/30"
                      : "border-white/10 bg-white/[0.02] text-text-secondary hover:bg-white/5 hover:text-text-primary",
                  ].join(" ")}
                >
                  <span className="mr-1.5">{country.flag}</span>
                  {country.name}
                </button>
              ))}
            </div>
          )}

          {selectedCountry && (
            <div className="mt-5 flex items-center justify-between gap-3">
              <span className="text-sm text-text-secondary truncate">
                {selectedCountry.flag} <strong>{selectedCountry.name}</strong>
              </span>
              <button
                type="button"
                onClick={handleImport}
                disabled={importMutation.isPending}
                className="shrink-0 rounded-xl bg-accent px-5 py-2.5 text-sm font-medium text-white transition-all hover:bg-accent/90 hover:shadow-lg hover:shadow-accent/20 disabled:opacity-50"
              >
                {importMutation.isPending ? (
                  <span className="flex items-center gap-2">
                    <Spinner size="sm" /> Importing...
                  </span>
                ) : (
                  t('liveTV.importChannels')
                )}
              </button>
            </div>
          )}

          {importMutation.isError && (
            <p className="mt-3 text-sm text-error">
              {t('liveTV.importFailed')}
            </p>
          )}
        </div>
      </div>
    </div>
  );
}
