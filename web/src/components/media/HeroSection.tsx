import { useState, useRef, useEffect, useCallback, useMemo } from "react";
import type { FC, ReactNode } from "react";
import { useTranslation } from "react-i18next";
import type { MediaItem, Person } from "@/api/types";
import { Button } from "@/components/common/Button";
import { Badge } from "@/components/common/Badge";
import { thumb } from "@/utils/imageUrl";

// Posters are small enough (≤340px tall) that a 720px-wide variant
// covers 2x DPR comfortably — worth the bandwidth save. The hero
// backdrop, however, fills the entire viewport width on widescreen
// monitors (1920+); requesting a 1280-wide variant for it produced
// visibly upscaled, soft edges. The backdrop now uses the source
// URL directly so the browser receives the largest available
// ingest size and scales DOWN at most.
const HERO_POSTER_WIDTH = 720;

// ─── Menu item type ─────────────────────────────────────────────────────────

export interface HeroMenuItem {
  label: string;
  icon: ReactNode;
  onClick: () => void;
  variant?: "default" | "danger";
  adminOnly?: boolean;
}

interface HeroSectionProps {
  item: MediaItem;
  onPlay?: () => void;
  /**
   * Custom label for the primary CTA. Defaults to t("common.play").
   * Used by the federation peer detail page to surface
   * "Reanudar 0:58" when there's a saved cross-peer position.
   */
  playLabel?: string;
  onToggleFavorite?: () => void;
  isFavorite?: boolean;
  menuItems?: HeroMenuItem[];
  /**
   * Cast / crew rows from ItemDetail. The hero only consumes them to
   * surface the director credit prominently below the title (Plex-
   * style "Directed by …"). MediaItem itself doesn't carry people, so
   * the page passes them through as a sibling prop. Optional —
   * federation peer detail and any caller without the detail payload
   * pass nothing and the line just doesn't render.
   */
  people?: Person[];
}

// Extract the headline crew credit. Movies almost always have a single
// "Director" entry; if multiple are listed (co-direction) we surface
// the first by sort_order so the line stays one row. The lookup is
// case-insensitive because the scanner has historically inserted both
// "Director" and "director" depending on TMDb's response shape.
function findDirector(people: Person[] | undefined): string | null {
  if (!people || people.length === 0) return null;
  const sorted = [...people].sort((a, b) => a.sort_order - b.sort_order);
  const director = sorted.find((p) => p.role.toLowerCase() === "director");
  return director?.name ?? null;
}

// External-ID button row — small icons that link out to the canonical
// page on IMDb / TMDb / TVDb when the scanner persisted a provider id.
// Order is fixed (IMDb first because it's the most-recognised in the
// movie world) so users build muscle memory.
const EXTERNAL_PROVIDERS: ReadonlyArray<{
  key: string;
  label: "openOnImdb" | "openOnTmdb" | "openOnTvdb";
  href: (id: string) => string;
  text: string;
}> = [
  {
    key: "imdb",
    label: "openOnImdb",
    href: (id) => `https://www.imdb.com/title/${encodeURIComponent(id)}/`,
    text: "IMDb",
  },
  {
    key: "tmdb",
    label: "openOnTmdb",
    href: (id) => `https://www.themoviedb.org/movie/${encodeURIComponent(id)}`,
    text: "TMDb",
  },
  {
    key: "tvdb",
    label: "openOnTvdb",
    href: (id) => `https://thetvdb.com/?tab=series&id=${encodeURIComponent(id)}`,
    text: "TVDb",
  },
];

function formatRating(rating: number): string {
  return rating.toFixed(1);
}

function formatRuntime(ticks: number | null | undefined): string | null {
  if (!ticks) return null;
  const totalMin = Math.round(ticks / 10_000_000 / 60);
  if (totalMin < 60) return `${totalMin}m`;
  const h = Math.floor(totalMin / 60);
  const m = totalMin % 60;
  return m > 0 ? `${h}h ${m}m` : `${h}h`;
}

// ─── Kebab menu ─────────────────────────────────────────────────────────────

