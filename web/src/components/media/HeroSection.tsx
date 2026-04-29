import { useState, useRef, useEffect, useCallback } from "react";
import type { FC, ReactNode } from "react";
import { useTranslation } from "react-i18next";
import type { MediaItem } from "@/api/types";
import { Button } from "@/components/common/Button";
import { Badge } from "@/components/common/Badge";
import { useVibrantColors } from "@/hooks/useVibrantColors";
import { thumb } from "@/utils/imageUrl";

// Hero backdrops and posters live on a large surface; serve a
// bandwidth-efficient resized variant rather than the full-resolution
// ingest. 1280 covers retina laptops; 720 covers the inline poster.
const HERO_BACKDROP_WIDTH = 1280;
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
  onToggleFavorite?: () => void;
  isFavorite?: boolean;
  menuItems?: HeroMenuItem[];
}

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
  onToggleFavorite,
  isFavorite = false,
  menuItems = [],
}) => {
  const { t } = useTranslation();
  const duration = formatRuntime(item.duration_ticks);

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
  const hasServerPalette =
    !!(item.backdrop_colors?.vibrant || item.backdrop_colors?.muted);
  const runtimePalette = useVibrantColors(
    hasServerPalette ? null : heroBackdropUrl,
  );
  const gradientStart =
    item.backdrop_colors?.muted ?? runtimePalette.muted ?? "rgb(8, 12, 16)";
  const gradientMid =
    item.backdrop_colors?.vibrant ?? runtimePalette.vibrant ?? "rgb(8, 12, 16)";

  return (
    <section
      className="relative -mx-4 md:-mx-6 overflow-hidden"
      style={{
        ['--hero-c1' as string]: gradientStart,
        ['--hero-c2' as string]: gradientMid,
        // Bleed behind the sticky TopBar so the backdrop reaches the
        // very top of the viewport. Same pattern as SeriesHero — keeps
        // both detail surfaces visually aligned.
        marginTop: "calc(var(--topbar-height) * -1)",
      }}
    >
      <div className="relative min-h-[460px] sm:min-h-[540px] lg:min-h-[600px] max-h-[720px]">
        {heroBackdropUrl ? (
          <img
            src={thumb(heroBackdropUrl, HERO_BACKDROP_WIDTH) ?? heroBackdropUrl}
            alt=""
            loading="eager"
            className="absolute inset-0 h-full w-full object-cover"
            style={{ objectPosition: "right top" }}
          />
        ) : (
          <div className="absolute inset-0 bg-gradient-to-br from-bg-elevated to-bg-card" />
        )}

        {/* Vibrant-color fade on the left ~50%. Solid colour under the
            poster, mid stop bleeds into the image, third stop is fully
            transparent so the backdrop breathes on the right. */}
        <div
          className="absolute inset-0"
          style={{
            background:
              "linear-gradient(to right, var(--hero-c1) 0%, color-mix(in srgb, var(--hero-c2) 60%, transparent) 30%, transparent 55%)",
          }}
        />

        {/* Bottom-fade — image -> page tint. Targets `--detail-tint`
            so the hero blends into the rest of the page with no seam. */}
        <div
          className="absolute inset-x-0 bottom-0 h-48 lg:h-56"
          style={{
            background:
              "linear-gradient(to bottom, transparent, var(--detail-tint, rgb(8 12 16)))",
          }}
        />

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

              <div className="flex flex-wrap items-center gap-2 text-sm text-text-secondary">
                {/* Episodes: prefer the full air date — "12 Mar 2025"
                    reads more meaningfully than the bare year on the
                    per-episode page. Movies and seasons keep the
                    year-only line. */}
                {isEpisode && item.premiere_date ? (
                  <span className="font-medium">
                    {new Date(item.premiere_date).toLocaleDateString(undefined, {
                      day: "numeric",
                      month: "short",
                      year: "numeric",
                    })}
                  </span>
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
                    taxonomy badges, same pattern as SeriesHero. */}
                {item.studio && (
                  <span className="text-xs text-text-muted">· {item.studio}</span>
                )}
              </div>

              {item.overview != null && (
                <p className="line-clamp-3 max-w-2xl text-sm leading-relaxed text-text-secondary sm:text-[15px]">
                  {item.overview}
                </p>
              )}

              <div className="flex items-center gap-3 pt-1">
                <Button size="lg" onClick={onPlay}>
                  <svg className="h-5 w-5" viewBox="0 0 24 24" fill="currentColor">
                    <path d="M8 5v14l11-7z" />
                  </svg>
                  {t("common.play")}
                </Button>

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
