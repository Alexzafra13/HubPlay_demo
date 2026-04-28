import { useState, useCallback, useRef, useEffect, useMemo } from "react";
import { useParams, useNavigate, Link } from "react-router";
import { useTranslation } from "react-i18next";
import { useQueryClient } from "@tanstack/react-query";
import { useItem, useItemChildren, useToggleFavorite, queryKeys } from "@/api/hooks";
import { api } from "@/api/client";
import type { MediaItem, PlaybackMethod } from "@/api/types";
import { Spinner, EmptyState } from "@/components/common";
import { HeroSection, SeriesHero, MediaMeta, EpisodeCard, EpisodeRow } from "@/components/media";
import type { HeroMenuItem } from "@/components/media/HeroSection";
import { VideoPlayer } from "@/components/player";
import { ImageManager } from "@/components/ImageManager";
import { useAuthStore } from "@/store/auth";
import { useResumeTarget } from "@/hooks/useSeriesResumeTarget";

export default function ItemDetail() {
  const { t } = useTranslation();
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const { data: item, isLoading, isError } = useItem(id ?? "");
  const user = useAuthStore((s) => s.user);
  const isAdmin = user?.role === "admin";
  const isSeries = item?.type === "series";
  const isSeason = item?.type === "season";
  // Both series and season pages render the SeriesHero — the layout
  // (full-bleed backdrop + poster column) works for both. The
  // distinguishing scope drives where Play resumes from.
  const heroScope: "series" | "season" | null =
    isSeries ? "series" : isSeason ? "season" : null;

  // Resume target — drives the smart Play button label ("Reproducir"
  // vs "Seguir viendo S01E03") and where the click lands. Inert when
  // the page isn't a series/season.
  const resumeTarget = useResumeTarget(
    heroScope ?? "series",
    heroScope && id ? id : null,
  );

  const queryClient = useQueryClient();
  const toggleFavoriteMutation = useToggleFavorite();

  // Image manager state
  const [imageManagerOpen, setImageManagerOpen] = useState(false);

  // Player state
  const [showPlayer, setShowPlayer] = useState(false);
  const [playerInfo, setPlayerInfo] = useState<{
    playbackMethod: PlaybackMethod;
    masterPlaylistUrl: string | null;
    directUrl: string | null;
  } | null>(null);
  const [playError, setPlayError] = useState<string | null>(null);
  const isPlayingRef = useRef(false);

  // Episode context for prefetching next episode
  const [playingItemId, setPlayingItemId] = useState<string | null>(null);

  // Fetch sibling episodes when the current item is an episode. The list is
  // pure derivation — filter + sort — so it lives in a useMemo. Previously
  // this was a state + effect that re-derived on every siblings change,
  // which React 19 + Compiler flagged as a cascading render.
  const parentId = item?.parent_id;
  const { data: siblings } = useItemChildren(parentId ?? "", { enabled: !!parentId && item?.type === "episode" });
  const siblingEpisodes = useMemo<MediaItem[]>(() => {
    if (!siblings || siblings.length === 0) return [];
    return siblings
      .filter((s) => s.type === "episode")
      .sort((a, b) => (a.episode_number ?? 0) - (b.episode_number ?? 0));
  }, [siblings]);

  // ─── Favorite state ─────────────────────────────────────────────────────

  const isFavorite = item?.user_data?.is_favorite ?? false;

  const handleToggleFavorite = useCallback(() => {
    if (!id) return;
    toggleFavoriteMutation.mutate(id);
  }, [id, toggleFavoriteMutation]);

  // ─── Kebab menu items ───────────────────────────────────────────────────
  //
  // Plain derivation rebuilt on every render. Used to be a useMemo with
  // deps [isAdmin, id, item, t], but the compiler rejected the manual
  // memoization because the body also reads `queryClient` (not in deps)
  // — so it was always re-computing anyway. With React Compiler, the
  // whole component gets auto-memoized; a manual wrapper only gets in
  // its way.

  const menuItems: HeroMenuItem[] = (() => {
    const items: HeroMenuItem[] = [];

    // Admin-only items
    if (isAdmin && id) {
      items.push({
        label: t("imageManager.title"),
        icon: (
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} className="h-4 w-4">
            <path strokeLinecap="round" strokeLinejoin="round" d="M2.25 15.75l5.159-5.159a2.25 2.25 0 013.182 0l5.159 5.159m-1.5-1.5l1.409-1.409a2.25 2.25 0 013.182 0l2.909 2.909M3.75 21h16.5A2.25 2.25 0 0022.5 18.75V5.25A2.25 2.25 0 0020.25 3H3.75A2.25 2.25 0 001.5 5.25v13.5A2.25 2.25 0 003.75 21z" />
          </svg>
        ),
        onClick: () => setImageManagerOpen(true),
      });

      items.push({
        label: t("itemDetail.refreshMetadata"),
        icon: (
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} className="h-4 w-4">
            <path strokeLinecap="round" strokeLinejoin="round" d="M16.023 9.348h4.992v-.001M2.985 19.644v-4.992m0 0h4.992m-4.993 0l3.181 3.183a8.25 8.25 0 0013.803-3.7M4.031 9.865a8.25 8.25 0 0113.803-3.7l3.181 3.182" />
          </svg>
        ),
        onClick: () => {
          // Re-fetch this item's metadata
          queryClient.invalidateQueries({ queryKey: queryKeys.item(id!) });
        },
      });
    }

    // Media info (scroll to section)
    if (item?.media_streams && item.media_streams.length > 0) {
      items.push({
        label: t("itemDetail.mediaInfo"),
        icon: (
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} className="h-4 w-4">
            <path strokeLinecap="round" strokeLinejoin="round" d="M11.25 11.25l.041-.02a.75.75 0 011.063.852l-.708 2.836a.75.75 0 001.063.853l.041-.021M21 12a9 9 0 11-18 0 9 9 0 0118 0zm-9-3.75h.008v.008H12V8.25z" />
          </svg>
        ),
        onClick: () => {
          document.getElementById("media-info-section")?.scrollIntoView({ behavior: "smooth" });
        },
      });
    }

    return items;
  })();

  // ─── Playback ───────────────────────────────────────────────────────────

  const cleanupSession = useCallback(async (itemId: string) => {
    try {
      const token = localStorage.getItem("hubplay_access_token");
      await fetch(`/api/v1/stream/${itemId}/session`, {
        method: "DELETE",
        headers: token ? { Authorization: `Bearer ${token}` } : {},
      });
    } catch { /* best-effort cleanup */ }
    isPlayingRef.current = false;
  }, []);

  // handlePlay accepts an optional override targetId so the season
  // detail page can fire the inline player against ANY of its episodes
  // (clicking an EpisodeRow), not just the page's own item. Default is
  // the URL id, which preserves the original "Play this item" semantics
  // movies and direct-played episodes rely on.
  const handlePlay = useCallback(async (targetId?: string) => {
    const playId = targetId ?? id;
    if (!playId) return;
    setPlayError(null);

    try {
      if (isPlayingRef.current && playingItemId) {
        await cleanupSession(playingItemId);
      }

      const info = await api.getStreamInfo(playId);
      const rawMethod = (info as Record<string, unknown>).method as string ?? "";
      const methodMap: Record<string, PlaybackMethod> = {
        DirectPlay: "direct_play",
        DirectStream: "direct_stream",
        Transcode: "transcode",
      };
      const method: PlaybackMethod = methodMap[rawMethod] ?? "transcode";

      const masterUrl = method !== "direct_play"
        ? `/api/v1/stream/${playId}/master.m3u8`
        : null;
      const directUrl = method === "direct_play"
        ? `/api/v1/stream/${playId}/direct`
        : null;

      isPlayingRef.current = true;
      setPlayingItemId(playId);
      setPlayerInfo({ playbackMethod: method, masterPlaylistUrl: masterUrl, directUrl });
      setShowPlayer(true);
    } catch {
      setPlayError(t('itemDetail.playbackError'));
    }
  }, [id, playingItemId, cleanupSession, t]);

  // Next episode lookup. Used both to prefetch its item data when the
  // current episode starts playing (warmer cache for the auto-advance
  // round-trip) and to feed the up-next overlay so it knows what to
  // promote when the current video ends.
  const nextEpisode = useMemo<MediaItem | undefined>(() => {
    if (!playingItemId || siblingEpisodes.length === 0) return undefined;
    const idx = siblingEpisodes.findIndex((ep) => ep.id === playingItemId);
    return idx >= 0 ? siblingEpisodes[idx + 1] : undefined;
  }, [playingItemId, siblingEpisodes]);

  useEffect(() => {
    if (!nextEpisode) return;
    queryClient.prefetchQuery({
      queryKey: queryKeys.item(nextEpisode.id),
      queryFn: () => api.getItem(nextEpisode.id),
      staleTime: 5 * 60 * 1000,
    });
  }, [nextEpisode, queryClient]);

  const nextUpInfo = useMemo(() => {
    if (!nextEpisode) return undefined;
    return {
      title: nextEpisode.title,
      seasonNumber: nextEpisode.season_number,
      episodeNumber: nextEpisode.episode_number,
      posterUrl: nextEpisode.poster_url,
      backdropUrl: nextEpisode.backdrop_url,
    };
  }, [nextEpisode]);

  // Convert backend ticks → seconds at this boundary so the player
  // and SeekBar stay unit-agnostic (they only know about seconds,
  // matching `<video>.currentTime`). 10_000_000 ticks per second is
  // the constant used everywhere else in the codebase.
  const chapterMarkers = useMemo(() => {
    if (!item?.chapters || item.chapters.length === 0) return undefined;
    return item.chapters.map((c) => ({
      startSeconds: c.start_ticks / 10_000_000,
      title: c.title,
    }));
  }, [item?.chapters]);

  const handlePlayerEnded = useCallback(() => {
    if (!playingItemId || siblingEpisodes.length === 0) return;
    const idx = siblingEpisodes.findIndex((ep) => ep.id === playingItemId);
    const nextEp = idx >= 0 ? siblingEpisodes[idx + 1] : undefined;
    if (!nextEp) return;

    setPlayingItemId(nextEp.id);
    (async () => {
      try {
        if (isPlayingRef.current && playingItemId) {
          await cleanupSession(playingItemId);
        }
        const info = await api.getStreamInfo(nextEp.id);
        const rawMethod = (info as Record<string, unknown>).method as string ?? "";
        const methodMap: Record<string, PlaybackMethod> = {
          DirectPlay: "direct_play", DirectStream: "direct_stream", Transcode: "transcode",
        };
        const method: PlaybackMethod = methodMap[rawMethod] ?? "transcode";
        isPlayingRef.current = true;
        setPlayerInfo({
          playbackMethod: method,
          masterPlaylistUrl: method !== "direct_play" ? `/api/v1/stream/${nextEp.id}/master.m3u8` : null,
          directUrl: method === "direct_play" ? `/api/v1/stream/${nextEp.id}/direct` : null,
        });
      } catch {
        setShowPlayer(false);
        setPlayerInfo(null);
      }
    })();
  }, [playingItemId, siblingEpisodes, cleanupSession]);

  const handleClosePlayer = useCallback(async () => {
    setShowPlayer(false);
    setPlayerInfo(null);
    setPlayingItemId(null);
    if (playingItemId || id) {
      await cleanupSession(playingItemId || id!);
    }
  }, [id, playingItemId, cleanupSession]);

  // ─── Render ─────────────────────────────────────────────────────────────

  if (isLoading) {
    return (
      <div className="flex min-h-[60vh] items-center justify-center">
        <Spinner size="lg" />
      </div>
    );
  }

  if (isError || !item) {
    return (
      <div className="flex min-h-[60vh] items-center justify-center">
        <EmptyState
          title={t('itemDetail.notFoundTitle')}
          description={t('itemDetail.notFoundDescription')}
          icon={
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={1.5}>
              <path
                strokeLinecap="round"
                strokeLinejoin="round"
                d="M12 9v3.75m9-.75a9 9 0 11-18 0 9 9 0 0118 0zm-9 3.75h.008v.008H12v-.008z"
              />
            </svg>
          }
        />
      </div>
    );
  }

  return (
    <div className="flex flex-col">
      {/* Video Player Overlay */}
      {showPlayer && playerInfo && (playingItemId || id) && (
        <VideoPlayer
          itemId={playingItemId || id!}
          sessionToken=""
          masterPlaylistUrl={playerInfo.masterPlaylistUrl}
          directUrl={playerInfo.directUrl}
          playbackMethod={playerInfo.playbackMethod}
          title={item.title}
          knownDuration={
            item.duration_ticks
              ? item.duration_ticks / 10_000_000
              : undefined
          }
          nextUp={nextUpInfo}
          chapters={chapterMarkers}
          audioStreams={item.media_streams?.filter((s) => s.type === "audio")}
          onClose={handleClosePlayer}
          onEnded={handlePlayerEnded}
        />
      )}

      {heroScope ? (
        <SeriesHero
          item={item}
          resumeMode={resumeTarget.mode}
          // Hero button stays a clean "Reproducir" regardless of
          // resume state — Plex / Netflix put the explicit "Sigue
          // viendo" affordance in its own panel below the hero
          // (see <ContinueWatchingPanel>) so the primary CTA above
          // the fold reads consistently. The button still resolves
          // to the same target the panel would (resume → next-up →
          // start), it just doesn't shout SXXEYY at the user.
          resumeLabel={t("common.play")}
          resumeProgressPercent={null}
          onPlay={() => {
            if (!resumeTarget.episode) return;
            if (heroScope === "season") {
              handlePlay(resumeTarget.episode.id);
              return;
            }
            // series scope: navigate so the episode's own surface
            // picks up audio tracks / next-up state.
            navigate(`/items/${resumeTarget.episode.id}`);
          }}
          onToggleFavorite={handleToggleFavorite}
          isFavorite={isFavorite}
          menuItems={menuItems}
        />
      ) : (
        <HeroSection
          item={item}
          onPlay={handlePlay}
          onToggleFavorite={handleToggleFavorite}
          isFavorite={isFavorite}
          menuItems={menuItems}
        />
      )}

      {playError && (
        <div className="mx-6 mt-4 rounded-[--radius-md] bg-error/10 px-4 py-3 text-sm text-error sm:mx-10">
          {playError}
        </div>
      )}

      <div className="flex flex-col gap-8 px-6 py-8 sm:px-10">
        {/* Media info */}
        {item.media_streams?.length > 0 && (
          <section id="media-info-section">
            <h2 className="mb-3 text-lg font-semibold text-text-primary">
              {t('itemDetail.mediaInfo')}
            </h2>
            <MediaMeta streams={item.media_streams} />
          </section>
        )}

        {/* Cast */}
        {item.people?.length > 0 && (
          <section>
            <h2 className="mb-3 text-lg font-semibold text-text-primary">
              {t('itemDetail.cast')}
            </h2>
            <div className="flex flex-wrap gap-3">
              {item.people.slice(0, 12).map((person) => (
                <div
                  key={`${person.name}-${person.role}`}
                  className="flex items-center gap-2 rounded-[--radius-md] bg-bg-elevated px-3 py-2"
                >
                  <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-full bg-bg-card text-xs font-bold text-text-muted">
                    {person.name.charAt(0)}
                  </div>
                  <div className="flex flex-col">
                    <span className="text-sm font-medium text-text-primary">
                      {person.name}
                    </span>
                    {person.role && (
                      <span className="text-xs text-text-muted">
                        {person.role}
                      </span>
                    )}
                  </div>
                </div>
              ))}
            </div>
          </section>
        )}

        {/* "Sigue viendo" panel — surfaces the resume-target episode
            as a one-row Jellyfin-style card with progress bar +
            synopsis. Lives below the hero (Netflix / Plex pattern)
            instead of squashing the affordance into the main button.
            Only renders when the user actually has progress on this
            entity — cold-start users see "Reproducir" in the hero
            and the seasons grid right below, no panel noise. */}
        {heroScope &&
          resumeTarget.mode === "resume" &&
          resumeTarget.episode && (
            <section>
              <h2 className="mb-3 text-lg font-semibold text-text-primary">
                {t("itemDetail.continueWatching")}
              </h2>
              <EpisodeRow
                item={resumeTarget.episode}
                onPlay={(epId) => {
                  // On the season page we play inline; on the series
                  // page we navigate so the VideoPlayer's title +
                  // up-next prefetch use the episode's own context
                  // instead of the series shell's.
                  if (heroScope === "season") {
                    handlePlay(epId);
                  } else {
                    navigate(`/items/${epId}`);
                  }
                }}
              />
            </section>
          )}

        {/* Seasons & Episodes (for series) */}
        {/* Series view shows the season grid only — episodes live on
            their own season-detail page. Season view shows the flat
            episode list directly under the hero. */}
        {item.type === "series" && <SeasonEpisodes seriesId={item.id} />}
        {item.type === "season" && (
          <section>
            <h2 className="mb-4 text-lg font-semibold text-text-primary">
              {t("itemDetail.episodes")}
            </h2>
            {/* Episode rows fire inline play through handlePlay so the
                user never leaves the season page. The VideoPlayer
                overlay at the top of this component renders over
                whatever is selected — same UX as Jellyfin's "play
                from this row" affordance. */}
            <SeasonEpisodeList seasonId={item.id} onPlay={handlePlay} />
          </section>
        )}
      </div>

      {/* Image Manager (admin only) */}
      {isAdmin && id && (
        <ImageManager
          itemId={id}
          isOpen={imageManagerOpen}
          onClose={() => setImageManagerOpen(false)}
        />
      )}
    </div>
  );
}

