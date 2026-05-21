// BecauseYouWatchedRail — "Porque viste X" rail on Home.
//
// Seeded by the user's most recently completed watch (episode
// completes fold to the parent series, so the header reads
// "Porque viste Breaking Bad" instead of an episode title). The
// rail items are unwatched movies / series that share genres with
// the seed.
//
// Hides itself when:
//   - the user has no completed watches yet (cold-start) —
//     /me/home/because-you-watched returns seed: null in that
//     case, the hook turns it into items.length === 0
//   - the seed has no genres tagged — backend returns items: []
//     so the rail would be empty anyway
//   - the request errors — same fallback as the other rails: a
//     missing rail beats a broken one
//
// Visual continuity: same card vocabulary as Trending / Recommended
// (PosterCard at the same widths) so the four rails read as
// siblings of the same family rather than competing widgets.

import { useTranslation } from "react-i18next";
import { useHomeBecauseYouWatched } from "@/api/hooks";
import type { HomeRecommendedItem, MediaItem } from "@/api/types";
import { PosterCard } from "@/components/media";
import { Skeleton } from "@/components/common";
import { HomeRail } from "./HomeRail";

export function BecauseYouWatchedRail() {
  const { t } = useTranslation();
  const { data, isLoading, isError } = useHomeBecauseYouWatched();

  if (isError) return null;

  if (isLoading) {
    return (
      <HomeRail
        title={t("home.becauseYouWatchedLoading", {
          defaultValue: "Porque viste…",
        })}
      >
        {Array.from({ length: 6 }, (_, i) => (
          <div
            key={`because-skeleton-${i}`}
            className="w-[180px] md:w-[200px] lg:w-[220px] xl:w-[240px] shrink-0"
          >
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

  const seed = data?.seed;
  const items = data?.items ?? [];
  if (!seed || items.length === 0) return null;

  return (
    <HomeRail
      title={t("home.becauseYouWatched", {
        defaultValue: "Porque viste {{title}}",
        title: seed.title,
      })}
    >
      {items.map((it) => (
        <div
          key={it.id}
          className="w-[180px] md:w-[200px] lg:w-[220px] xl:w-[240px] shrink-0"
        >
          <PosterCard item={recommendedToMediaItem(it)} />
        </div>
      ))}
    </HomeRail>
  );
}

// recommendedToMediaItem widens the slim Home-discovery row into
// the MediaItem shape PosterCard reads from. Same trick the
// Trending rail uses — keeps the four discovery rails (Continue,
// Trending, Recommended, Because) on a single card vocabulary.
function recommendedToMediaItem(it: HomeRecommendedItem): MediaItem {
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
