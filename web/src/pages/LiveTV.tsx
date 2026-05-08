import { useCallback, useEffect, useMemo, useState } from "react";
import { useSearchParams } from "react-router";
import { useTranslation } from "react-i18next";
import { useLiveTvPlayer } from "@/store/liveTvPlayer";
import {
  useAddChannelFavorite,
  useChannelFavoriteIDs,
  useRemoveChannelFavorite,
} from "@/api/hooks";
import type { Channel, ChannelCategory, EPGProgram } from "@/api/types";
import { useLiveTvData } from "./liveTv/useLiveTvData";
import {
  CategoryChips,
  type CategoryFilter,
  CountrySelector,
  DiscoverView,
  EPGGrid,
  HeroSpotlight,
  LiveTvSkeleton,
  type ViewTab,
  PlayerOverlay,
  ProgramDetailModal,
  getNowPlaying,
  useHeroSpotlight,
} from "@/components/livetv";

// Map from legacy ?tab values (pre 2026-05-08, when Live TV had 4
// tabs: Now / Discover / Guide / Favorites) onto the current 2-tab
// vocabulary. Sidebar links and any bookmarks from before the redesign
// route through here on first land so users don't hit a dead URL.
const LEGACY_TAB_MAP: Record<string, ViewTab> = {
  now: "inicio",
  guide: "inicio",
  discover: "explorar",
  favorites: "explorar",
};

function normalizeViewTab(raw: string | null): ViewTab {
  if (raw === "inicio" || raw === "explorar") return raw;
  if (raw && LEGACY_TAB_MAP[raw]) return LEGACY_TAB_MAP[raw];
  return "inicio";
}

/**
 * LiveTV — Plex-style 2-surface design: Inicio + Explorar.
 *
 * Inicio: hero spotlight + Plex-style EPG schedule grid (filterable
 *   by category). Answers the user's "what's on right now?" question.
 * Explorar: full channel lineup with category sections and an inline
 *   "Solo favoritos" filter for browsing.
 *
 * Responsibilities kept on this page:
 *   - Fetch livetv libraries + channels + schedules + unhealthy list.
 *   - Tab / category / search / favourites-filter state (URL-backed).
 *   - Player overlay state + Escape handler.
 *   - Favorite toggle.
 *   - Derive counts + filtered channels for both surfaces.
 *
 * Composition lives inline (small enough now) — the page-level pieces
 * (HeroSpotlight, EPGGrid, DiscoverView) live under web/src/components/
 * livetv/ so each can iterate independently.
 */
