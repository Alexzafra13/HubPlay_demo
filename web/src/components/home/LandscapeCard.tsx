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

import { Link } from "react-router";
import type { MediaItem } from "@/api/types";

interface LandscapeCardProps {
  item: MediaItem;
}

export function LandscapeCard({ item }: LandscapeCardProps) {
  const isEpisode = item.type === "episode";
  // Episodes link to their season's page so the user lands in the
  // episode list rather than hitting an isolated episode-detail
  // route. Movies / series link to their own detail surface.
  const href =
    item.type === "series"
      ? `/series/${item.id}`
      : item.type === "episode" && item.parent_id
        ? `/items/${item.parent_id}`
        : `/movies/${item.id}`;

  // Image priority: episode → its own screencap (backdrop_url is
  // populated by the scanner from the per-episode still). Movies →
  // backdrop, then poster as fallback.
  const image = item.backdrop_url ?? item.poster_url;

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
