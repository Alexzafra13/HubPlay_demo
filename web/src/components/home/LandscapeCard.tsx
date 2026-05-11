// LandscapeCard — 16:9 card used for Continue Watching and other
// rails where a backdrop tells the story better than a poster.
//
// The "smart" part: when the item is an episode (item.type ===
// "episode"), the card displays the screencap (backdrop) and a
// "S0XE0Y · Episode title" subtitle, with the SERIES title as the
// main label — that's what the user actually wants to see in
// Continue Watching for series ("Daredevil — S01E03 The Cut Man"),
// not the bare episode title.
//
// Movies render with their own backdrop + title + year/genre. Both
// shapes share the same hover/zoom and rating-badge treatment so
// the rail keeps a consistent rhythm.

import { Check, X } from "lucide-react";
import { Link } from "react-router";
import { useTranslation } from "react-i18next";
import type { MediaItem } from "@/api/types";

interface LandscapeCardProps {
  item: MediaItem;
  // autoPlay=true tags the destination URL with `?play=1`, which the
  // detail page consumes to launch the player overlay immediately
  // instead of stopping on the metadata view. Used by Continue
  // Watching where every card is already a "resume" affordance —
  // clicking the play icon on a half-watched episode and then
  // having to click Reproducir again is a Plex pet peeve.
  autoPlay?: boolean;
  // Row actions for the Continue Watching surface. When provided the
  // card renders a top-right overlay (hover/focus revealed) with a
  // check button (mark as watched) and/or an X button (remove from
  // the rail). The card's surrounding <Link> doesn't fire when these
  // are clicked because we preventDefault + stopPropagation. Both
  // are optional so a single button can be rendered if only one
  // makes sense in context.
  onMarkWatched?: (item: MediaItem) => void;
  onRemove?: (item: MediaItem) => void;
}

