import { useState, useCallback, useMemo, type CSSProperties } from "react";
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
import type { Person } from "@/api/types";

// CastChip renders one entry of the cast/crew strip in the Plex style:
// a generous circular avatar stacked over the name and the
// character/role line. No card chrome around it — the avatar IS the
// frame, and the surrounding hero/page tint reads through. Failed
// photo loads (broken URL, 404 from the people thumb endpoint) flip
// to an initial-letter placeholder via `onError`; the failure state
// keys off the URL so a re-fetch with a new URL retries instead of
// inheriting the previous failure.
function CastChip({ person }: { person: Person }) {
  const [failedUrl, setFailedUrl] = useState<string | null>(null);
  const showImage = !!person.image_url && failedUrl !== person.image_url;
  // Actor entries put the character name on the second line; crew
  // entries (director, writer, producer) put the role label there
  // because the role IS the descriptor for them.
  const subtitle = person.character || person.role;

  return (
    <div className="flex w-[120px] flex-col items-center gap-2 text-center">
      <div className="flex h-24 w-24 shrink-0 items-center justify-center overflow-hidden rounded-full bg-bg-elevated text-xl font-bold text-text-muted ring-1 ring-border/40 sm:h-28 sm:w-28">
        {showImage ? (
          <img
            src={person.image_url}
            alt={person.name}
            loading="lazy"
            className="h-full w-full object-cover"
            onError={() => setFailedUrl(person.image_url ?? null)}
          />
        ) : (
          person.name.charAt(0)
        )}
      </div>
      <div className="flex flex-col gap-0.5">
        <span className="line-clamp-2 text-sm font-medium leading-snug text-text-primary">
          {person.name}
        </span>
        {subtitle && (
          <span className="line-clamp-2 text-xs leading-snug text-text-muted">
            {subtitle}
          </span>
        )}
      </div>
    </div>
  );
}

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

    // External-provider deep links. We only render the entries we know
    // how to URL-build, so an unknown provider key in the wire (e.g.
    // a future "wikidata") is silently ignored rather than emitting a
    // dead "Open in wikidata" row pointing nowhere. Series prefer the
    // tv subpath on TMDb; movies/episodes use /movie/. Episodes don't
    // get their own IMDb deep-link surface — TMDb folds them under the
    // show — so we suppress per-episode rows when the id matches a
    // show-level id from the series above (best-effort).
    if (item?.external_ids) {
      const ids = item.external_ids;
      const tmdbType: "tv" | "movie" =
        item.type === "series" || item.type === "season" || item.type === "episode"
          ? "tv"
          : "movie";

      if (ids.imdb) {
        items.push({
          label: t("itemDetail.openInIMDb", { defaultValue: "Ver en IMDb" }),
          icon: (
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} className="h-4 w-4">
              <path strokeLinecap="round" strokeLinejoin="round" d="M13.5 6H5.25A2.25 2.25 0 003 8.25v10.5A2.25 2.25 0 005.25 21h10.5A2.25 2.25 0 0018 18.75V10.5m-10.5 6L21 3m0 0h-5.25M21 3v5.25" />
            </svg>
          ),
          onClick: () => {
            window.open(`https://www.imdb.com/title/${ids.imdb}/`, "_blank", "noopener,noreferrer");
          },
        });
      }
      if (ids.tmdb) {
        items.push({
          label: t("itemDetail.openInTMDb", { defaultValue: "Ver en TMDb" }),
          icon: (
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} className="h-4 w-4">
              <path strokeLinecap="round" strokeLinejoin="round" d="M13.5 6H5.25A2.25 2.25 0 003 8.25v10.5A2.25 2.25 0 005.25 21h10.5A2.25 2.25 0 0018 18.75V10.5m-10.5 6L21 3m0 0h-5.25M21 3v5.25" />
            </svg>
          ),
          onClick: () => {
            window.open(`https://www.themoviedb.org/${tmdbType}/${ids.tmdb}`, "_blank", "noopener,noreferrer");
          },
        });
      }
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

  // Premium colour-bleed — PS3-XMB style "ambient aurora". Each
  // detail page tints the entire viewport with a layered background
  // built from the cover's dominant palette: a flat tint base, plus
  // two large soft radial blobs (vibrant in the upper-left, muted
  // in the lower-right) that read as drifting clouds of colour
  // rather than a single flat wash. Each page therefore feels
  // personalised by its cover art without ever leaving the
  // self-hosted dark aesthetic.
  //
  // Why a fixed full-viewport layer and not a `backgroundColor` on
  // the wrapper: a wrapper-level background only paints inside the
  // AppLayout content gutters and creates a visibly different
  // colour band against AppLayout's own bg-base — read as a
  // "displaced container" around the seasons grid (the user has
  // flagged that twice). Painting on a `position: fixed, inset-0,
  // -z-10` layer makes the tint the page canvas itself: edge-to-
  // edge of the viewport, behind every sibling, unmounts cleanly
  // when the user navigates away.
  //
  // The CSS variable `--detail-tint` is also published on the
  // wrapper so the hero's bottom-fade gradient targets the exact
  // base colour the canvas paints in (no visible seam between hero
  // and the rest of the page).
  const palette = item.backdrop_colors;
  const tintSeed = palette?.muted ?? palette?.vibrant;
  const tintBase = tintSeed
    ? `color-mix(in srgb, ${tintSeed} 14%, rgb(8 12 16))`
    : null;
  const detailStyle: CSSProperties | undefined = tintBase
    ? { ['--detail-tint' as string]: tintBase }
    : undefined;
  // Layered radial-gradient stack for the ambient aurora background.
  // Built ONLY when the item has a palette — otherwise the page
  // falls through to plain bg-base and reads exactly like the rest
  // of the app. Each blob is tuned for low intensity (≤30%) so the
  // foreground content (titles, badges, seasons grid) keeps
  // unambiguous contrast against the canvas; intentionally kept
  // static rather than animated, both to respect the user's reduce-
  // motion preference by default and to avoid the GPU cost of an
  // always-painting full-viewport composite.
  const auroraBackground = (() => {
    if (!tintBase) return undefined;
    const vibrant = palette?.vibrant;
    const muted = palette?.muted;
    // Both blobs prefer the VIBRANT swatch — by definition it's the
    // most saturated colour the palette extracted, so it carries the
    // page identity. Muted falls in only as a backstop for items
    // whose vibrant slot couldn't be filled (rare, but happens on
    // monochrome posters). Earlier revisions used muted for the
    // lower-right blob, which read as "soso" because muted IS by
    // definition desaturated; the lower half of the page is exactly
    // where the user spends the most time scrolling, so it's the
    // wrong place to dial colour back.
    const primary = vibrant ?? muted;
    const secondary = muted ?? vibrant;
    const layers: string[] = [];
    if (primary) {
      // Upper-left vibrant blob — covers the hero left side and
      // bleeds into the seasons-grid headline area. Big radius so
      // the bleed reads as "the whole top of the page is tinted",
      // not "there's a circle of red here".
      layers.push(
        `radial-gradient(ellipse 100% 80% at 10% 0%, color-mix(in srgb, ${primary} 60%, transparent) 0%, transparent 65%)`,
      );
    }
    if (primary) {
      // Lower-right vibrant blob — the seasons grid + cast strip
      // sit here. Same vibrant swatch but slightly muted via the
      // mix percentage so foreground text stays readable.
      layers.push(
        `radial-gradient(ellipse 90% 90% at 90% 100%, color-mix(in srgb, ${primary} 50%, transparent) 0%, transparent 70%)`,
      );
    }
    if (secondary) {
      // Cooler counter-blob: balances the warm primary with a
      // softer accent so the whole canvas isn't a single hue.
      layers.push(
        `radial-gradient(circle 55% at 50% 55%, color-mix(in srgb, ${secondary} 28%, transparent) 0%, transparent 75%)`,
      );
    }
    return {
      backgroundColor: tintBase,
      backgroundImage: layers.join(", "),
    };
  })();

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
