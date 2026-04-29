import { useState, useCallback, useMemo } from "react";
import { useParams, useNavigate } from "react-router";
import { useTranslation } from "react-i18next";
import { useQueryClient } from "@tanstack/react-query";
import { useItem, useItemChildren, useToggleFavorite, queryKeys } from "@/api/hooks";
import type { MediaItem } from "@/api/types";
import { Spinner, EmptyState } from "@/components/common";
import { HeroSection, SeriesHero, MediaMeta, EpisodeRow } from "@/components/media";
import type { HeroMenuItem } from "@/components/media/HeroSection";
import { VideoPlayer } from "@/components/player";
import { ImageManager } from "@/components/ImageManager";
import { useAuthStore } from "@/store/auth";
import { useResumeTarget } from "@/hooks/useSeriesResumeTarget";
import { usePlayback } from "./itemDetail/usePlayback";
import { SeasonEpisodes, SeasonEpisodeList } from "./itemDetail/season";

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

  // Sibling episodes for auto-advance + the resume rail. Pure
  // derivation (filter + sort) — useMemo-able. Episodes-only;
  // movies and series get an empty list and the playback hook
  // treats it as "no auto-advance available".
  const parentId = item?.parent_id;
  const { data: siblings } = useItemChildren(parentId ?? "", {
    enabled: !!parentId && item?.type === "episode",
  });
  const siblingEpisodes = useMemo<MediaItem[]>(() => {
    if (!siblings || siblings.length === 0) return [];
    return siblings
      .filter((s) => s.type === "episode")
      .sort((a, b) => (a.episode_number ?? 0) - (b.episode_number ?? 0));
  }, [siblings]);

  // Playback machinery (overlay state + handlers + auto-advance +
  // session cleanup) lives in usePlayback; this page just wires the
  // returned handlers to the hero buttons and the VideoPlayer.
  const {
    showPlayer,
    playerInfo,
    playingItemId,
    playError,
    nextUpInfo,
    handlePlay,
    handlePlayerEnded,
    handleClosePlayer,
  } = usePlayback({ pageItemId: id, siblingEpisodes });

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