function SeasonEpisodes({ seriesId }: { seriesId: string }) {
  const { t } = useTranslation();
  const { data: children, isLoading } = useItemChildren(seriesId);

  if (isLoading) {
    return (
      <div className="flex justify-center py-8">
        <Spinner size="md" />
      </div>
    );
  }

  if (!children || children.length === 0) return null;

  const seasons = children.filter((c) => c.type === "season");
  const episodes = children.filter((c) => c.type === "episode");

  if (seasons.length > 0) {
    return <SeasonGrid seasons={seasons} />;
  }

  return (
    <section>
      <h2 className="mb-4 text-lg font-semibold text-text-primary">
        {t('itemDetail.episodes')}
      </h2>
      <div className="grid grid-cols-[repeat(auto-fill,minmax(280px,1fr))] gap-4">
        {episodes.map((ep) => (
          <EpisodeCard key={ep.id} item={ep} />
        ))}
      </div>
    </section>
  );
}

function SeasonGrid({ seasons }: { seasons: MediaItem[] }) {
  const { t } = useTranslation();
  const sorted = useMemo(
    () => [...seasons].sort((a, b) => (a.season_number ?? 0) - (b.season_number ?? 0)),
    [seasons],
  );

  return (
    <section>
      <h2 className="mb-4 text-lg font-semibold text-text-primary">
        {t("itemDetail.seasons")}
      </h2>

      <div className="grid grid-cols-[repeat(auto-fill,minmax(160px,1fr))] gap-4 sm:grid-cols-[repeat(auto-fill,minmax(180px,1fr))]">
        {sorted.map((season) => (
          <SeasonCard key={season.id} season={season} />
        ))}
      </div>
    </section>
  );
}

