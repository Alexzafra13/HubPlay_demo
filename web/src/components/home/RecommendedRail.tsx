// RecommendedRail — "Recomendados para ti" rail on Home.
//
// Genre-affinity picks: títulos que el usuario no ha empezado y que
// comparten géneros con lo que ve activamente. El mismo pool que
// alimenta la tier "Recomendado" del hero y el buscador, pero aquí como
// rail dedicado — complemento natural de "Porque viste X" (que se siembra
// de UN título concreto; este es la afinidad agregada).
//
// Se oculta cuando:
//   - el usuario es cold-start (sin historial) → backend devuelve []
//   - la request falla → un rail ausente es mejor que uno roto
//
// Mismo vocabulario de tarjeta (PosterCard a los mismos anchos) que
// Trending / Porque viste, así los rails de descubrimiento leen como
// hermanos de la misma familia.

import { useTranslation } from "react-i18next";
import { useHomeRecommended } from "@/api/hooks";
import type { HomeRecommendedItem, MediaItem } from "@/api/types";
import { PosterCard } from "@/components/media";
import { Skeleton } from "@/components/common";
import { HomeRail } from "./HomeRail";

export function RecommendedRail() {
  const { t } = useTranslation();
  const { data, isLoading, isError } = useHomeRecommended();

  if (isError) return null;

  if (isLoading) {
    return (
      <HomeRail
        title={t("home.recommendedForYou", {
          defaultValue: "Recomendados para ti",
        })}
      >
        {Array.from({ length: 6 }, (_, i) => (
          <div
            key={`recommended-skeleton-${i}`}
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

  const items = data ?? [];
  if (items.length === 0) return null;

  return (
    <HomeRail
      title={t("home.recommendedForYou", {
        defaultValue: "Recomendados para ti",
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

// recommendedToMediaItem widens the slim Home-discovery row into the
// MediaItem shape PosterCard reads from. Mismo helper local que usan
// BecauseYouWatchedRail / TrendingRail — mantiene los rails de
// descubrimiento en un único vocabulario de tarjeta.
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
