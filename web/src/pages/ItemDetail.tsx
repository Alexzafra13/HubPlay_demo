import { useState, useCallback, useMemo } from "react";
import { useParams, useNavigate } from "react-router";
import { useTranslation } from "react-i18next";
import { useItem, useItemChildren, useToggleFavorite } from "@/api/hooks";
import type { MediaItem } from "@/api/types";
import { Spinner, EmptyState } from "@/components/common";
import {
  HeroSection,
  SeriesHero,
  MediaMeta,
  EpisodeRow,
  CastChip,
} from "@/components/media";
import { VideoPlayer } from "@/components/player";
import { ImageManager } from "@/components/ImageManager";
import { useAuthStore } from "@/store/auth";
import { useResumeTarget } from "@/hooks/useSeriesResumeTarget";
import { usePlayback } from "./itemDetail/usePlayback";
import { SeasonEpisodes, SeasonEpisodeList } from "./itemDetail/season";
import { buildAuroraStyle } from "./itemDetail/aurora";
import { useDetailMenu } from "./itemDetail/useDetailMenu";

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

  // Hero kebab menu rows. Composition + provider deep-link rules live
  // in the hook so the page stays presentation-glue.
  const menuItems = useDetailMenu({
    item,
    itemId: id,
    isAdmin,
    onOpenImageManager: () => setImageManagerOpen(true),
  });

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

  // Premium colour-bleed — PS3-XMB style "ambient aurora". The
  // gradient math is in itemDetail/aurora.ts so it can be tested
  // without rendering JSX; here we just consume the result.
  //
  // Why a fixed full-viewport layer and not a `backgroundColor` on
  // the wrapper: a wrapper-level background only paints inside the
  // AppLayout content gutters and creates a visibly different
  // colour band against AppLayout's own bg-base — read as a
  // "displaced container" around the seasons grid. Painting on a
  // `position: fixed, inset-0, -z-10` layer makes the tint the page
  // canvas itself: edge-to-edge of the viewport, behind every
  // sibling, unmounts cleanly when the user navigates away.
  //
  // The CSS variable `--detail-tint` is also published on the
  // wrapper so the hero's bottom-fade gradient targets the exact
  // base colour the canvas paints in (no visible seam between hero
  // and the rest of the page).
  const { detailStyle, auroraBackground } = buildAuroraStyle(
    item.backdrop_colors,
  );

  return (
    <div className="flex flex-col" style={detailStyle}>
      {/* Page-wide ambient-aurora canvas — fixed, full viewport,
          behind every other layer. Layered radial gradients give
          the surface a PS3-XMB-style cloud-of-colour quality: the
          page reads as personalised by the cover art rather than
          a flat tint. Only mounts when we actually have a palette;
          otherwise the body's bg-base shows through and the page
          looks identical to the rest of the app. */}
      {auroraBackground && (
        <div
          aria-hidden="true"
          className="fixed inset-0 -z-10"
          style={auroraBackground}
        />
      )}

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

        {/* Cast / crew. The chip stays the same shape whether or not
            we have a profile photo — the avatar slot either renders
            an <img> with onError fallback to the initial chip, or
            jumps straight to the initial chip when image_url is
            absent. Limited to the first 12 entries server-side
            ordering (TMDb billing position) so the most-recognised
            faces lead. */}
        {item.people && item.people.length > 0 && (
          <section>
            <h2 className="mb-3 text-lg font-semibold text-text-primary">
              {t('itemDetail.cast')}
            </h2>
            <div className="flex flex-wrap gap-3">
              {item.people.slice(0, 12).map((person) => (
                <CastChip key={person.id} person={person} />
              ))}
            </div>
          </section>
        )}

        {/* "Sigue viendo" panel — series scope only. Surfaces the
            resume-target episode as a one-row Jellyfin-style card so
            the user can resume in one click without scrolling
            through seasons + episodes to find their spot.
            Suppressed on the SEASON page on purpose: the season's
            full episode list renders right below the hero anyway,
            and the in-progress episode in that list already shows
            its own progress bar + resume affordance — surfacing it
            twice (panel above + row in the list) reads as
            duplication. Cold-start users on the series page see
            "Reproducir" in the hero and the seasons grid right
            below, no panel noise. */}
        {heroScope === "series" &&
          resumeTarget.mode === "resume" &&
          resumeTarget.episode && (
            <section>
              <h2 className="mb-3 text-lg font-semibold text-text-primary">
                {t("itemDetail.continueWatching")}
              </h2>
              <EpisodeRow
                item={resumeTarget.episode}
                onPlay={(epId) => {
                  // Series page: navigate so the VideoPlayer's title
                  // + up-next prefetch use the episode's own context
                  // instead of the series shell's.
                  navigate(`/items/${epId}`);
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