export function LandscapeCard({
  item,
  autoPlay = false,
  onMarkWatched,
  onRemove,
}: LandscapeCardProps) {
  const { t } = useTranslation();
  const isEpisode = item.type === "episode";
  // Episodes link to their season's page so the user lands in the
  // episode list rather than hitting an isolated episode-detail
  // route. Movies / series link to their own detail surface.
  // Resume mode (autoPlay) is special: episodes link directly to
  // their own /items/:id route so the auto-play handler can fire
  // without a season-scope detour.
  const baseHref =
    item.type === "series"
      ? `/series/${item.id}`
      : item.type === "episode"
        ? autoPlay
          ? `/items/${item.id}`
          : item.parent_id
            ? `/items/${item.parent_id}`
            : `/items/${item.id}`
        : `/movies/${item.id}`;
  const href = autoPlay ? `${baseHref}?play=1` : baseHref;

  // Image priority depends on type:
  //   episode → backdrop_url (the per-episode screencap the scanner
  //             pulled from the still). Already 16:9 native.
  //   movie   → thumb_url first (the 16:9 "miniatura" providers ship
  //             with the cartel — purpose-built for landscape
  //             listing cards), then backdrop, then poster as the
  //             last resort. Older catalog entries without a thumb
  //             still get something usable.
  const image = isEpisode
    ? (item.backdrop_url ?? item.poster_url)
    : (item.thumb_url ?? item.backdrop_url ?? item.poster_url);

  // Title strategy — for episodes the "main" label is the series
  // name (which the API returns in `series_title` when available;
  // falls back to whatever's on item.title). The episode-specific
  // fields land in the subtitle.
  const seriesTitle = (item as MediaItem & { series_title?: string }).series_title;
  const mainTitle = isEpisode && seriesTitle ? seriesTitle : item.title;
  const episodeSub = isEpisode ? formatEpisodeSubtitle(item) : null;

  // Progress bar — only show partial state. Watched items don't
  // belong on continue-watching rails anyway, but be defensive.
  const progress = item.user_data?.progress?.percentage;
  const showProgress =
    progress != null && progress > 0 && progress < 100 && !item.user_data?.played;

  return (
    <Link
      to={href}
      className="group flex-shrink-0 w-[300px] md:w-[340px] lg:w-[380px] xl:w-[420px] flex flex-col gap-2"
    >
      <div className="relative aspect-[16/9] overflow-hidden rounded-[--radius-md] bg-bg-elevated">
        {image ? (
          <img
            src={image}
            alt=""
            loading="lazy"
            className="h-full w-full object-cover transition-transform duration-300 group-hover:scale-105"
          />
        ) : (
          <div className="flex h-full w-full items-center justify-center bg-gradient-to-br from-bg-elevated to-bg-card">
            <span className="text-2xl font-bold text-text-muted">
              {mainTitle.charAt(0)}
            </span>
          </div>
        )}

        {/* Hover play affordance */}
        <div className="absolute inset-0 flex items-center justify-center bg-black/0 transition-colors duration-200 group-hover:bg-black/30">
          <div className="flex h-10 w-10 items-center justify-center rounded-full bg-white/20 backdrop-blur-sm opacity-0 scale-90 transition-all duration-200 group-hover:opacity-100 group-hover:scale-100">
            <svg className="h-5 w-5 text-white ml-0.5" viewBox="0 0 24 24" fill="currentColor">
              <path d="M8 5v14l11-7z" />
            </svg>
          </div>
        </div>

        {/* Row actions overlay — top-right, only rendered when at
            least one handler is provided. Buttons stopPropagation so
            the surrounding <Link> doesn't navigate when the user
            actually wanted "mark as watched" or "remove from rail". */}
        {(onMarkWatched || onRemove) && (
          <div className="absolute top-2 left-2 flex items-center gap-1 opacity-0 transition-opacity duration-200 group-hover:opacity-100 group-focus-within:opacity-100">
            {onMarkWatched && (
              <button
                type="button"
                aria-label={t("home.markWatched")}
                title={t("home.markWatched")}
                onClick={(e) => {
                  e.preventDefault();
                  e.stopPropagation();
                  onMarkWatched(item);
                }}
                className="flex h-7 w-7 items-center justify-center rounded-full bg-black/70 text-white backdrop-blur-sm transition-colors hover:bg-black/85 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
              >
                <Check className="h-3.5 w-3.5" />
              </button>
            )}
            {onRemove && (
              <button
                type="button"
                aria-label={t("home.removeFromContinueWatching")}
                title={t("home.removeFromContinueWatching")}
                onClick={(e) => {
                  e.preventDefault();
                  e.stopPropagation();
                  onRemove(item);
                }}
                className="flex h-7 w-7 items-center justify-center rounded-full bg-black/70 text-white backdrop-blur-sm transition-colors hover:bg-black/85 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
              >
                <X className="h-3.5 w-3.5" />
              </button>
            )}
          </div>
        )}

        {/* Rating badge */}
        {item.community_rating != null && (
          <div className="absolute top-2 right-2 flex items-center gap-1 rounded-[--radius-sm] bg-black/70 backdrop-blur-sm px-1.5 py-0.5 text-[11px] font-semibold text-white">
            <svg className="h-2.5 w-2.5 text-warning" viewBox="0 0 24 24" fill="currentColor">
              <path d="M12 2l3.09 6.26L22 9.27l-5 4.87 1.18 6.88L12 17.77l-6.18 3.25L7 14.14 2 9.27l6.91-1.01L12 2z" />
            </svg>
            {item.community_rating.toFixed(1)}
          </div>
        )}

        {/* Progress bar — Continue Watching surface */}
        {showProgress && (
          <div className="absolute bottom-0 left-0 right-0 h-1 bg-black/50">
            {/* Floor at ~2% so a barely-started item still shows
                a visible chip — at 0.46% the bar would otherwise
                vanish into a sub-pixel sliver. */}
            <div
              className="h-full bg-accent"
              style={{ width: `${Math.min(100, Math.max(2, progress!))}%` }}
            />
          </div>
        )}
      </div>

      <div className="flex flex-col gap-0.5 px-0.5">
        <p className="text-sm font-medium text-text-primary truncate group-hover:text-white transition-colors">
          {mainTitle}
        </p>
        <div className="flex items-center gap-1.5 text-xs text-text-muted">
          {episodeSub ? (
            <span className="truncate">{episodeSub}</span>
          ) : (
            <>
              {item.year != null && <span>{item.year}</span>}
              {item.genres && item.genres.length > 0 && (
                <>
                  {item.year != null && <span className="text-text-muted/40">·</span>}
                  <span className="truncate">{item.genres.slice(0, 2).join(", ")}</span>
                </>
              )}
            </>
          )}
        </div>
      </div>
    </Link>
  );
}

function formatEpisodeSubtitle(item: MediaItem): string {
  const s = item.season_number;
  const e = item.episode_number;
  const code =
    s != null && e != null
      ? `S${String(s).padStart(2, "0")}E${String(e).padStart(2, "0")}`
      : null;
  // For episodes the scanner sets `item.title` to the EPISODE name
  // (the series name is sourced from `series_title` when present;
  // otherwise from `parent_id` ancestry — which the home payload
  // doesn't carry, so we just show the episode name on its own).
  return [code, item.title].filter(Boolean).join(" · ");
}