const KebabMenu: FC<{ items: HeroMenuItem[] }> = ({ items }) => {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const menuRef = useRef<HTMLDivElement>(null);

  const close = useCallback(() => setOpen(false), []);

  useEffect(() => {
    if (!open) return;
    const onClickOutside = (e: MouseEvent) => {
      if (menuRef.current && !menuRef.current.contains(e.target as Node)) {
        close();
      }
    };
    const onEsc = (e: KeyboardEvent) => {
      if (e.key === "Escape") close();
    };
    document.addEventListener("mousedown", onClickOutside);
    document.addEventListener("keydown", onEsc);
    return () => {
      document.removeEventListener("mousedown", onClickOutside);
      document.removeEventListener("keydown", onEsc);
    };
  }, [open, close]);

  if (items.length === 0) return null;

  return (
    <div ref={menuRef} className="relative">
      <button
        type="button"
        onClick={() => setOpen((o) => !o)}
        className="flex h-10 w-10 items-center justify-center rounded-full border border-border bg-bg-card/60 backdrop-blur-sm transition-colors hover:bg-bg-elevated cursor-pointer"
        aria-label={t("common.moreOptions")}
        aria-expanded={open}
      >
        <svg className="h-5 w-5 text-text-secondary" viewBox="0 0 24 24" fill="currentColor">
          <circle cx="12" cy="5" r="1.5" />
          <circle cx="12" cy="12" r="1.5" />
          <circle cx="12" cy="19" r="1.5" />
        </svg>
      </button>

      {open && (
        <div className="absolute bottom-full mb-2 left-0 z-50 min-w-[200px] rounded-[--radius-lg] border border-border bg-bg-card shadow-xl shadow-black/40 backdrop-blur-xl overflow-hidden">
          {items.map((item, i) => (
            <button
              key={i}
              type="button"
              onClick={() => {
                close();
                item.onClick();
              }}
              className={[
                "flex w-full items-center gap-3 px-4 py-2.5 text-sm transition-colors cursor-pointer",
                item.variant === "danger"
                  ? "text-error hover:bg-error/10"
                  : "text-text-secondary hover:text-text-primary hover:bg-bg-elevated",
              ].join(" ")}
            >
              <span className="flex h-5 w-5 shrink-0 items-center justify-center">
                {item.icon}
              </span>
              {item.label}
            </button>
          ))}
        </div>
      )}
    </div>
  );
};

// ─── Hero section ───────────────────────────────────────────────────────────

/**
 * HeroSection — full-bleed hero used by movies, seasons and episodes.
 *
 * Mirrors the SeriesHero layout exactly: fixed-height band that bleeds
 * up behind the sticky TopBar, vibrant left-side colour fade driven by
 * the dominant palette, content stacked vertically in a column anchored
 * to the LEFT 30-40% of the band (poster on top, title block below,
 * action buttons last). The series hero used to be a separate visual
 * language; the movie / episode hero has now been pulled into the
 * same shape so the detail surface reads consistently regardless of
 * what type of media you opened.
 *
 * Episode-specific bits inside the column: a small uppercase
 * breadcrumb above the title for "what show is this?", and an
 * "S01E03 · Title" prefix on the heading. Movies and seasons just
 * show the regular logo-or-title heading.
 */