interface SeasonCardProps {
  season: MediaItem;
}

function SeasonCard({ season }: SeasonCardProps) {
  const { t } = useTranslation();
  const year = season.year ?? (season.premiere_date ? new Date(season.premiere_date).getFullYear() : null);
  const rating = season.community_rating;
  const epCount = season.episode_count;

  return (
    <Link
      to={`/items/${season.id}`}
      className={[
        "group flex flex-col gap-2 text-left outline-none rounded-[--radius-lg] transition-transform",
        "focus-visible:ring-2 focus-visible:ring-accent focus-visible:ring-offset-2 focus-visible:ring-offset-bg-card",
      ].join(" ")}
    >
      <div
        className={[
          "relative aspect-[2/3] overflow-hidden rounded-[--radius-lg] bg-bg-elevated transition-all duration-300",
          "ring-1 ring-transparent group-hover:ring-border group-hover:shadow-lg",
        ].join(" ")}
      >
        {season.poster_url ? (
          <img
            src={season.poster_url}
            alt={season.title}
            loading="lazy"
            className="h-full w-full object-cover transition-transform duration-300 group-hover:scale-[1.03]"
          />
        ) : (
          <div className="flex h-full w-full items-center justify-center bg-gradient-to-br from-bg-card to-bg-elevated">
            <span className="text-2xl font-bold text-text-muted">
              {season.season_number != null
                ? `S${String(season.season_number).padStart(2, "0")}`
                : season.title}
            </span>
          </div>
        )}

        {rating != null && (
          <div className="absolute top-2 right-2 flex items-center gap-1 rounded-full bg-black/70 px-2 py-1 text-xs font-semibold text-warning backdrop-blur-sm">
            <svg className="h-3 w-3" viewBox="0 0 24 24" fill="currentColor">
              <path d="M12 2l3.09 6.26L22 9.27l-5 4.87 1.18 6.88L12 17.77l-6.18 3.25L7 14.14 2 9.27l6.91-1.01L12 2z" />
            </svg>
            {rating.toFixed(1)}
          </div>
        )}
      </div>

      <div className="flex flex-col gap-0.5 px-0.5">
        <p className="truncate text-sm font-medium text-text-primary">
          {season.title}
        </p>
        <div className="flex items-center gap-2 text-xs text-text-muted">
          {year != null && <span>{year}</span>}
          {epCount != null && (
            <span>
              {t("itemDetail.episodeCount", { count: epCount })}
            </span>
          )}
        </div>
      </div>
    </Link>
  );
}

function SeasonEpisodeList({
  seasonId,
  onPlay,
}: {
  seasonId: string;
  onPlay?: (itemId: string) => void;
}) {
  const { data: episodes, isLoading } = useItemChildren(seasonId);

  if (isLoading) {
    return (
      <div className="flex justify-center py-8">
        <Spinner size="md" />
      </div>
    );
  }

  // When an onPlay handler is wired (season detail surface) we render
  // the rich Jellyfin-style EpisodeRow with synopsis + end time +
  // inline-play. Without it (legacy callers) we fall back to the
  // compact EpisodeCard which still navigates via Link.
  if (onPlay) {
    return (
      <div className="flex flex-col gap-2">
        {(episodes ?? []).map((ep) => (
          <EpisodeRow key={ep.id} item={ep} onPlay={onPlay} />
        ))}
      </div>
    );
  }

  return (
    <div className="grid grid-cols-[repeat(auto-fill,minmax(280px,1fr))] gap-4">
      {(episodes ?? []).map((ep) => (
        <EpisodeCard key={ep.id} item={ep} />
      ))}
    </div>
  );
}
