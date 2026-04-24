import { useCallback, useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { useQueries } from "@tanstack/react-query";
import {
  queryKeys,
  useAddChannelFavorite,
  useBulkSchedule,
  useChannelFavoriteIDs,
  useLibraries,
  useRemoveChannelFavorite,
} from "@/api/hooks";
import { api } from "@/api/client";
import type { Channel, ChannelCategory, UnhealthyChannel } from "@/api/types";
import { Spinner } from "@/components/common";
import {
  type CategoryFilter,
  CategoryChips,
  ChannelCard,
  ChannelRail,
  CountrySelector,
  EPGGrid,
  HeroMosaic,
  type HeroTileData,
  PlayerOverlay,
  getNowPlaying,
  getUpNext,
} from "@/components/livetv";

type ViewTab = "discover" | "guide" | "favorites";

/**
 * LiveTV — Discover / Guide / Favorites.
 *
 * The page wraps everything in `[data-theme="tv"]` so components can use
 * the TV-scoped tokens (`--tv-accent`, `--tv-bg-*`, etc.) without leaking
 * outside. The three tabs live on one route — switching is local state
 * because deep-linking to a tab isn't a product requirement yet; when it
 * becomes one, lift `tab` to a search param.
 *
 * Channel selection opens a fullscreen player overlay rather than
 * embedding a persistent hero player (the previous model). This matches
 * the redesign's navigation flow: the mosaic stays the entry surface
 * and the player is a modal that can be dismissed.
 */
export default function LiveTV() {
  const { t } = useTranslation();
  const { data: libraries, isLoading: librariesLoading } = useLibraries();

  // Every livetv library the current user can see. Channels from all of
  // them are merged into a single pool for the Discover/Guide surfaces —
  // the admin can have multiple (one per country, one per provider…) and
  // the viewer shouldn't care which library a channel came from.
  const liveTvLibraries = useMemo(
    () => (libraries ?? []).filter((l) => l.content_type === "livetv"),
    [libraries],
  );

  // Parallel channel fetches — one query per library. `useQueries` returns
  // the same shape as `useQuery` for each entry; we flatten `.data` into a
  // single Channel[] below. Cache keys match `useChannels` so a library
  // scan invalidation hits both hooks.
  const channelQueries = useQueries({
    queries: liveTvLibraries.map((lib) => ({
      queryKey: queryKeys.channels(lib.id),
      queryFn: () => api.getChannels(lib.id),
    })),
  });
  const channelsLoading =
    liveTvLibraries.length > 0 && channelQueries.some((q) => q.isLoading);
  const rawChannels = useMemo<Channel[]>(
    () => channelQueries.flatMap((q) => q.data ?? []),
    [channelQueries],
  );

  // Inactive channels 404 on playback — hide them rather than leave dead
  // clicks in the mosaic.
  const channels = useMemo(
    () => (rawChannels ?? []).filter((c) => c.is_active !== false),
    [rawChannels],
  );

  const channelIds = useMemo(() => channels.map((c) => c.id), [channels]);
  const { data: scheduleData } = useBulkSchedule(channelIds);
  const scheduleByChannel = useMemo(() => scheduleData ?? {}, [scheduleData]);

  // Unhealthy channels per library. The backend filters these out of the
  // main channel list (ListHealthyByLibrary) so Discover stays clean, but
  // we still want to surface them — dimmed — in a dedicated "Apagados"
  // rail so the viewer knows the channel exists and the admin can tell
  // at a glance what's currently off the air without jumping to the
  // admin page.
  const unhealthyQueries = useQueries({
    queries: liveTvLibraries.map((lib) => ({
      queryKey: queryKeys.unhealthyChannels(lib.id),
      queryFn: () => api.listUnhealthyChannels(lib.id),
    })),
  });
  const unhealthyChannels = useMemo<UnhealthyChannel[]>(
    () => unhealthyQueries.flatMap((q) => q.data ?? []),
    [unhealthyQueries],
  );

  // ── Tabs + filters ────────────────────────────────────────────────
  const [tab, setTab] = useState<ViewTab>("discover");
  const [category, setCategory] = useState<CategoryFilter>("all");
  const [search, setSearch] = useState("");

  // ── Player overlay ────────────────────────────────────────────────
  const [playingChannel, setPlayingChannel] = useState<Channel | null>(null);

  // Close overlay on Escape. Placed here (not in the overlay) so the key
  // listener stays paired with the state it mutates.
  useEffect(() => {
    if (!playingChannel) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") setPlayingChannel(null);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [playingChannel]);

  const openPlayer = useCallback((ch: Channel) => setPlayingChannel(ch), []);
  const closePlayer = useCallback(() => setPlayingChannel(null), []);

  // ── Favorites ────────────────────────────────────────────────────
  // The IDs query powers the ♥ state on every ChannelCard; we keep it
  // as a Set for O(1) lookups inside render.
  const { data: favoriteIDs } = useChannelFavoriteIDs();
  const favoriteSet = useMemo(
    () => new Set(favoriteIDs ?? []),
    [favoriteIDs],
  );
  const addFavorite = useAddChannelFavorite();
  const removeFavorite = useRemoveChannelFavorite();
  const toggleFavorite = useCallback(
    (channelId: string) => {
      if (favoriteSet.has(channelId)) {
        removeFavorite.mutate(channelId);
      } else {
        addFavorite.mutate(channelId);
      }
    },
    [favoriteSet, addFavorite, removeFavorite],
  );

  // ── Derived: counts per category ─────────────────────────────────
  const counts = useMemo<Record<CategoryFilter, number>>(() => {
    const base: Record<CategoryFilter, number> = {
      all: channels.length,
      general: 0,
      news: 0,
      sports: 0,
      movies: 0,
      music: 0,
      entertainment: 0,
      kids: 0,
      culture: 0,
      documentaries: 0,
      international: 0,
      travel: 0,
      religion: 0,
      adult: 0,
    };
    for (const ch of channels) base[ch.category] += 1;
    return base;
  }, [channels]);

  // ── Derived: filtered + grouped for Discover ─────────────────────
  const filteredChannels = useMemo(() => {
    let list = channels;
    if (category !== "all") {
      list = list.filter((c) => c.category === category);
    }
    if (search.trim()) {
      const q = search.trim().toLowerCase();
      list = list.filter(
        (c) =>
          c.name.toLowerCase().includes(q) ||
          (c.group_name ?? "").toLowerCase().includes(q),
      );
    }
    return list;
  }, [channels, category, search]);

  const channelsByCategory = useMemo(() => {
    const byCat = new Map<ChannelCategory, Channel[]>();
    for (const ch of filteredChannels) {
      const list = byCat.get(ch.category) ?? [];
      list.push(ch);
      byCat.set(ch.category, list);
    }
    return byCat;
  }, [filteredChannels]);

  // Featured = up to 5 channels that currently have a known program on
  // air, so the hero never looks empty on bootstrap. Falls back to the
  // first 5 channels if no EPG has landed yet.
  // Featured = up to 5 channels for the hero mosaic; prefer ones with EPG
  // so the tiles show real "Now on air" titles instead of empty chrome.
  const featured = useMemo<HeroTileData[]>(() => {
    const withProgram: HeroTileData[] = [];
    for (const ch of filteredChannels) {
      const np = getNowPlaying(scheduleByChannel[ch.id]);
      if (np) withProgram.push({ channel: ch, nowPlaying: np });
      if (withProgram.length === 5) break;
    }
    if (withProgram.length > 0) return withProgram;
    return filteredChannels.slice(0, 5).map((c) => ({ channel: c }));
  }, [filteredChannels, scheduleByChannel]);

  // Topbar counter: number of channels *actually broadcasting now* — in IPTV
  // all active channels stream continuously, so this is simply the count of
  // channels that also have an EPG "now on air" entry. If EPG hasn't been
  // loaded yet (or isn't configured), it falls back to the total active count
  // so the stat doesn't stay stuck at 0.
  const liveNowCount = useMemo(() => {
    let n = 0;
    for (const ch of channels) {
      if (getNowPlaying(scheduleByChannel[ch.id])) n++;
    }
    return n > 0 ? n : channels.length;
  }, [channels, scheduleByChannel]);

  // ── Loading + empty states ───────────────────────────────────────
  if (librariesLoading || channelsLoading) {
    return (
      <div className="flex min-h-[60vh] items-center justify-center">
        <Spinner size="lg" />
      </div>
    );
  }

  if (liveTvLibraries.length === 0 || channels.length === 0) {
    return <CountrySelector hasLibrary={liveTvLibraries.length > 0} />;
  }

  // ── Active-channel pointer for EPGGrid (kept for the Guide tab) ───
  const guideActiveChannel = playingChannel ?? channels[0] ?? null;

  return (
    <section
      data-theme="tv"
      data-accent="lime"
      className="-mx-4 -mt-2 flex flex-col gap-6 px-4 pb-10 pt-4 md:-mx-6 md:px-6"
    >
      <TopBar
        tab={tab}
        onTab={setTab}
        search={search}
        onSearch={setSearch}
        totalChannels={channels.length}
        liveNow={liveNowCount}
      />

      {tab === "discover" && (
        <DiscoverView
          featured={featured}
          counts={counts}
          category={category}
          onCategoryChange={setCategory}
          channelsByCategory={channelsByCategory}
          scheduleByChannel={scheduleByChannel}
          onOpen={openPlayer}
          favoriteSet={favoriteSet}
          onToggleFavorite={toggleFavorite}
          unhealthyChannels={unhealthyChannels}
          t={t}
        />
      )}

      {tab === "guide" && (
        <EPGGrid
          channels={filteredChannels.length > 0 ? filteredChannels : channels}
          scheduleByChannel={scheduleByChannel}
          activeChannelId={guideActiveChannel?.id}
          onSelectChannel={openPlayer}
        />
      )}

      {tab === "favorites" && (
        <FavoritesView
          channels={channels}
          favoriteSet={favoriteSet}
          scheduleByChannel={scheduleByChannel}
          onOpen={openPlayer}
          onToggleFavorite={toggleFavorite}
          t={t}
        />
      )}

      {playingChannel && (
        <PlayerOverlay
          channel={playingChannel}
          allChannels={channels}
          scheduleByChannel={scheduleByChannel}
          isFavorite={favoriteSet.has(playingChannel.id)}
          onToggleFavorite={() => toggleFavorite(playingChannel.id)}
          onClose={closePlayer}
          onSelectChannel={openPlayer}
        />
      )}
    </section>
  );
}

// ───────────────────────────────────────────────────────────────────
// TopBar
// ───────────────────────────────────────────────────────────────────

interface TopBarProps {
  tab: ViewTab;
  onTab: (t: ViewTab) => void;
  search: string;
  onSearch: (s: string) => void;
  totalChannels: number;
  liveNow: number;
}

function TopBar({
  tab,
  onTab,
  search,
  onSearch,
  totalChannels,
  liveNow,
}: TopBarProps) {
  const { t } = useTranslation();
  const tabs: { id: ViewTab; label: string }[] = [
    {
      id: "discover",
      label: t("liveTV.tab.discover", { defaultValue: "Descubrir" }),
    },
    { id: "guide", label: t("liveTV.tab.guide", { defaultValue: "Guía" }) },
    {
      id: "favorites",
      label: t("liveTV.tab.favorites", { defaultValue: "Favoritos" }),
    },
  ];

  return (
    <header className="flex flex-col gap-4 lg:flex-row lg:items-center lg:justify-between">
      <div>
        <h1 className="flex items-center gap-2 text-xl font-bold text-tv-fg-0 md:text-2xl">
          <span className="inline-flex h-2.5 w-2.5 animate-pulse rounded-full bg-tv-live shadow-[0_0_8px_var(--tv-live)]" />
          {t("liveTV.title", { defaultValue: "TV en directo" })}
        </h1>
        <p className="mt-1 text-xs text-tv-fg-2">
          <b className="text-tv-fg-1">{totalChannels}</b>{" "}
          {t("liveTV.channels", { defaultValue: "canales" })} ·{" "}
          <b className="text-tv-fg-1">{liveNow}</b>{" "}
          {t("liveTV.liveNow", { defaultValue: "en vivo ahora" })}
        </p>
      </div>

      <div className="flex flex-wrap items-center gap-3">
        <label className="relative flex items-center">
          <span className="sr-only">{t("liveTV.searchPlaceholder")}</span>
          <SearchIcon />
          <input
            type="search"
            value={search}
            onChange={(e) => onSearch(e.target.value)}
            placeholder={t("liveTV.searchPlaceholder", {
              defaultValue: "Busca canales o programas…",
            })}
            className="w-72 rounded-full border border-tv-line bg-tv-bg-1 py-2 pl-9 pr-3 text-sm text-tv-fg-0 placeholder:text-tv-fg-3 focus:border-tv-accent focus:outline-none focus:ring-2 focus:ring-tv-accent/30"
          />
        </label>

        <div
          role="tablist"
          aria-label={t("liveTV.viewMode", { defaultValue: "Vista" })}
          className="flex items-center gap-1 rounded-full border border-tv-line bg-tv-bg-1 p-1"
        >
          {tabs.map((it) => (
            <button
              key={it.id}
              role="tab"
              aria-selected={tab === it.id}
              type="button"
              onClick={() => onTab(it.id)}
              className={[
                "rounded-full px-3 py-1 text-xs font-medium transition-colors",
                tab === it.id
                  ? "bg-tv-accent text-tv-accent-ink"
                  : "text-tv-fg-1 hover:text-tv-fg-0",
              ].join(" ")}
            >
              {it.label}
            </button>
          ))}
        </div>
      </div>
    </header>
  );
}

function SearchIcon() {
  return (
    <svg
      width="14"
      height="14"
      viewBox="0 0 20 20"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.5"
      strokeLinecap="round"
      strokeLinejoin="round"
      className="absolute left-3 top-1/2 -translate-y-1/2 text-tv-fg-3"
      aria-hidden="true"
    >
      <circle cx="8.5" cy="8.5" r="5" />
      <path d="M12.5 12.5L17 17" />
    </svg>
  );
}

// ───────────────────────────────────────────────────────────────────
// DiscoverView
// ───────────────────────────────────────────────────────────────────

interface DiscoverViewProps {
  featured: HeroTileData[];
  counts: Record<CategoryFilter, number>;
  category: CategoryFilter;
  onCategoryChange: (c: CategoryFilter) => void;
  channelsByCategory: Map<ChannelCategory, Channel[]>;
  scheduleByChannel: Record<string, import("@/api/types").EPGProgram[]>;
  onOpen: (ch: Channel) => void;
  favoriteSet: Set<string>;
  onToggleFavorite: (channelId: string) => void;
  unhealthyChannels: UnhealthyChannel[];
  t: ReturnType<typeof useTranslation>["t"];
}

function DiscoverView({
  featured,
  counts,
  category,
  onCategoryChange,
  channelsByCategory,
  scheduleByChannel,
  onOpen,
  favoriteSet,
  onToggleFavorite,
  unhealthyChannels,
  t,
}: DiscoverViewProps) {
  // Rail ordering mirrors the chips' default order so what the user
  // selects in chips and what they scroll past in rails feel consistent.
  const railOrder: ChannelCategory[] = [
    "news",
    "sports",
    "movies",
    "music",
    "documentaries",
    "entertainment",
    "kids",
    "culture",
    "international",
    "travel",
    "religion",
    "general",
    "adult",
  ];

  const visibleRails =
    category === "all"
      ? railOrder
          .map((c) => [c, channelsByCategory.get(c) ?? []] as const)
          .filter(([, list]) => list.length > 0)
      : [[category, channelsByCategory.get(category) ?? []] as const];

  return (
    <div className="flex flex-col gap-8">
      <HeroMosaic items={featured} onOpen={onOpen} />

      <CategoryChips
        counts={counts}
        active={category}
        onChange={onCategoryChange}
      />

      {visibleRails.length === 0 && (
        <div className="rounded-tv-lg border border-dashed border-tv-line bg-tv-bg-1 p-10 text-center text-sm text-tv-fg-2">
          {t("liveTV.noChannelsInCategory", {
            defaultValue: "No hay canales en esta categoría.",
          })}
        </div>
      )}

      {visibleRails.map(([cat, list]) => (
        <ChannelRail
          key={cat}
          title={t(`liveTV.category.${cat}`, {
            defaultValue: capitalize(cat),
          })}
          count={list.length}
          onSeeAll={
            category === "all" ? () => onCategoryChange(cat) : undefined
          }
        >
          {list.map((ch) => (
            <ChannelCard
              key={ch.id}
              channel={ch}
              nowPlaying={getNowPlaying(scheduleByChannel[ch.id])}
              upNext={getUpNext(scheduleByChannel[ch.id])}
              isFavorite={favoriteSet.has(ch.id)}
              onClick={() => onOpen(ch)}
              onToggleFavorite={() => onToggleFavorite(ch.id)}
            />
          ))}
        </ChannelRail>
      ))}

      {/* "Apagados" — channels the health probe has flagged as failing.
          The backend filters them out of the main channel list so
          Discover stays clean, but we surface them here, dimmed, at
          the bottom of the page. A click still tries to play (the
          probe might be stale); the rail fades to near-nothing when
          there's nothing to show, no hard empty state. */}
      {unhealthyChannels.length > 0 && category === "all" ? (
        <ChannelRail
          title={t("liveTV.category.apagados", { defaultValue: "Apagados" })}
          count={unhealthyChannels.length}
          subtitle={t("liveTV.apagadosSubtitle", {
            defaultValue:
              "Canales con fallos recientes; reintenta, quizá hayan vuelto.",
          })}
        >
          {unhealthyChannels.map((ch) => (
            <ChannelCard
              key={ch.id}
              channel={ch}
              isFavorite={favoriteSet.has(ch.id)}
              onClick={() => onOpen(ch)}
              onToggleFavorite={() => onToggleFavorite(ch.id)}
              previewOnHover={false}
              dimmed
            />
          ))}
        </ChannelRail>
      ) : null}
    </div>
  );
}

function capitalize(s: string) {
  return s.charAt(0).toUpperCase() + s.slice(1);
}

// ───────────────────────────────────────────────────────────────────
// FavoritesView
// ───────────────────────────────────────────────────────────────────

interface FavoritesViewProps {
  channels: Channel[];
  favoriteSet: Set<string>;
  scheduleByChannel: Record<string, import("@/api/types").EPGProgram[]>;
  onOpen: (ch: Channel) => void;
  onToggleFavorite: (channelId: string) => void;
  t: ReturnType<typeof useTranslation>["t"];
}

/**
 * FavoritesView — renders the user's favorite channels as a responsive
 * grid of ChannelCards. Derives the list from the current library's
 * channels filtered through `favoriteSet` so stale favorites (channels
 * removed by an M3U refresh) disappear automatically.
 *
 * We derive client-side rather than re-query the `/favorites/channels`
 * endpoint because it lets the grid share the same Channel objects already
 * loaded for Discover — keeping the cache tight and avoiding a second
 * fetch when both tabs are visited in one session.
 */
function FavoritesView({
  channels,
  favoriteSet,
  scheduleByChannel,
  onOpen,
  onToggleFavorite,
  t,
}: FavoritesViewProps) {
  const favorites = useMemo(
    () => channels.filter((c) => favoriteSet.has(c.id)),
    [channels, favoriteSet],
  );

  if (favorites.length === 0) {
    return (
      <div className="flex min-h-[40vh] flex-col items-center justify-center gap-2 rounded-tv-lg border border-dashed border-tv-line bg-tv-bg-1 p-8 text-center text-sm text-tv-fg-2">
        <div className="text-4xl" aria-hidden="true">
          ♡
        </div>
        <p>
          {t("liveTV.favoritesEmpty", {
            defaultValue:
              "Aún no tienes favoritos. Toca ♥ en cualquier canal para añadirlo.",
          })}
        </p>
      </div>
    );
  }

  return (
    <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 md:grid-cols-3 lg:grid-cols-4 xl:grid-cols-5">
      {favorites.map((ch) => (
        <ChannelCard
          key={ch.id}
          channel={ch}
          nowPlaying={getNowPlaying(scheduleByChannel[ch.id])}
          upNext={getUpNext(scheduleByChannel[ch.id])}
          isFavorite
          onClick={() => onOpen(ch)}
          onToggleFavorite={() => onToggleFavorite(ch.id)}
        />
      ))}
    </div>
  );
}

