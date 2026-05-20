import { useEffect, useRef, useState, type FC, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import type { MediaItem } from "@/api/types";
import { Badge } from "@/components/common/Badge";
import type { HeroMenuItem } from "./HeroSection";
import { HeroTrailer } from "./HeroTrailer";
import { ExternalIdRow, OverviewWithReadMore, StudioMark } from "./heroMeta";
import { formatPremiereDate } from "@/utils/heroMeta";
import { useUserPreference } from "@/api/hooks";
import { TRAILERS_ENABLED_PREF_KEY } from "@/utils/playbackPrefs";
import type { SeriesResumeMode } from "@/hooks/useSeriesResumeTarget";

interface SeriesHeroProps {
  item: MediaItem;
  resumeMode: SeriesResumeMode;
  resumeLabel: ReactNode; // "Reproducir" or "Seguir viendo S01E03"
  resumeProgressPercent: number | null;
  onPlay: () => void;
  onToggleFavorite: () => void;
  isFavorite: boolean;
  menuItems: HeroMenuItem[];
}

function formatRating(rating: number): string {
  return rating.toFixed(1);
}

/**
 * SeriesHero — full-bleed hero for the series detail page.
 *
 * Layout, in order of stacking from bottom up:
 *
 *   1. Backdrop image, full-width, right-aligned crop. Uses absolute
 *      inset-0 so it spans from the sidebar edge to the viewport's
 *      right edge. The wrapper bleeds out of AppLayout's px-4/px-6
 *      via negative margins so we don't see the sidebar gutter on
 *      the left side.
 *
 *   2. Color-fade overlay on the LEFT 50%. Uses two CSS variables
 *      driven by node-vibrant's runtime palette extraction (vibrant
 *      + dark-muted) — we set them on the root and the gradient picks
 *      them up. Falls back to `bg-base` when the palette is still
 *      loading or extraction failed; that fallback is also why the
 *      gradient sits BELOW the content layer, never above (otherwise
 *      a slow extraction would make the title flash).
 *
 *   3. Content column, anchored to the LEFT 30-40% of the hero:
 *        • poster (vertically centered against the hero)
 *        • title block immediately below: title, meta row, overview
 *        • action buttons: smart Play, favorite, kebab
 *
 * Why a separate component from HeroSection:
 *   The movie / season / episode pages still want the original
 *   right-aligned layout (poster left, info right). This new layout
 *   is series-only and inverts that arrangement deliberately — the
 *   poster + info form a vertical column on the left side, leaving
 *   the right two-thirds for the backdrop image to dominate. Trying
 *   to gate this in HeroSection via a "mode" prop would couple two
 *   layouts that share basically nothing visually.
 */
const SeriesHero: FC<SeriesHeroProps> = ({
  item,
  resumeMode,
  resumeLabel,
  resumeProgressPercent,
  onPlay,
  onToggleFavorite,
  isFavorite,
  menuItems,
}) => {
  const { t, i18n } = useTranslation();
  const firstAirDate = formatPremiereDate(item.premiere_date, i18n.language);
  // Backdrop resolution priority:
  //   1. The item's own backdrop (full series, when present).
  //   2. The series's backdrop folded in by attachSeriesContext —
  //      relevant for season pages, which have a poster but no
  //      backdrop of their own. Falling back to the season's vertical
  //      poster looked terrible (stretched + portrait orientation in
  //      a 16:7 hero), so the series backdrop wins this slot before
  //      the poster ever does.
  //   3. The item's own poster (last resort, blurred underpainting).
  const heroBackdropUrl =
    item.backdrop_url ?? item.series_backdrop_url ?? item.poster_url ?? null;

  // Prefer the backend-precomputed palette (no decode delay, no extra
  // image fetch) and fall back to runtime extraction only when the
  // primary image was ingested before the extraction code shipped.
  // The hook is a no-op when imageUrl is null, so we skip the fetch
  // entirely on rows whose colours already arrived with the response.
  // Palette extraction lives in ItemDetail.tsx now (it owns the
  // page-wide aurora + publishes `--detail-tint`). The hero just
  // consumes that variable via the bottom-fade and the backdrop
  // image's mask — no per-hero palette state needed here.

  // When the trailer reveals, the static backdrop image fades out
  // so the two layers don't fight for attention (Plex / Netflix do
  // the same). Tracked here (not inside HeroTrailer) because the
  // <img> sits in a sibling tree — coordination has to happen at
  // the common parent.
  const [trailerActive, setTrailerActive] = useState(false);

  // Cross-session opt-out, persisted in user_preferences. Default
  // true so a fresh account gets the Netflix-style preview without
  // having to find a setting first; users that find it intrusive
  // toggle it off in Settings → Reproducción and the embed never
  // mounts again on any device.
  const [trailersEnabled] = useUserPreference<boolean>(
    TRAILERS_ENABLED_PREF_KEY,
    true,
  );

  return (
    <section
      className="relative overflow-hidden"
      style={{
        // Bleed behind the sticky TopBar so the backdrop reaches the
        // very top of the viewport. The TopBar paints over it at z-30
        // and goes glass-blurred on scroll (see TopBar.tsx). Same
        // pattern as the Home page hero — keeps the visual language
        // consistent across surfaces.
        marginTop: "calc(var(--topbar-height) * -1)",
      }}
    >
      {/* Backdrop layer — fixed-height band so the season grid below
          stays visible above the fold on a typical 1080p viewport
          (~960px usable after browser chrome).

          On wide desktop windows `object-cover` scales the natively
          16:9 TMDb backdrop to fit the viewport width, which means
          a band shorter than ~560px on lg ends up showing only the
          centre slice of the image and reads as "very zoomed in".
          Two levers fight that: a generous min-height so we reveal
          more of the scaled image, and `object-position: right top`
          so what we DO reveal includes the upper third where actor
          faces typically sit (instead of cropping heads off, which
          the previous `object-right` centre-anchor did). */}
      <div className="relative min-h-[400px] sm:min-h-[440px] lg:min-h-[480px]">
        {/* Plex-style backdrop placement — see HeroSection.tsx for the
            full rationale. Image lives on the right ~2/3 of the hero
            and is masked into the page colour on its left edge, so
            the page's 4-corner aurora shows through under the
            poster/title column. */}
        {heroBackdropUrl ? (
          <img
            src={heroBackdropUrl}
            alt=""
            loading="eager"
            className={[
              "absolute inset-y-0 right-0 size-full sm:w-4/5 lg:w-2/3 object-cover transition-opacity duration-700",
              // Fade the static backdrop out when the trailer
              // reveals so the two layers don't fight for attention.
              // The transition matches HeroTrailer's own 700ms
              // opacity fade so the swap reads as a single move.
              trailerActive ? "opacity-0" : "opacity-100",
            ].join(" ")}
            style={{
              objectPosition: "right top",
              maskImage:
                "linear-gradient(to right, transparent 0%, rgba(0,0,0,0.2) 25%, rgba(0,0,0,0.85) 55%, black 75%), linear-gradient(to bottom, black 55%, rgba(0,0,0,0.2) 92%, transparent 100%)",
              WebkitMaskImage:
                "linear-gradient(to right, transparent 0%, rgba(0,0,0,0.2) 25%, rgba(0,0,0,0.85) 55%, black 75%), linear-gradient(to bottom, black 55%, rgba(0,0,0,0.2) 92%, transparent 100%)",
              maskComposite: "intersect",
              WebkitMaskComposite: "source-in",
            }}
          />
        ) : (
          <div className="absolute inset-0 bg-gradient-to-br from-bg-elevated to-bg-card" />
        )}

        {/* Netflix-style preview: a couple of seconds after the page
            settles, fade in a muted trailer over the backdrop. The
            HeroTrailer component handles the timer, the embed URL
            (YouTube / Vimeo) and graceful dismissal — when there's
            no trailer it renders nothing. The reveal/dismiss
            callbacks let SeriesHero fade the static backdrop out
            while the trailer is on screen so they don't blend. */}
        {item.trailer && (
          <HeroTrailer
            siteKey={item.trailer.site}
            videoKey={item.trailer.key}
            userOptedOut={!trailersEnabled}
            onReveal={() => setTrailerActive(true)}
            onDismiss={() => setTrailerActive(false)}
          />
        )}

        {/* Left-side gradient overlay removed — the image's own
            mask (above) plus the page's 4-corner aurora handle the
            same job without an extra layer. Keeping this comment as
            a tombstone so future maintainers don't reintroduce a
            duplicate fade thinking it's missing. */}
        {/* Bottom-fade overlay removed — the image's own mask
            (above) now fades the bottom edge to transparent so the
            page background flows through uninterrupted. The old
            overlay painted a solid `--detail-tint` band that left
            a visible horizontal seam against the page's 4-corner
            aurora (the colour at the centre-bottom of the page does
            not match `--detail-tint`). */}

        {/* Content column. `max-w` keeps it on the left third on wide
            screens; on mobile it stretches and the gradient extends
            further across. Vertical centering matches Plex / Jellyfin
            where the poster sits in the optical centre of the hero.
            `pt-[topbar]` keeps the poster from sliding behind the
            translucent TopBar — the backdrop bleeds up there but
            interactive content stays clear. */}
        <div
          className="relative z-10 flex h-full items-center px-6 pb-12 sm:px-10 lg:px-16"
          style={{ paddingTop: "calc(var(--topbar-height) + 1.5rem)" }}
        >
          <div className="flex w-full max-w-md flex-col items-start gap-5 lg:max-w-lg">
            {item.poster_url && (
              <img
                src={item.poster_url}
                alt={item.title}
                className="h-[240px] w-auto rounded-[--radius-lg] shadow-2xl shadow-black/60 object-cover sm:h-[280px] lg:h-[340px]"
              />
            )}

            <div className="flex flex-col gap-3">
              {item.logo_url ? (
                <img
                  src={item.logo_url}
                  alt={item.title}
                  className="max-h-[60px] w-auto max-w-full object-contain object-left drop-shadow-lg"
                />
              ) : (
                <h1 className="text-3xl font-semibold text-text-primary drop-shadow-lg sm:text-4xl">
                  {item.title}
                </h1>
              )}

              {/* Tagline sits between the title and the meta badges.
                  Plex and TMDb both surface this string ("With great
                  power comes great responsibility") right under the
                  title — italic, single line, dimmer than the
                  overview. Hidden when empty so series without a
                  tagline don't get a phantom row. */}
              {item.tagline && (
                <p className="-mt-1 max-w-xl text-sm italic text-text-primary/80 drop-shadow-md">
                  {item.tagline}
                </p>
              )}

              {/* Network / studio — own line above the chip row,
                  Plex-style. Sits between tagline and the year /
                  rating / genre badges so HBO, Disney+, etc. read
                  with proper visual weight instead of competing
                  inline. */}
              {(item.studio_logo_url || item.studio) && (
                <div className="pt-0.5">
                  <StudioMark
                    studio={item.studio}
                    studioLogoUrl={item.studio_logo_url}
                    studioSlug={item.studio_slug}
                  />
                </div>
              )}

              <div className="flex flex-wrap items-center gap-2 text-sm text-text-primary/85">
                {/* Series air date: prefer the full first-air-date
                    when TMDb returned one (matches Plex's "Sep 8,
                    2017" treatment), fall back to the bare year
                    otherwise. Locale-aware via the active i18n
                    language. */}
                {firstAirDate ? (
                  <span className="font-medium">{firstAirDate}</span>
                ) : item.year != null ? (
                  <span className="font-medium">{item.year}</span>
                ) : null}
                {item.community_rating != null && (
                  <Badge variant="warning">
                    <svg className="size-3" viewBox="0 0 24 24" fill="currentColor">
                      <path d="M12 2l3.09 6.26L22 9.27l-5 4.87 1.18 6.88L12 17.77l-6.18 3.25L7 14.14 2 9.27l6.91-1.01L12 2z" />
                    </svg>
                    {formatRating(item.community_rating)}
                  </Badge>
                )}
                {item.content_rating != null && <Badge>{item.content_rating}</Badge>}
                {item.genres?.slice(0, 3).map((g) => (
                  <Badge key={g}>{g}</Badge>
                ))}
              </div>

              {/* Watched-count aggregate — only present on the series
                  scope and only when the user is authenticated. Shown
                  as a thin progress bar with the count alongside, so
                  a glance at the hero answers "how far am I in this
                  show?" without scrolling to the seasons grid. */}
              {item.episode_progress && item.episode_progress.total > 0 && (
                <div className="flex items-center gap-3 text-xs text-text-primary/85">
                  <div
                    className="h-1.5 w-32 overflow-hidden rounded-full bg-bg-elevated/60"
                    role="progressbar"
                    aria-valuemin={0}
                    aria-valuemax={item.episode_progress.total}
                    aria-valuenow={item.episode_progress.watched}
                  >
                    <div
                      className="h-full bg-accent transition-all duration-500"
                      style={{
                        width: `${Math.min(
                          100,
                          (item.episode_progress.watched /
                            item.episode_progress.total) *
                            100,
                        )}%`,
                      }}
                    />
                  </div>
                  <span>
                    {item.episode_progress.watched >= item.episode_progress.total
                      ? t("itemDetail.episodesAllWatched", {
                          total: item.episode_progress.total,
                        })
                      : t("itemDetail.episodesWatched", {
                          watched: item.episode_progress.watched,
                          total: item.episode_progress.total,
                        })}
                  </span>
                </div>
              )}

              <OverviewWithReadMore overview={item.overview} />

              {/* External-ID chips (IMDb / TMDb / TVDb). Empty row
                  hides itself so series without a TMDb match keep
                  the original spacing. */}
              <ExternalIdRow item={item} />

              <div className="flex items-center gap-3 pt-1">
                <button
                  type="button"
                  onClick={onPlay}
                  disabled={resumeMode === "none"}
                  className="group relative flex items-center gap-2 overflow-hidden rounded-[--radius-md] bg-accent px-5 py-2.5 text-sm font-semibold text-white shadow-lg shadow-accent/30 transition-all hover:bg-accent-hover hover:shadow-accent/40 disabled:cursor-not-allowed disabled:opacity-50 disabled:shadow-none cursor-pointer"
                >
                  <svg className="size-5 shrink-0" viewBox="0 0 24 24" fill="currentColor">
                    <path d="M8 5v14l11-7z" />
                  </svg>
                  <span>{resumeLabel}</span>
                  {resumeProgressPercent != null && resumeProgressPercent > 0 && (
                    <span
                      className="absolute bottom-0 left-0 h-0.5 bg-white/70"
                      style={{ width: `${Math.min(100, Math.max(0, resumeProgressPercent))}%` }}
                      aria-hidden="true"
                    />
                  )}
                </button>

                <button
                  type="button"
                  onClick={onToggleFavorite}
                  className="flex size-10 items-center justify-center rounded-full border border-border bg-bg-card/60 backdrop-blur-sm transition-colors hover:bg-bg-elevated cursor-pointer"
                  aria-label={
                    isFavorite
                      ? t("itemDetail.removeFromFavorites")
                      : t("itemDetail.addToFavorites")
                  }
                >
                  <svg
                    className={`size-5 transition-colors ${isFavorite ? "text-error fill-error" : "text-text-secondary"}`}
                    viewBox="0 0 24 24"
                    fill={isFavorite ? "currentColor" : "none"}
                    stroke="currentColor"
                    strokeWidth={2}
                  >
                    <path
                      strokeLinecap="round"
                      strokeLinejoin="round"
                      d="M20.84 4.61a5.5 5.5 0 00-7.78 0L12 5.67l-1.06-1.06a5.5 5.5 0 00-7.78 7.78l1.06 1.06L12 21.23l7.78-7.78 1.06-1.06a5.5 5.5 0 000-7.78z"
                    />
                  </svg>
                </button>

                {menuItems.length > 0 && <SeriesHeroKebab items={menuItems} />}
              </div>
            </div>
          </div>
        </div>
      </div>
    </section>
  );
};

// Lightweight kebab variant of HeroSection's menu, kept inline here so
// SeriesHero stays self-contained. Mirrors the original API + visual.
const SeriesHeroKebab: FC<{ items: HeroMenuItem[] }> = ({ items }) => {
  const { t } = useTranslation();
  // Local open state — no portal needed, the hero overflow is hidden but
  // the menu pops upward on top of it.
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;
    const onClickOutside = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false);
    };
    const onEsc = (e: KeyboardEvent) => {
      if (e.key === "Escape") setOpen(false);
    };
    document.addEventListener("mousedown", onClickOutside);
    document.addEventListener("keydown", onEsc);
    return () => {
      document.removeEventListener("mousedown", onClickOutside);
      document.removeEventListener("keydown", onEsc);
    };
  }, [open]);

  return (
    <div ref={ref} className="relative">
      <button
        type="button"
        onClick={() => setOpen((o) => !o)}
        className="flex size-10 items-center justify-center rounded-full border border-border bg-bg-card/60 backdrop-blur-sm transition-colors hover:bg-bg-elevated cursor-pointer"
        aria-label={t("common.moreOptions")}
        aria-expanded={open}
      >
        <svg className="size-5 text-text-secondary" viewBox="0 0 24 24" fill="currentColor">
          <circle cx="12" cy="5" r="1.5" />
          <circle cx="12" cy="12" r="1.5" />
          <circle cx="12" cy="19" r="1.5" />
        </svg>
      </button>
      {open && (
        <div className="absolute bottom-full mb-2 left-0 z-50 min-w-[200px] rounded-[--radius-lg] border border-border bg-bg-card shadow-xl shadow-black/40 backdrop-blur-xl overflow-hidden">
          {items.map((mi) => (
            <button
              // El label es visible al usuario y único dentro del menú.
              key={mi.label}
              type="button"
              onClick={() => {
                setOpen(false);
                mi.onClick();
              }}
              className={[
                "flex w-full items-center gap-3 px-4 py-2.5 text-sm transition-colors cursor-pointer",
                mi.variant === "danger"
                  ? "text-error hover:bg-error/10"
                  : "text-text-secondary hover:text-text-primary hover:bg-bg-elevated",
              ].join(" ")}
            >
              <span className="flex size-5 shrink-0 items-center justify-center">{mi.icon}</span>
              {mi.label}
            </button>
          ))}
        </div>
      )}
    </div>
  );
};

// HeroTrailer + its helpers (TRAILER_DISMISSED_KEY,
// trailersDismissedThisSession, shouldSkipTrailer, trailerEmbedURL) used
// to live inline here. They moved to ./HeroTrailer.tsx so HeroSection
// can render the same Netflix-style preview on movie detail pages
// without duplicating ~220 lines and the suppression-decision tree
// drifting between the two surfaces.

export { SeriesHero };
export type { SeriesHeroProps };