export default function LiveTV() {
  const { t } = useTranslation();

  // Data layer (parallel fetch + flatten) lives in useLiveTvData so
  // this page only orchestrates URL state, player overlay, favourites,
  // and rendering. Replaces ~60 lines of inline useQueries + useMemo
  // chains that mixed data-shape concerns with view orchestration.
  const {
    liveTvLibraries,
    channels,
    channelsLoading,
    librariesLoading,
    unhealthyChannels,
    scheduleByChannel,
    continueWatching,
  } = useLiveTvData();

  // ── Tabs + filters (URL-backed) ───────────────────────────────────
  // The page's filter state lives in `?tab&cat&q=fav=` so deep-links
  // survive a refresh, the browser Back button works as the user
  // expects, and links are shareable. Defaults are the canonical empty
  // state and are kept out of the URL (we delete the param) so the bar
  // stays tidy when the user is on Inicio/Todos/no-search.
  const [searchParams, setSearchParams] = useSearchParams();
  const tab = normalizeViewTab(searchParams.get("tab"));
  const category = (searchParams.get("cat") as CategoryFilter) ?? "all";
  const search = searchParams.get("q") ?? "";
  const favoritesOnly = searchParams.get("fav") === "1";

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
  const setCategory = useCallback(
    (next: CategoryFilter) => updateParam("cat", next, "all"),
    [updateParam],
  );
  // tab / search / favouritesOnly are mirrored to the URL by callers
  // outside this page (the global TopBar SearchBar mirrors `?q=`,
  // the MainNav dropdown links to `/live-tv?tab=…&fav=1`). Reading
  // them as URL state keeps this page passive — it just renders
  // whichever combination the URL describes.

  // Migrate legacy `?tab=now|guide|discover|favorites` URLs (sidebar
  // links and bookmarks pre-2026-05-08) onto the new 2-tab vocabulary.
  // Runs once on mount; subsequent navigations land directly on the
  // new param names. We rewrite via `setSearchParams({replace})` so
  // the user's history isn't polluted with an extra entry.
  useEffect(() => {
    const raw = searchParams.get("tab");
    if (!raw) return;
    const migrated = LEGACY_TAB_MAP[raw];
    if (!migrated) return;
    setSearchParams(
      (prev) => {
        const next = new URLSearchParams(prev);
        if (migrated === "inicio") next.delete("tab");
        else next.set("tab", migrated);
        // `?sort=` was an artefact of the old "Ahora" tab — drop it.
        next.delete("sort");
        return next;
      },
      { replace: true },
    );
  }, [searchParams, setSearchParams]);

  // ── Player overlay (lives in the global LiveTV player store so it
  //    survives navigation as a corner mini-player) ────────────────
  const playingChannel = useLiveTvPlayer((s) => s.channel);
  const overlayExpanded = useLiveTvPlayer((s) => s.expanded);
  const openPlayerStore = useLiveTvPlayer((s) => s.open);
  const selectChannel = useLiveTvPlayer((s) => s.select);
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

  // Plex-style click model:
  //   - `selectPreview`: click on a channel card / EPG row puts the
  //     channel in the hero (preview) and does NOT escalate to
  //     fullscreen. Used by every "pick a channel" surface.
  //   - `openPlayer`: explicit "Reproducir" / "Expandir" action
  //     (hero play button, deep-link from /live-tv?channel=X). Goes
  //     fullscreen.
  // Both seed the surf list with whichever subset the user is
  // currently browsing so ↑/↓ matches what they're seeing.
  const selectPreview = useCallback(
    (ch: Channel, surfList?: Channel[]) => {
      selectChannel(ch, surfList ?? channels);
    },
    [selectChannel, channels],
  );
  const openPlayer = useCallback(
    (ch: Channel, surfList?: Channel[]) => {
      openPlayerStore(ch, surfList ?? channels);
    },
    [openPlayerStore, channels],
  );
  const closePlayer = collapsePlayer;

  // Deep-link: ?channel=<id> lands here from the Home page's "En
  // directo ahora" rail (and from any other surface that routes
  // straight into a specific channel). Opens the player as soon as
  // the channel list is hydrated, then strips the param from the
  // URL so a back-nav or refresh doesn't re-trigger.
  const channelParam = searchParams.get("channel");
  useEffect(() => {
    if (!channelParam) return;
    if (channels.length === 0) return; // wait for channels to load
    const ch = channels.find((c) => c.id === channelParam);
    if (ch) openPlayer(ch);
    setSearchParams(
      (prev) => {
        const next = new URLSearchParams(prev);
        next.delete("channel");
        return next;
      },
      { replace: true },
    );
  }, [channelParam, channels, openPlayer, setSearchParams]);

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
  // `filteredChannels` honours category + search + favourites toggle
  // (Explorar) and is shared with the EPG grid on Inicio (which only
  // honours category + search since Inicio has no favourites filter
  // — favourites are surfaced via the hero spotlight there).
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

  // Explorar applies the favourites toggle on top of the shared
  // filtering — kept separate so Inicio's EPG keeps showing every
  // channel the user can scroll to.
  const explorarChannels = useMemo(() => {
    if (!favoritesOnly) return filteredChannels;
    return filteredChannels.filter((c) => favoriteSet.has(c.id));
  }, [filteredChannels, favoritesOnly, favoriteSet]);

  const channelsByCategory = useMemo(() => {
    const byCat = new Map<ChannelCategory, Channel[]>();
    for (const ch of explorarChannels) {
      const list = byCat.get(ch.category) ?? [];
      list.push(ch);
      byCat.set(ch.category, list);
    }
    return byCat;
  }, [explorarChannels]);

  // Hero spotlight — preference, silent fallback and mode options live
  // in the dedicated hook so this page stays focused on layout.
  const {
    items: heroAutoItems,
    label: heroAutoLabel,
    mode: heroMode,
    setMode: setHeroMode,
  } = useHeroSpotlight({ channels, scheduleByChannel, favoriteSet });

  // Effective hero feed: when the user has clicked a channel (Plex
  // pattern → preview without fullscreen), the hero shows that
  // channel. Otherwise we fall back to the auto-curated spotlight.
  // The label flips to "Selected" so the user knows the hero is
  // mirroring their click, not a curated suggestion.
  const heroItems = useMemo(() => {
    if (playingChannel) {
      return [
        {
          channel: playingChannel,
          nowPlaying: getNowPlaying(scheduleByChannel[playingChannel.id]) ?? null,
        },
      ];
    }
    return heroAutoItems;
  }, [playingChannel, scheduleByChannel, heroAutoItems]);
  const heroLabel = playingChannel
    ? t("liveTV.heroLabelSelected", { defaultValue: "Canal seleccionado" })
    : heroAutoLabel;

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

  // ── Active-channel pointer for EPGGrid ───────────────────────────
  // Fall back to the first visible row so the "active" highlight never
  // points off-screen when the user has a filter or search applied.
  const inicioActiveChannel =
    playingChannel ?? filteredChannels[0] ?? channels[0] ?? null;

  // Header overlay for the hero — page title + counters. Lifted into
  // the hero so the spotlight hugs the TopBar instead of sitting under
  // a separate stripe.
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
      // Small top breathing room (pt-3) so the hero feels close to the
      // global TopBar without looking sliced off at the top edge.
      // No `data-accent` override — the section inherits the global
      // platform accent (teal) instead of the previous lime so the
      // active state on chips, the now-line and the live-cell ring
      // match the buttons elsewhere in the app.
      className="-mx-4 flex flex-col gap-6 px-4 pb-10 pt-3 md:-mx-6 md:px-6"
    >
      {/* Page-level subnav and search live in the GLOBAL TopBar so
          /live-tv keeps the same chrome as the rest of the app:
          - the global SearchBar already filter-mode mirrors `?q=` on
            this route (FILTER_ROUTES in SearchBar.tsx)
          - Inicio / Explorar are reachable via the "TV en vivo"
            dropdown in MainNav. We don't redraw a second nav strip
            here.
          - Hero settings (the gear) and the favourites filter live
            inside their respective surfaces (hero overlay / Explorar
            chips), not as a bar across the top. */}

      {tab === "inicio" && (
        <div className="flex flex-col gap-6">
          {heroMode !== "off" && heroItems.length > 0 ? (
            <HeroSpotlight
              items={heroItems}
              label={heroLabel}
              onOpen={openPlayer}
              isFavorite={
                heroItems[0] ? favoriteSet.has(heroItems[0].channel.id) : false
              }
              onToggleFavorite={toggleFavorite}
              headerOverlay={heroHeaderOverlay}
            />
          ) : null}
          <CategoryChips
            counts={counts}
            active={category}
            onChange={setCategory}
          />
          <EPGGrid
            channels={filteredChannels}
            scheduleByChannel={scheduleByChannel}
            activeChannelId={inicioActiveChannel?.id}
            // Plex-style: clicking a channel row swaps the hero
            // (preview) instead of opening fullscreen. The hero's
            // play button stays the explicit "go fullscreen" action.
            onSelectChannel={selectPreview}
            onSelectProgram={openProgramDetail}
          />
        </div>
      )}

      {tab === "explorar" && (
        <DiscoverView
          heroItems={heroItems}
          heroLabel={heroLabel}
          counts={counts}
          category={category}
          onCategoryChange={setCategory}
          channelsByCategory={channelsByCategory}
          scheduleByChannel={scheduleByChannel}
          // Same Plex pattern as the EPG: card click previews on
          // the hero, the hero's play button is the fullscreen action.
          onOpen={selectPreview}
          favoriteSet={favoriteSet}
          onToggleFavorite={toggleFavorite}
          unhealthyChannels={unhealthyChannels}
          continueWatching={continueWatching}
          // Hero overlay/title only on Explorar when we keep the
          // spotlight (we hide it here when the user is filtering by
          // favourites — the favourites toggle replaces the hero as
          // the dominant signal).
          heroHeaderOverlay={favoritesOnly ? undefined : heroHeaderOverlay}
          heroMode={favoritesOnly ? "off" : heroMode}
          onHeroModeChange={setHeroMode}
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
