// LatestInLibraryRail — portrait-poster rail showing the most recently
// added items in one library. Reuses the existing
// /api/v1/items/latest?library_id=X endpoint via the
// `useLatestItems(libraryId)` hook — no new endpoint needed.
//
// The rail title is provided by the home layout (server-side resolved
// from libraries.name) so a library rename is reflected without a
// second round-trip.

import { useTranslation } from "react-i18next";
import { useLatestItems, useLibraries } from "@/api/hooks";
import { PosterCard } from "@/components/media";
import { Skeleton } from "@/components/common";
import { HomeRail } from "./HomeRail";

interface LatestInLibraryRailProps {
  libraryId: string;
  libraryName: string;
}

export function LatestInLibraryRail({
  libraryId,
  libraryName,
}: LatestInLibraryRailProps) {
  const { t } = useTranslation();

  // Look up the library to derive the click target + the per-library
  // type filter. Lazy: the libraries list is already cached app-wide
  // so this rides on the existing fetch with no extra round-trip.
  const { data: libraries } = useLibraries();
  const library = libraries?.find((l) => l.id === libraryId);

  // For shows libraries we ask the backend for series rows directly.
  // Without the filter, `/items/latest` returns the most recently
  // added items by `added_at` — which in a TV library is dominated
  // by episodes (new episodes are the most common write pattern).
  // The frontend would then filter them out client-side and the rail
  // ends up showing one or two series at most. Pushing the filter
  // server-side keeps the rail honest with its title and avoids
  // wasting payload on rows we'd discard anyway.
  const typeFilter: "series" | undefined =
    library?.content_type === "shows" ? "series" : undefined;

  const { data, isLoading, isError } = useLatestItems(libraryId, {
    type: typeFilter,
  });

  if (isError) return null;

  // Title pattern matches Jellyfin's "Latest in <Library>" so users
  // who migrate over recognise the section instantly.
  const title = t("home.latestIn", {
    library: libraryName,
    defaultValue: `Reciente en ${libraryName}`,
  });

  // Defensive client-side filter for libraries that aren't shows
  // (movies, mixed): keep only top-level catalogue items so seasons
  // and orphan episodes never leak into the rail. Shows libraries
  // already get the right shape from the server.
  const filtered = (data ?? []).filter(
    (i) => i.type === "movie" || i.type === "series",
  );

  // Click target for the rail title — landing page filtered by
  // content type. We don't deep-link the library yet (the catalog
  // pages don't accept a library param), so the click lands on the
  // overall Movies / Series listing which is the closest match.
  const linkTo =
    library?.content_type === "movies"
      ? "/movies"
      : library?.content_type === "shows"
        ? "/series"
        : undefined;

  if (isLoading) {
    return (
      <HomeRail title={title} linkTo={linkTo}>
        {Array.from({ length: 7 }, (_, i) => (
          <div key={i} className="w-[180px] md:w-[200px] lg:w-[220px] xl:w-[240px] shrink-0">
            <Skeleton
              variant="rectangular"
              className="aspect-[2/3] w-full rounded-lg"
            />
            <Skeleton variant="text" width="80%" className="mt-2" />
            <Skeleton variant="text" width="50%" className="mt-1" />
          </div>
        ))}
      </HomeRail>
    );
  }

  if (filtered.length === 0) return null;

  return (
    <HomeRail title={title} linkTo={linkTo}>
      {filtered.map((item) => (
        <div key={item.id} className="w-[180px] md:w-[200px] lg:w-[220px] xl:w-[240px] shrink-0">
          <PosterCard item={item} />
        </div>
      ))}
    </HomeRail>
  );
}
