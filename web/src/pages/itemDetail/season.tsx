// Season-hierarchy renderers for the item-detail page.
//
// Two surfaces share these components:
//   - Series page → `<SeasonEpisodes>` dispatches to a poster-grid of
//     seasons, OR (when the show is a flat one-season set) directly to
//     a grid of episode cards.
//   - Season page → `<SeasonEpisodeList>` renders the flat episode
//     list under the hero, with optional inline-play wiring.
//
// All four components live together because they share the same
// data shape (MediaItem) and the same translation keys; splitting
// them across files would scatter the season-rendering rules with
// no ergonomic benefit.

import { useMemo } from "react";
import { Link } from "react-router";
import { useTranslation } from "react-i18next";
import { useItemChildren } from "@/api/hooks";
import { Spinner } from "@/components/common";
import { EpisodeCard, EpisodeRow } from "@/components/media";
import type { MediaItem } from "@/api/types";
import { thumb } from "@/utils/imageUrl";

// Same target as PosterCard — season cards live on the same grid
// surface so the served bytes match and the browser cache hits across
// pages when the same image is reused.
const SEASON_THUMB_WIDTH = 480;

export function SeasonEpisodes({ seriesId }: { seriesId: string }) {
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
        {t("itemDetail.episodes")}
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
  const year =
    season.year ??
    (season.premiere_date ? new Date(season.premiere_date).getFullYear() : null);
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
            src={thumb(season.poster_url, SEASON_THUMB_WIDTH) ?? season.poster_url}
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

export function SeasonEpisodeList({
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
