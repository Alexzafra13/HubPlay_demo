import { useCallback, useEffect, useMemo, useState } from "react";
import { useQueries } from "@tanstack/react-query";
import {
  queryKeys,
  useAddChannelFavorite,
  useBulkSchedule,
  useChannelFavoriteIDs,
  useContinueWatchingChannels,
  useLibraries,
  useRemoveChannelFavorite,
} from "@/api/hooks";
import { api } from "@/api/client";
import type { Channel, ChannelCategory, UnhealthyChannel } from "@/api/types";
import { Spinner } from "@/components/common";
import {
  type CategoryFilter,
  CountrySelector,
  DiscoverView,
  EPGGrid,
  FavoritesView,
  LiveTvTopBar,
  type ViewTab,
  PlayerOverlay,
  getNowPlaying,
  useHeroSpotlight,
} from "@/components/livetv";

/**
 * LiveTV — Discover / Guide / Favorites, wired.
 *
 * Responsibilities kept on this page:
 *   - Fetch livetv libraries + channels + schedules + unhealthy list.
 *   - Tab / category / search state.
 *   - Player overlay state + Escape handler.
 *   - Favorite toggle.
 *   - Derive counts + filtered channels for the tabs.
 *
 * The three tab bodies (DiscoverView, EPGGrid, FavoritesView) are
 * separate components under web/src/components/livetv/. The hero policy
 * lives in useHeroSpotlight. Keeps the page focused on orchestration so
 * changing a tab body doesn't bloat this file.
 */
export default function LiveTV() {
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

  // "Continuar viendo" rail — per-user, populated by the beacon the
  // ChannelPlayer fires on first play. The rail only shows up on the
  // "all" category tab; DiscoverView handles the gating.
  const { data: continueWatching = [] } = useContinueWatchingChannels();

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

  // Hero spotlight — preference, silent fallback and mode options live
  // in the dedicated hook so this page stays focused on layout.
  const {
    items: heroItems,
    label: heroLabel,
    mode: heroMode,
    setMode: setHeroMode,
    modeOptions: heroModeOptions,
  } = useHeroSpotlight({ channels, scheduleByChannel, favoriteSet });

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
  // Fall back to the first visible row so the "active" highlight never
  // points off-screen when the user has a filter or search applied.
  const guideActiveChannel =
    playingChannel ?? filteredChannels[0] ?? channels[0] ?? null;

  return (
    <section
      data-theme="tv"
      data-accent="lime"
      className="-mx-4 -mt-2 flex flex-col gap-6 px-4 pb-10 pt-4 md:-mx-6 md:px-6"
    >
      <LiveTvTopBar
        tab={tab}
        onTab={setTab}
        search={search}
        onSearch={setSearch}
        totalChannels={channels.length}
        liveNow={liveNowCount}
        heroMode={heroMode}
        heroModeOptions={heroModeOptions}
        onHeroModeChange={setHeroMode}
      />

      {tab === "discover" && (
        <DiscoverView
          heroItems={heroItems}
          heroLabel={heroLabel}
          counts={counts}
          category={category}
          onCategoryChange={setCategory}
          channelsByCategory={channelsByCategory}
          scheduleByChannel={scheduleByChannel}
          onOpen={openPlayer}
          favoriteSet={favoriteSet}
          onToggleFavorite={toggleFavorite}
          unhealthyChannels={unhealthyChannels}
          continueWatching={continueWatching}
        />
      )}

      {tab === "guide" && (
        <EPGGrid
          channels={filteredChannels}
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
