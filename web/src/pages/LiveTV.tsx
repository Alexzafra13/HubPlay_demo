import { useCallback, useEffect, useMemo, useState } from "react";
import { useSearchParams } from "react-router";
import { useTranslation } from "react-i18next";
import { useQueries } from "@tanstack/react-query";
import { useLiveTvPlayer } from "@/store/liveTvPlayer";
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
import type {
  Channel,
  ChannelCategory,
  EPGProgram,
  UnhealthyChannel,
} from "@/api/types";
import {
  type CategoryFilter,
  CountrySelector,
  DiscoverView,
  EPGGrid,
  FavoritesView,
  LiveNowView,
  type LiveNowSort,
  LiveTvSkeleton,
  LiveTvTopBar,
  type ViewTab,
  PlayerOverlay,
  ProgramDetailModal,
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

  // "Continuar viendo" rail — per-user, populated by the beacon the
  // ChannelPlayer fires on first play. The rail only shows up on the
  // "all" category tab; DiscoverView handles the gating.
  const { data: continueWatching = [] } = useContinueWatchingChannels();

  // ── Tabs + filters (URL-backed) ───────────────────────────────────
  // The page's filter state lives in `?tab&cat&q=` so deep-links survive
  // a refresh, the browser Back button works as the user expects, and
  // links are shareable. Defaults are the canonical empty state and
  // are kept out of the URL (we delete the param) so the bar stays
  // tidy when the user is on Discover/Todos/no-search.
  const [searchParams, setSearchParams] = useSearchParams();
  const tab = (searchParams.get("tab") as ViewTab) ?? "now";
  const category = (searchParams.get("cat") as CategoryFilter) ?? "all";
  const search = searchParams.get("q") ?? "";
  const sort = (searchParams.get("sort") as LiveNowSort) ?? "favorites";

  const updateParam = useCallback(
    (key: string, value: string, defaultValue: string) => {
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev);
          if (value === defaultValue || value === "") next.delete(key);
          else next.set(key, value);
          return next;
        },
        { replace: true },
      );
    },
    [setSearchParams],
  );
  const setTab = useCallback(
    (next: ViewTab) => updateParam("tab", next, "now"),
    [updateParam],
  );
  const setCategory = useCallback(
    (next: CategoryFilter) => updateParam("cat", next, "all"),
    [updateParam],
  );
  const setSearch = useCallback(
    (next: string) => updateParam("q", next, ""),
    [updateParam],
  );
  const setSort = useCallback(
    (next: LiveNowSort) => updateParam("sort", next, "favorites"),
    [updateParam],
  );

  // ── Player overlay (lives in the global LiveTV player store so it
  //    survives navigation as a corner mini-player) ────────────────
  const playingChannel = useLiveTvPlayer((s) => s.channel);
  const overlayExpanded = useLiveTvPlayer((s) => s.expanded);
  const openPlayerStore = useLiveTvPlayer((s) => s.open);
  const collapsePlayer = useLiveTvPlayer((s) => s.collapse);
  const surfNext = useLiveTvPlayer((s) => s.surfNext);
  const surfPrev = useLiveTvPlayer((s) => s.surfPrev);

  // Esc collapses the overlay to the corner mini-player (audio keeps
  // going) — explicit "stop" lives on the mini-player's X button.
  useEffect(() => {
    if (!playingChannel || !overlayExpanded) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") collapsePlayer();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [playingChannel, overlayExpanded, collapsePlayer]);

  // Channel surfing keyboard — ↑/↓ walks `surfList` (which the page
  // keeps in sync with the user's currently-visible filtered list, see
  // the effect below). Only active while the overlay is up; the corner
  // mini-player intentionally doesn't surf so a user navigating other
  // pages doesn't accidentally switch channels.
  useEffect(() => {
    if (!playingChannel || !overlayExpanded) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "ArrowDown") {
        e.preventDefault();
        surfNext();
      } else if (e.key === "ArrowUp") {
        e.preventDefault();
        surfPrev();
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [playingChannel, overlayExpanded, surfNext, surfPrev]);

  // Helper used by every "click a channel card" path on this page.
  // Seeds the surf list with whichever subset is currently visible to
  // the user so ↑/↓ matches what they're actually browsing.
  const openPlayer = useCallback(
    (ch: Channel, surfList?: Channel[]) => {
      openPlayerStore(ch, surfList ?? channels);
    },
    [openPlayerStore, channels],
  );
  const closePlayer = collapsePlayer;

  // ── Program detail modal ─────────────────────────────────────────
  // Opened from EPGGrid by clicking a programme cell; closes when
  // the user dismisses or clicks "Ver canal ahora" (which also opens
  // the player). Keeping both pieces of state independent so the
  // modal close-out animates separately from the overlay open.
  const [detail, setDetail] = useState<{
    channel: Channel;
    program: EPGProgram;
  } | null>(null);
  const openProgramDetail = useCallback(
    (ch: Channel, p: EPGProgram) => setDetail({ channel: ch, program: p }),
    [],
  );
  const closeProgramDetail = useCallback(() => setDetail(null), []);

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
      "no-signal": 0,
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
    for (const ch of channels) {
      base[ch.category] += 1;
      // "no-signal" is virtual — counts every channel currently
      // failing (degraded or dead). Server hides truly-dead channels
      // so this is mostly the degraded set, which is the actionable
      // bucket the user sees on the chip.
      if (ch.health_status === "degraded" || ch.health_status === "dead") {
        base["no-signal"] += 1;
      }
    }
    return base;
  }, [channels]);

  // ── Derived: filtered + grouped for Discover ─────────────────────
  const filteredChannels = useMemo(() => {
    let list = channels;
    if (category === "no-signal") {
      list = list.filter(
        (c) => c.health_status === "degraded" || c.health_status === "dead",
      );
    } else if (category !== "all") {
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

  // ── "Ahora en directo" rail ──────────────────────────────────────
  // Independent of the hero — surfaces every channel that has an EPG
  // entry currently broadcasting, capped at a manageable rail length.
  // Favourites bubble to the front so the user's anchor channels don't
  // get buried; ties break by channel number for a stable order.
  // Capped at 18 because beyond that the rail becomes a wall the user
  // can't really scan; the dedicated "Guía" tab is the right place for
  // exhaustive listings.
  const liveNowChannels = useMemo(() => {
    return channels
      .filter((c) => getNowPlaying(scheduleByChannel[c.id]))
      .sort((a, b) => {
        const aFav = favoriteSet.has(a.id) ? 0 : 1;
        const bFav = favoriteSet.has(b.id) ? 0 : 1;
        return aFav - bFav || a.number - b.number;
      });
  }, [channels, scheduleByChannel, favoriteSet]);

  // Counts per category — scoped to live-now channels only — so the
  // chip pills on the "Ahora" tab read "how many channels in this
  // category are on right now", not "how many total". The total
  // counts already live in `counts` (used by DiscoverView).
  const liveNowCounts = useMemo<Record<CategoryFilter, number>>(() => {
    const base: Record<CategoryFilter, number> = {
      all: liveNowChannels.length,
      "no-signal": 0,
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
    for (const ch of liveNowChannels) {
      base[ch.category] += 1;
    }
    return base;
  }, [liveNowChannels]);

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
  // Skeleton (vs. centred Spinner) so the user gets a silhouette of
  // the final layout straight away — perceived performance is much
  // better when the page chrome is visible during fetch.
  if (librariesLoading || channelsLoading) {
    return <LiveTvSkeleton />;
  }

  if (liveTvLibraries.length === 0 || channels.length === 0) {
    return <CountrySelector hasLibrary={liveTvLibraries.length > 0} />;
  }

  // ── Active-channel pointer for EPGGrid (kept for the Guide tab) ───
  // Fall back to the first visible row so the "active" highlight never
  // points off-screen when the user has a filter or search applied.
  const guideActiveChannel =
    playingChannel ?? filteredChannels[0] ?? channels[0] ?? null;

  // Header overlay for the hero — page title + counters. Lifted into
  // the hero so the spotlight hugs the TopBar instead of sitting under
  // a separate stripe; reused on the discover tab only (the other tabs
  // still get the inline title from LiveTvTopBar).
  const heroHeaderOverlay = (
    <div>
      <h1 className="flex items-center gap-2 text-xl font-bold text-tv-fg-0 drop-shadow-md md:text-2xl">
        <span className="inline-flex h-2.5 w-2.5 animate-pulse rounded-full bg-tv-live shadow-[0_0_8px_var(--tv-live)]" />
        {t('liveTV.title')}
      </h1>
      <p className="mt-1 text-xs text-tv-fg-1 drop-shadow">
        <b className="text-tv-fg-0">{channels.length}</b> {t('liveTV.channels')} ·{" "}
        <b className="text-tv-fg-0">{liveNowCount}</b> {t('liveTV.liveNow')}
      </p>
    </div>
  );

  return (
    <section
      data-theme="tv"
      data-accent="lime"
      // Small top breathing room (pt-3) so the hero feels close to the
      // global TopBar without looking sliced off at the top edge —
      // flush-zero looked cropped against the bar's frosted glass.
      // The hero keeps its rounded corners (no flushTop) for the same
      // reason: a soft-edged card framed by a few px of bg reads as
      // intentional whereas hard square corners under the bar read as
      // overflow.
      className="-mx-4 flex flex-col gap-6 px-4 pb-10 pt-3 md:-mx-6 md:px-6"
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
          heroHeaderOverlay={heroHeaderOverlay}
          heroMode={heroMode}
          onHeroModeChange={setHeroMode}
        />
      )}

      {tab === "now" && (
        <LiveNowView
          channels={liveNowChannels}
          scheduleByChannel={scheduleByChannel}
          category={category}
          onCategoryChange={setCategory}
          counts={liveNowCounts}
          search={search}
          sort={sort}
          onSortChange={setSort}
          onOpen={openPlayer}
          favoriteSet={favoriteSet}
          onToggleFavorite={toggleFavorite}
        />
      )}

      {tab === "guide" && (
        <EPGGrid
          channels={filteredChannels}
          scheduleByChannel={scheduleByChannel}
          activeChannelId={guideActiveChannel?.id}
          onSelectChannel={openPlayer}
          onSelectProgram={openProgramDetail}
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

      {playingChannel && overlayExpanded && (
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

      <ProgramDetailModal
        isOpen={detail !== null}
        onClose={closeProgramDetail}
        program={detail?.program ?? null}
        channel={detail?.channel ?? null}
        // Up-next list: programmes on the same channel that start
        // AFTER the selected one ends, capped at 3. The schedule
        // cache is already ordered by start_time on the backend
        // (`ORDER BY start_time ASC`), so we don't re-sort here.
        upNext={
          detail
            ? (scheduleByChannel[detail.channel.id] ?? [])
                .filter(
                  (p) =>
                    new Date(p.start_time).getTime() >=
                    new Date(detail.program.end_time).getTime(),
                )
                .slice(0, 3)
            : []
        }
        onWatch={() => {
          if (!detail) return;
          const ch = detail.channel;
          closeProgramDetail();
          openPlayer(ch);
        }}
      />
    </section>
  );
}
