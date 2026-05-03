// TrendingRail — server-wide top-played in the last 7 days.
//
// The /me/home/trending payload is a slimmer projection than
// MediaItem (no studio, no full user_data, no hierarchy fields), so
// we adapt each entry to the MediaItem shape PosterCard expects.
// Missing fields default to safe nulls — PosterCard already tolerates
// them. Keeps every home rail rendering through the same card so
// they share hover/zoom rhythm.

import { useTranslation } from "react-i18next";
import { useHomeTrending } from "@/api/hooks";
import type { HomeTrendingItem, MediaItem } from "@/api/types";
import { PosterCard } from "@/components/media";
import { Skeleton } from "@/components/common";
import { HomeRail } from "./HomeRail";

export function TrendingRail() {
  const { t } = useTranslation();
  const { data, isLoading, isError } = useHomeTrending();

  if (isError) return null;

  if (isLoading) {
    return (
      <HomeRail title={t("home.trending", { defaultValue: "Tendencia esta semana" })}>
        {Array.from({ length: 7 }, (_, i) => (
          <div key={i} className="w-[150px] sm:w-[170px] shrink-0">
            <Skeleton
              variant="rectangular"
              className="aspect-[2/3] w-full rounded-lg"
            />
            <Skeleton variant="text" width="80%" className="mt-2" />
          </div>
        ))}
      </HomeRail>
    );
  }

  const items = data ?? [];
  if (items.length === 0) return null;

  return (
    <HomeRail title={t("home.trending", { defaultValue: "Tendencia esta semana" })}>
      {items.map((it) => (
        <div key={it.id} className="w-[150px] sm:w-[170px] shrink-0">
          <PosterCard item={trendingToMediaItem(it)} />
        </div>
      ))}
    </HomeRail>
  );
}

// trendingToMediaItem widens the trending row into the MediaItem
// surface PosterCard reads from. Fields the trending endpoint
// doesn't carry (path, parent_id, premiere_date, etc.) get the
// nullable default the MediaItem type already permits — nothing
// downstream blows up on the absence.
function trendingToMediaItem(it: HomeTrendingItem): MediaItem {
  return {
    id: it.id,
    type: it.type as MediaItem["type"],
    title: it.title,
    original_title: null,
    year: it.year ?? null,
    sort_title: it.title,
    overview: it.overview ?? null,
    tagline: null,
    genres: it.genres ?? [],
    community_rating: it.community_rating ?? null,
    content_rating: null,
    duration_ticks: null,
    premiere_date: null,
    poster_url: it.poster_url ?? null,
    backdrop_url: it.backdrop_url ?? null,
    logo_url: it.logo_url ?? null,
    poster_color: it.poster_color,
    poster_color_muted: it.poster_color_muted,
    poster_blurhash: it.poster_blurhash,
    parent_id: null,
    series_id: null,
    season_number: null,
    episode_number: null,
    path: null,
  };
}