const HeroSection: FC<HeroSectionProps> = ({
  item,
  onPlay,
  playLabel,
  onToggleFavorite,
  isFavorite = false,
  menuItems = [],
  people,
}) => {
  const { t, i18n } = useTranslation();
  const duration = formatRuntime(item.duration_ticks);
  const director = useMemo(() => findDirector(people), [people]);

  // Movies in Plex render the full premiere date ("7 sept 2012")
  // whereas episodes already do this elsewhere. Falls back to the
  // bare year when there's no premiere_date row, and to nothing when
  // neither exists. Locale-aware via the active i18n language so
  // Spanish UI shows "7 sept 2012" and English shows "Sep 7, 2012".
  const movieReleaseDate = useMemo(() => {
    if (item.type !== "movie") return null;
    if (!item.premiere_date) return null;
    const d = new Date(item.premiere_date);
    if (Number.isNaN(d.getTime())) return null;
    return d.toLocaleDateString(i18n.language, {
      day: "numeric",
      month: "short",
      year: "numeric",
    });
  }, [item.type, item.premiere_date, i18n.language]);

  const externalLinks = useMemo(() => {
    if (!item.external_ids) return [];
    return EXTERNAL_PROVIDERS.flatMap((p) => {
      const id = item.external_ids?.[p.key];
      return id ? [{ ...p, id }] : [];
    });
  }, [item.external_ids]);

  // Overview clamp toggle — the description sits between the meta
  // chips and the CTA row, and the design clamps to 3 lines so the
  // hero stays focused on the play button. A "Read more" link reveals
  // the full text in place; "Read less" collapses it back. Disabled
  // when the overview obviously fits in 3 lines (≤240 chars is a
  // pragmatic threshold — covers ~3 lines at hero width).
  const [overviewExpanded, setOverviewExpanded] = useState(false);
  const overviewClampable = (item.overview?.length ?? 0) > 240;

  // Episodes and seasons carry limited visuals on their own (episodes
  // get a still, seasons get a poster, neither gets a backdrop). The
  // backend folds the parent series' artwork into `series_*_url` so
  // the hero falls back through this chain instead of rendering an
  // empty black slab. `isSubItem` is the "this row inherits from a
  // parent series" check; movies + series themselves never inherit.
  const isEpisode = item.type === "episode";
  const isSubItem = isEpisode || item.type === "season";
  const heroBackdropUrl =
    item.backdrop_url ??
    (isSubItem ? item.series_backdrop_url : undefined) ??
    item.poster_url ??
    null;
  const heroPosterUrl = isEpisode
    ? item.series_poster_url ?? item.poster_url ?? null
    : item.poster_url ?? null;
  const logoUrl = isSubItem
    ? item.series_logo_url ?? item.logo_url ?? undefined
    : item.logo_url ?? undefined;

  // Episode title rendering: "S01E01 · Pilot" or just the title when
  // numbering is missing.
  const episodeCode =
    isEpisode && item.season_number != null && item.episode_number != null
      ? `S${String(item.season_number).padStart(2, "0")}E${String(item.episode_number).padStart(2, "0")}`
      : null;

  // Same vibrant-colour pipeline as SeriesHero: prefer the backend-
  // precomputed palette (no decode delay) and fall back to runtime
  // extraction only when the primary image was ingested before the
  // extraction code shipped. The hook is a no-op when imageUrl is null,
  // so we skip the fetch entirely on rows whose colours already arrived
  // with the response.
  // Palette extraction lives in ItemDetail.tsx now (it owns the
  // page-wide aurora + publishes `--detail-tint` for hero hooks).
  // The hero just consumes that variable via the bottom-fade and
  // the image's mask — no per-hero palette state needed here.
  return (
    <section
      className="relative overflow-hidden"
      style={{
        // Bleed behind the sticky TopBar so the backdrop reaches the
        // very top of the viewport. Same pattern as SeriesHero — keeps
        // both detail surfaces visually aligned.
        marginTop: "calc(var(--topbar-height) * -1)",
      }}
    >
      <div className="relative min-h-[400px] sm:min-h-[440px] lg:min-h-[480px]">
        {/* Plex-style backdrop placement: image lives in the RIGHT
            half of the hero only and is masked into the page colour
            on its left edge. The result is that the page's 4-corner
            aurora shows through the left of the hero (under the
            poster + title) instead of the image bleeding edge to
            edge. Plex implements this with an SVG mask; CSS
            mask-image with a linear-gradient gives the same look in
            ~3 lines and stays responsive. */}
        {heroBackdropUrl ? (
          <img
            src={heroBackdropUrl}
            alt=""
            loading="eager"
            className="absolute inset-y-0 right-0 h-full w-full sm:w-4/5 lg:w-2/3 object-cover"
            style={{
              objectPosition: "right top",
              // Two-axis fade composited via mask-composite:intersect.
              // Mask 1 fades the image's left edge to transparent so
              // the page colour shows through under the title column.
              // Mask 2 fades the bottom edge to transparent so the
              // image dissolves into the page below the hero with no
              // visible seam (Plex's SVG mask does the same job; CSS
              // mask-composite is the lighter equivalent). Without
              // this, an extra `bottom-fade` overlay used to paint a
              // solid `--detail-tint` band that didn't match the
              // page's 4-corner aurora at that y-coordinate.
              // mask-composite: intersect (modern) +
              // -webkit-mask-composite: source-in (Safari legacy).
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
            where the poster sits in the optical centre of the hero. */}
        <div
          className="relative z-10 flex h-full items-center px-6 pb-12 sm:px-10 lg:px-16"
          style={{ paddingTop: "calc(var(--topbar-height) + 1.5rem)" }}
        >
          <div className="flex w-full max-w-md flex-col items-start gap-5 lg:max-w-lg">
            {/* Poster — same vertical orientation as SeriesHero.
                Suppressed for episodes (the show's vertical poster
                next to a horizontal episode still feels redundant
                and clashes; the still IS the episode's identity, but
                here we render the series poster anyway as a parent
                anchor — falls back gracefully when there's no
                inherited series_poster_url). */}
            {heroPosterUrl && !isEpisode && (
              <img
                src={thumb(heroPosterUrl, HERO_POSTER_WIDTH) ?? heroPosterUrl}
                alt={item.title}
                className="h-[240px] w-auto rounded-[--radius-lg] shadow-2xl shadow-black/60 object-cover sm:h-[280px] lg:h-[340px]"
              />
            )}

            <div className="flex flex-col gap-3">
              {/* Series breadcrumb — episodes only. Anchors the page
                  to "what show is this?" before the episode title
                  takes over. */}
              {isEpisode && item.series_title && (
                <p className="text-sm font-semibold uppercase tracking-wider text-text-muted">
                  {item.series_title}
                </p>
              )}

              {!isEpisode && logoUrl ? (
                <img
                  src={logoUrl}
                  alt={item.title}
                  className="max-h-[60px] sm:max-h-[80px] w-auto max-w-full object-contain object-left drop-shadow-lg"
                />
              ) : (
                <h1 className="text-3xl font-bold text-text-primary drop-shadow-lg sm:text-4xl">
                  {episodeCode ? (
                    <>
                      <span className="text-text-secondary">{episodeCode}</span>
                      <span className="px-2 text-text-muted">·</span>
                      {item.title}
                    </>
                  ) : (
                    item.title
                  )}
                </h1>
              )}

              {/* Tagline — italic line under the title, suppressed on
                  episodes. */}
              {!isEpisode && item.tagline && (
                <p className="-mt-1 max-w-xl text-sm italic text-text-secondary/90 drop-shadow-md">
                  {item.tagline}
                </p>
              )}

              {/* "Directed by …" — Plex surfaces this prominently
                  between tagline and meta chips so the headline crew
                  credit isn't buried in the cast strip below the
                  fold. Only rendered for movies; episodes inherit
                  their show's directors per-episode (mixed bag, not
                  worth a single line) and series/seasons don't have
                  a single director attribution. */}
              {item.type === "movie" && director && (
                <p className="text-sm font-medium text-text-secondary/90 drop-shadow-md">
                  {t("itemDetail.directedBy", { name: director })}
                </p>
              )}

              <div className="flex flex-wrap items-center gap-2 text-sm text-text-secondary">
                {/* Episodes: prefer the full air date — "12 Mar 2025"
                    reads more meaningfully than the bare year on the
                    per-episode page. Movies use the same full date
                    when available (Plex parity). Series/seasons
                    keep the year-only line because their air
                    "premiere" is per-season and a single date is
                    misleading. */}
                {isEpisode && item.premiere_date ? (
                  <span className="font-medium">
                    {new Date(item.premiere_date).toLocaleDateString(i18n.language, {
                      day: "numeric",
                      month: "short",
                      year: "numeric",
                    })}
                  </span>
                ) : movieReleaseDate ? (
                  <span className="font-medium">{movieReleaseDate}</span>
                ) : item.year != null ? (
                  <span className="font-medium">{item.year}</span>
                ) : null}

                {item.community_rating != null && (
                  <Badge variant="warning">
                    <svg className="h-3 w-3" viewBox="0 0 24 24" fill="currentColor">
                      <path d="M12 2l3.09 6.26L22 9.27l-5 4.87 1.18 6.88L12 17.77l-6.18 3.25L7 14.14 2 9.27l6.91-1.01L12 2z" />
                    </svg>
                    {formatRating(item.community_rating)}
                  </Badge>
                )}

                {item.content_rating != null && <Badge>{item.content_rating}</Badge>}

                {duration && (
                  <span className="text-text-muted">{duration}</span>
                )}

                {item.genres?.slice(0, 3).map((genre) => (
                  <Badge key={genre}>{genre}</Badge>
                ))}

                {/* Studio / network — soft attribution after the
                    taxonomy badges, same pattern as SeriesHero. When
                    the scanner persisted a brand-mark URL we render
                    the image (Lucasfilm, HBO, Disney+ …); otherwise
                    fall back to the studio text. The leading "·"
                    only appears with the text fallback because the
                    image already reads as a separate visual. */}
                {item.studio_logo_url ? (
                  <img
                    src={item.studio_logo_url}
                    alt={item.studio ?? ""}
                    className="ml-1 h-5 w-auto max-w-[120px] object-contain opacity-80"
                    loading="lazy"
                  />
                ) : item.studio ? (
                  <span className="text-xs text-text-muted">· {item.studio}</span>
                ) : null}
              </div>

              {item.overview != null && (
                <div className="max-w-2xl">
                  <p
                    className={`text-sm leading-relaxed text-text-secondary sm:text-[15px] ${
                      overviewExpanded || !overviewClampable ? "" : "line-clamp-3"
                    }`}
                  >
                    {item.overview}
                  </p>
                  {overviewClampable && (
                    <button
                      type="button"
                      onClick={() => setOverviewExpanded((v) => !v)}
                      className="mt-1 text-sm font-medium text-text-secondary transition-colors hover:text-text-primary cursor-pointer"
                      aria-expanded={overviewExpanded}
                    >
                      {overviewExpanded
                        ? t("itemDetail.readLess")
                        : t("itemDetail.readMore")}
                    </button>
                  )}
                </div>
              )}

              {/* External-ID badges — small clickable chips that
                  jump to the canonical IMDb / TMDb / TVDb page. The
                  scanner only persists provider ids the metadata
                  provider returned, so this row stays empty when no
                  match was made (no broken links). target=_blank +
                  rel=noopener so we don't leak referrer or window
                  handle to the third party. */}
              {externalLinks.length > 0 && (
                <div className="flex flex-wrap items-center gap-2">
                  {externalLinks.map((link) => (
                    <a
                      key={link.key}
                      href={link.href(link.id)}
                      target="_blank"
                      rel="noopener noreferrer"
                      aria-label={t(`itemDetail.${link.label}`)}
                      className="rounded-md border border-border/60 bg-bg-card/40 px-2 py-1 text-xs font-semibold text-text-secondary transition-colors hover:border-border hover:bg-bg-elevated hover:text-text-primary backdrop-blur-sm"
                    >
                      {link.text}
                    </a>
                  ))}
                </div>
              )}

              <div className="flex items-center gap-3 pt-1">
                <Button size="lg" onClick={onPlay}>
                  <svg className="h-5 w-5" viewBox="0 0 24 24" fill="currentColor">
                    <path d="M8 5v14l11-7z" />
                  </svg>
                  {playLabel ?? t("common.play")}
                </Button>

                {onToggleFavorite && (
                  <button
                    type="button"
                    onClick={onToggleFavorite}
                    className="flex h-10 w-10 items-center justify-center rounded-full border border-border bg-bg-card/60 backdrop-blur-sm transition-colors hover:bg-bg-elevated cursor-pointer"
                    aria-label={
                      isFavorite
                        ? t("itemDetail.removeFromFavorites")
                        : t("itemDetail.addToFavorites")
                    }
                  >
                    <svg
                      className={`h-5 w-5 transition-colors ${isFavorite ? "text-error fill-error" : "text-text-secondary"}`}
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
                )}

                {menuItems.length > 0 && <KebabMenu items={menuItems} />}
              </div>
            </div>
          </div>
        </div>
      </div>
    </section>
  );
};

export { HeroSection };
export type { HeroSectionProps };
