import { useState, useCallback, useEffect, useMemo, useRef } from "react";
import { useParams, useNavigate, useSearchParams } from "react-router";
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
import { RecommendationsRail } from "@/components/media/RecommendationsRail";
import { VideoPlayer } from "@/components/player";
import { ImageManager } from "@/components/ImageManager";
import { useAuthStore } from "@/store/auth";
import { useResumeTarget } from "@/hooks/useSeriesResumeTarget";
import { useVibrantColors } from "@/hooks/useVibrantColors";
import { usePlayback } from "./itemDetail/usePlayback";
import { SeasonEpisodes, SeasonEpisodeList } from "./itemDetail/season";
import { buildAuroraStyle } from "./itemDetail/aurora";
import { useDetailMenu } from "./itemDetail/useDetailMenu";

export default function ItemDetail() {
  const { t } = useTranslation();
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const [searchParams, setSearchParams] = useSearchParams();
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

  // ─── Auto-play deep-link (?play=1) ──────────────────────────────────────
  // Lets surfaces like the home hero and Continue Watching launch the
  // player directly instead of dropping the user on the detail page
  // and asking them to click again. Movies / episodes call handlePlay;
  // series + season scopes resolve through the same resume-target the
  // hero's Reproducir button uses, then forward to the episode's own
  // route so audio-track / next-up state hydrates correctly.
  const autoPlayConsumed = useRef(false);
  useEffect(() => {
    if (autoPlayConsumed.current) return;
    if (searchParams.get("play") !== "1") return;
    if (!item) return;

    if (heroScope === "series" || heroScope === "season") {
      if (!resumeTarget.episode) return;
      autoPlayConsumed.current = true;
      // Strip the param then jump to the episode with auto-play
      // preserved so its detail surface launches the overlay.
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev);
          next.delete("play");
          return next;
        },
        { replace: true },
      );
      navigate(`/items/${resumeTarget.episode.id}?play=1`, { replace: true });
      return;
    }

    autoPlayConsumed.current = true;
    setSearchParams(
      (prev) => {
        const next = new URLSearchParams(prev);
        next.delete("play");
        return next;
      },
      { replace: true },
    );
    void handlePlay();
  }, [item, heroScope, resumeTarget.episode, searchParams, setSearchParams, navigate, handlePlay]);

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

  // Runtime palette extraction — must run on EVERY render (Rules of
  // Hooks) so it sits above the early returns. Server-side palette
  // wins when present; this fallback only fires for items scanned
  // before colour extraction shipped (and for federated remotes
  // that haven't re-scanned). The fallback URL chain mirrors
  // HeroSection so the swatch we tint with matches the swatch the
  // hero already paints with.
  const hasServerPalette = !!(
    item?.backdrop_colors?.vibrant || item?.backdrop_colors?.muted
  );
  const isSubItem = item?.type === "season" || item?.type === "episode";
  const fallbackUrl = hasServerPalette
    ? null
    : item?.backdrop_url ??
      (isSubItem ? item?.series_backdrop_url : undefined) ??
      item?.poster_url ??
      null;
  const runtimePalette = useVibrantColors(fallbackUrl);

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
  // Composed palette — server-side `backdrop_colors` is canonical
  // (extracted at scan time from the same source the hero shows)
  // but the legacy server schema only stores vibrant + muted; the
  // four-corner Plex composition wants up to four distinct
  // swatches, so the darkVibrant / lightVibrant / lightMuted
  // slots come from the runtime extractor (node-vibrant resolves
  // them in one pass alongside the basic two). Page tint stays
  // consistent with the hero because both consume the same merged
  // object.
  const { detailStyle, auroraBackground } = buildAuroraStyle({
    vibrant: item.backdrop_colors?.vibrant ?? runtimePalette.vibrant ?? undefined,
    muted: item.backdrop_colors?.muted ?? runtimePalette.muted ?? undefined,
    darkVibrant: runtimePalette.darkVibrant ?? undefined,
    lightVibrant: runtimePalette.lightVibrant ?? undefined,
    lightMuted: runtimePalette.lightMuted ?? undefined,
  });

  // Apply the aurora directly to the page wrapper instead of a
  // separate `position: fixed; -z-10` canvas. The fixed-canvas
  // approach lost a stacking battle against the body's bg-base
  // propagation in some browsers — the tint painted but stayed
  // invisible behind the propagated canvas colour. Painting on the
  // wrapper sidesteps the z-index war entirely: the wrapper IS the
  // surface every section sits on, so its background paints under
  // them by definition. min-h-screen guarantees the colour fills
  // the viewport even when the page content is shorter than the
  // window (small movies with no cast / extras).
  const wrapperStyle = auroraBackground
    ? { ...detailStyle, ...auroraBackground }
    : detailStyle;

  return (
    // -mx-4 md:-mx-6 cancels the AppLayout <main> px gutter so the
    // page-tint background reaches the very edges of the viewport
    // (Plex never has that grey strip on the right between the
    // coloured page and the scrollbar). pb cancels main's pb-4/6.
    // The hero already lives at the wrapper's edges; body content
    // re-adds horizontal padding via px-* on its own section.
    <div
      className="flex flex-col min-h-screen -mx-4 md:-mx-6 -mb-4 md:-mb-6"
      style={wrapperStyle}
    >
      {/* No separate aurora canvas — the wrapper carries the colour
          itself (see comment above the wrapperStyle composition).
          --detail-tint is still published on the wrapper so the
          hero's bottom-fade gradient lands on the same swatch. */}

      {/* Video Player Overlay */}
      {showPlayer && playerInfo && (playingItemId || id) && (
        <VideoPlayer
          itemId={playingItemId || id!}
          sessionToken=""
          masterPlaylistUrl={playerInfo.masterPlaylistUrl}
          directUrl={playerInfo.directUrl}
          playbackMethod={playerInfo.playbackMethod}
          title={item.title}
          logoUrl={item.logo_url ?? undefined}
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
          people={item.people}
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
        {/* Section order, Plex/Jellyfin-style: the most actionable
            content sits closest to the hero so the user doesn't have
            to scroll past technical metadata to find "where do I
            click to keep watching".
              1. Continue watching (series scope, resume available)
              2. Seasons / episodes (series + season scope)
              3. Cast
              4. More like this
              5. Media info (technical detail; lowest signal)
            Movies skip 1 + 2 and fall straight to cast — the same
            order then degrades gracefully without a special case. */}

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

        {/* "More like this" rail — TMDb /recommendations cross-
            referenced with the local library. Hides itself when no
            candidates were returned (no TMDb match, no provider
            configured, or empty list). Movies and series pages get
            it; episode/season detail surfaces don't bother because
            their parent series already shows the rail. */}
        {(item.type === "movie" || item.type === "series") && id && (
          <RecommendationsRail itemId={id} />
        )}

        {/* Media info — technical metadata (codecs, audio tracks,
            subtitles). Lowest signal for a casual user, highest for
            an admin debugging playback, so it sits at the bottom
            where it stays out of the way but remains findable. */}
        {item.media_streams?.length > 0 && (
          <section id="media-info-section">
            <h2 className="mb-3 text-lg font-semibold text-text-primary">
              {t('itemDetail.mediaInfo')}
            </h2>
            <MediaMeta streams={item.media_streams} />
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
